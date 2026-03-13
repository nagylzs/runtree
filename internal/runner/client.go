package runner

import (
	"context"
	"fmt"
	"io"
	"log"
	"log/slog"
	"math/rand"
	"net"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/xtaci/smux"
)

type Client struct {
	conn      net.Conn
	ptyStream *smux.Stream // the other side is connected to remote pty, on the runner
	cmdStream *smux.Stream // the other side is connected to the command handler, on the runner
	logger    *slog.Logger

	PtyPath         string       // local unix domain socket path for the forwarded pty
	ptyListener     net.Listener // local unix domain socket's listener for the forwarded pty
	ptyConn         net.Conn     // local unix domain socket for the forwarded pty
	unixSocketReady sync.WaitGroup

	stop      *atomic.Bool
	stateLock sync.Mutex
	lastState ProcState
	cmdLock   sync.Mutex
	onNotify  func(ProcState)
	onError   func(Error)
}

func NewClient(ctx context.Context, address string, logger *slog.Logger, onError func(Error)) (*Client, error) {
	conn, err := net.Dial("tcp", address)
	if err != nil {
		return nil, err
	}

	err = negotiateVersion(ctx, conn)
	if err != nil {
		return nil, err
	}

	c := &Client{
		conn:            conn,
		ptyStream:       nil,
		cmdStream:       nil,
		unixSocketReady: sync.WaitGroup{},
		logger:          logger,
		onNotify:        nil,
		onError:         onError,
		stop:            &atomic.Bool{},

		stateLock: sync.Mutex{},
	}
	return c, nil
}

// StartPty starts a pty master on the server side, and connects it to a local pty slave.
// This function starts several goroutines, and will only return an error if the startup failed.
// Calling this function permanently converts the client into a remote Pty session, and there is
// no way to use it for anything else. If this function returns an error, then you **must** call Close.
// If this function returns nil, then you **must** call Close later, when the remote process exits.
func (c *Client) StartPty(ctx context.Context, cmdArgs CmdArgs, onNotify func(state ProcState)) error {
	var session *smux.Session = nil
	var ptyStream *smux.Stream = nil
	var cmdStream *smux.Stream = nil

	// deferred cleanup on error
	defer func() {
		if cmdStream != nil {
			_ = cmdStream.Close()
		}
		if ptyStream != nil {
			_ = ptyStream.Close()
		}
		if session != nil {
			_ = session.Close()
		}
	}()

	err := SendCmdCode(ctx, c.conn, MsgStartPty)
	if err != nil {
		return err
	}

	session, err = smux.Client(c.conn, nil)
	if err != nil {
		return err
	}

	ptyStream, err = session.OpenStream()
	if err != nil {
		return err
	}
	cmdStream, err = session.OpenStream()
	if err != nil {
		return err
	}

	c.ptyStream, c.cmdStream, c.onNotify = ptyStream, cmdStream, onNotify

	// do not delete this line, this disables the deferred error cleanup
	ptyStream, cmdStream, session = nil, nil, nil

	// Send initial command to the terminal server.
	err = SendAny(ctx, c.cmdStream, cmdArgs)
	if err != nil {
		return err
	}

	go c.receiveCommands()
	c.unixSocketReady.Add(1)
	go c.run()
	c.unixSocketReady.Wait()
	return nil
}

func (c *Client) RequestProcState(ctx context.Context, ignoreError bool) error {
	return c.sendMsg(ctx, MsgProcState, nil, ignoreError)
}

func (c *Client) SendSignal(ctx context.Context, name string, ignoreError bool) error {
	return c.sendMsg(ctx, MsgProcSignal, name, ignoreError)
}

func (c *Client) sendMsg(ctx context.Context, code CmdCode, msg any, ignoreError bool) error {
	c.cmdLock.Lock()
	defer c.cmdLock.Unlock()
	err := SendAny(ctx, c.cmdStream, code)
	if err != nil && !ignoreError {
		c.requestStop(err)
		return err
	}
	if msg == nil {
		return nil
	}
	err = SendAny(ctx, c.cmdStream, msg)
	if err != nil && !ignoreError {
		c.requestStop(err)
	}
	return err
}

// SendSize sends pty slave (display) size change to master
func (c *Client) SendSize(ctx context.Context, ws WinSize, ignoreError bool) error {
	return c.sendMsg(ctx, MsgResize, &ws, ignoreError)
}

func (c *Client) logError(err error) {
	if err != nil {
		c.logger.Error(err.Error())
	}
}

func (c *Client) requestStop(err error) {
	c.stop.Store(true)

	c.stateLock.Lock()
	c.lastState.Exited = true
	if err != nil {
		c.lastState.ExitCode = -1
		c.lastState.Error = err.Error()
	}
	c.stateLock.Unlock()
	c.notifyListeners()

	if err != nil {
		c.logError(err)
	}
}

func (c *Client) receiveCommands() {
	ctx := context.Background()

	for !c.stop.Load() {
		var code CmdCode
		err := RecvAny(ctx, c.cmdStream, &code)
		if err != nil {
			c.requestStop(fmt.Errorf("could not receive command code: %w", err))
			return
		}
		switch code {
		case MsgProcState:
			c.recvProcState(ctx)
		case MsgError:
			c.recvError(ctx)
		default:
			c.requestStop(fmt.Errorf("invalid command received: %d", code))
		}
	}
}

func (c *Client) recvProcState(ctx context.Context) {
	var ps ProcState
	err := RecvAny(ctx, c.cmdStream, &ps)
	if err != nil {
		c.requestStop(fmt.Errorf("could not receive new ProcState: %w", err))
		return
	}
	slog.Info(fmt.Sprintf("ProcState: %+v", ps))
	c.stateLock.Lock()
	c.lastState = ps
	c.stateLock.Unlock()
	c.notifyListeners()

	if !ps.Alive() {
		err = c.sendMsg(ctx, MsgDisconnect, nil, false)
		if err != nil {
			slog.Info(fmt.Sprintf("recvProcState: MsgDisconnect: %s", err.Error()))
		}
		// not sure why this is needed, but if we disconnect too soon, then the error message
		// of a quickly exited program may not arrive
		time.Sleep(2 * time.Second)
		c.Close()
	}
}

func (c *Client) recvError(ctx context.Context) {
	var e Error
	err := RecvAny(ctx, c.cmdStream, &e)
	if err != nil {
		c.requestStop(fmt.Errorf("could not receive new Error: %w", err))
		return
	}
	c.logger.Error(fmt.Sprintf("Error: %+v", e))
	if c.onError != nil {
		c.onError(e)
	}
}

func (c *Client) failedStart(err error) {
	c.stateLock.Lock()
	c.lastState = ProcState{
		Pid:        0,
		Exited:     true,
		ExitCode:   0,
		SystemTime: 0,
		UserTime:   0,
		Error:      err.Error(),
	}
	c.stateLock.Unlock()
	c.logger.Error(err.Error())
}

func (c *Client) run() {
	path, listener, err := createUnixSocket()
	if err != nil {
		c.failedStart(err)
		return
	}
	c.PtyPath, c.ptyListener = path, listener
	defer os.Remove(c.PtyPath)
	defer c.ptyListener.Close()

	c.unixSocketReady.Done()

	c.ptyConn, err = listener.Accept()
	if err != nil {
		log.Fatalf("Accept error: %v", err)
		return
	}
	defer c.ptyConn.Close()

	wg := sync.WaitGroup{}
	wg.Add(1)
	go func() {
		io.Copy(c.ptyStream, c.ptyConn)
		// note: we do not wait for this to complete, because ptyConn will not be closed until run() returns,
		// so it will never complete...
	}()
	go func() {
		io.Copy(c.ptyConn, c.ptyStream)
		wg.Done()
		// in contrast, ptyStream will be closed when the remote process exits, this signals the end of
		// the communication
	}()
	wg.Wait()
}

func (c *Client) notifyListeners() {
	c.stateLock.Lock()
	state := c.lastState
	c.stateLock.Unlock()
	if c.onNotify != nil {
		c.onNotify(state)
	}
}

func (c *Client) LastState() ProcState {
	c.stateLock.Lock()
	defer c.stateLock.Unlock()
	return c.lastState
}

func (c *Client) Close() {
	c.requestStop(nil)
	if c.cmdStream != nil {
		_ = c.cmdStream.Close()
	}
	if c.ptyStream != nil {
		_ = c.ptyStream.Close()
	}
	if c.conn != nil {
		_ = c.conn.Close()
	}
}

// createUnixSocket creates a Unix domain socket with a unique name under /tmp
func createUnixSocket() (string, net.Listener, error) {
	for {
		socketName := fmt.Sprintf("socket-%d-%d.sock", time.Now().UnixNano(), rand.Intn(10000))
		socketPath := filepath.Join("/tmp", socketName)

		// Ensure the socket doesn't already exist
		if _, err := os.Stat(socketPath); os.IsNotExist(err) {
			listener, err := net.Listen("unix", socketPath)
			if err != nil {
				return "", nil, fmt.Errorf("failed to create socket: %w", err)
			}
			if err := os.Chmod(socketPath, 0600); err != nil {
				return "", nil, fmt.Errorf("Cannot chmod socket: %w", err)
			}
			return socketPath, listener, nil
		}
	}
}
