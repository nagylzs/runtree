package runner

import (
	"context"
	"errors"
	"fmt"
	"io"
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

	PtyPath     string       // local unix domain socket path for the forwarded pty (not with SetNoTerminal)
	ptyListener net.Listener // local unix domain socket's listener for the forwarded pty
	ptyConn     net.Conn     // local unix domain socket for the forwarded pty

	stop       *atomic.Bool
	stateLock  sync.Mutex
	lastState  ProcState
	cmdLock    sync.Mutex
	onNotify   func(ProcState)
	onError    func(Error)
	output     io.Writer // gets a copy of the terminal output, see SetOutput
	noTerminal bool      // see SetNoTerminal
}

// NewClient connects to a runner (see ParseAddress)
func NewClient(ctx context.Context, address string, logger *slog.Logger, onError func(Error)) (*Client, error) {
	a, err := ParseAddress(address)
	if err != nil {
		return nil, err
	}
	var conn net.Conn
	switch a.Network {
	case NetworkLocal:
		conn, err = local.dial()
	case NetworkSSH:
		handshake := func(conn *sshConn) (*Client, error) {
			c, err := newClient(ctx, conn, SSHHandshakeTimeout, logger, onError)
			if err != nil {
				err = conn.failure(err)
			}
			return c, err
		}
		if a.Managed() {
			return dialManaged(a, logger, handshake)
		}
		ssh, err := dialSSH(a, remoteCommand(a), logger)
		if err != nil {
			return nil, err
		}
		return handshake(ssh)
	default:
		d := net.Dialer{Timeout: HandshakeTimeout}
		conn, err = d.DialContext(ctx, "tcp", a.Host)
	}
	if err != nil {
		return nil, err
	}
	return newClient(ctx, conn, HandshakeTimeout, logger, onError)
}

// HandshakeTimeout is the time for a TCP runner to connect and answer the protocol handshake
const HandshakeTimeout = 30 * time.Second

// newClient negotiates the protocol version on conn, and closes it when that fails
func newClient(ctx context.Context, conn net.Conn, timeout time.Duration, logger *slog.Logger,
	onError func(Error)) (*Client, error) {
	_ = conn.SetDeadline(time.Now().Add(timeout))
	err := negotiateVersion(ctx, conn)
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	_ = conn.SetDeadline(time.Time{})

	c := &Client{
		conn:      conn,
		ptyStream: nil,
		cmdStream: nil,
		logger:    logger,
		onNotify:  nil,
		onError:   onError,
		stop:      &atomic.Bool{},

		stateLock: sync.Mutex{},
	}
	return c, nil
}

// SetOutput sets a writer that gets a copy of the terminal output of the process (not the input), e.g. to save it.
// Its errors are ignored, and it should not block for long: the terminal waits for it. When it is an io.Closer, it is
// closed after the end of the output. It must be called before StartPty.
func (c *Client) SetOutput(w io.Writer) {
	c.output = w
}

// SetNoTerminal starts the process without a local terminal (there is no PtyPath to attach to): its output only goes
// to the writer of SetOutput (or it is dropped), and nobody types into it (e.g. runtree-headless). It must be called
// before StartPty.
func (c *Client) SetNoTerminal() {
	c.noTerminal = true
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

	// the socket of the terminal, before the process is started (the caller closes the client when it fails)
	if !c.noTerminal {
		c.PtyPath, c.ptyListener, err = createUnixSocket()
		if err != nil {
			return err
		}
	}

	// Send initial command to the terminal server.
	err = SendAny(ctx, c.cmdStream, cmdArgs)
	if err != nil {
		if c.ptyListener != nil {
			_ = c.ptyListener.Close()
			_ = os.Remove(c.PtyPath)
		}
		return err
	}

	go c.receiveCommands()
	go c.run()
	return nil
}

func (c *Client) RequestProcState(ctx context.Context, ignoreError bool) error {
	return c.sendMsg(ctx, MsgProcState, nil, ignoreError)
}

func (c *Client) SendSignal(ctx context.Context, name string, ignoreError bool) error {
	return c.sendMsg(ctx, MsgProcSignal, name, ignoreError)
}

// CtrlC is the input of Ctrl+C: the terminal sends SIGINT to the processes in its foreground (unless a program reads
// it as input, e.g. ssh -t passes it to the remote terminal)
var CtrlC = []byte{3}

// Type writes input to the terminal of the process, as if it was typed (e.g. CtrlC). When a terminal is attached too,
// its input and this one may interleave. It must be called after StartPty.
func (c *Client) Type(input []byte) error {
	if c.ptyStream == nil {
		return errors.New("the process is not started")
	}
	_, err := c.ptyStream.Write(input)
	return err
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
	c.logger.Debug("process state", "pid", ps.Pid, "exited", ps.Exited, "exit_code", ps.ExitCode, "error", ps.Error)
	c.stateLock.Lock()
	c.lastState = ps
	c.stateLock.Unlock()
	c.notifyListeners()

	if !ps.Alive() {
		err = c.sendMsg(ctx, MsgDisconnect, nil, false)
		if err != nil {
			c.logger.Debug("could not send disconnect", "error", err)
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
	c.logger.Debug("runner error", "code", e.Code, "message", e.Message)
	if c.onError != nil {
		c.onError(e)
	}
}

// run copies the output of the process to the terminal that connects to the socket (and to the output writer), and
// the input from the terminal to the process, until the pty stream is closed (the process exited)
func (c *Client) run() {
	defer c.closeOutput()
	if c.noTerminal {
		// the errors of the output are ignored: the stream must be read, or the process blocks when it writes
		_, _ = io.Copy(teeWriter{io.Discard, c.output}, c.ptyStream)
		return
	}
	defer os.Remove(c.PtyPath)
	defer c.ptyListener.Close()

	var err error
	c.ptyConn, err = c.ptyListener.Accept()
	if err != nil {
		c.requestStop(fmt.Errorf("could not accept the terminal connection: %w", err))
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
		var dst io.Writer = c.ptyConn
		if c.output != nil {
			dst = teeWriter{dst, c.output}
		}
		io.Copy(dst, c.ptyStream)
		wg.Done()
		// in contrast, ptyStream will be closed when the remote process exits, this signals the end of
		// the communication
	}()
	wg.Wait()
}

// closeOutput closes the output writer, when it is an io.Closer
func (c *Client) closeOutput() {
	if cl, ok := c.output.(io.Closer); ok {
		_ = cl.Close()
	}
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

// teeWriter writes to w, and a copy to copyTo (its errors are ignored)
type teeWriter struct {
	w      io.Writer
	copyTo io.Writer
}

func (t teeWriter) Write(p []byte) (int, error) {
	n, err := t.w.Write(p)
	if n > 0 && t.copyTo != nil {
		_, _ = t.copyTo.Write(p[:n])
	}
	return n, err
}
