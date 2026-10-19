package runner

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/inhies/go-bytesize"
	"github.com/nagylzs/runtree/internal/signal"
	"github.com/xtaci/smux"
)

const BufSize = 32768 // L3 cache size...

// When the server stops serving a running process (e.g. the client disconnected), the process is terminated with
// SIGINT, then SIGTERM after TerminateGraceInt, then SIGKILL after TerminateGraceTerm.
const (
	TerminateGraceInt  = 10 * time.Second
	TerminateGraceTerm = 10 * time.Second
)

type PtyRunner interface {
	StdIn() io.WriteCloser
	StdOut() io.Reader
	Pty() io.Closer
	Pid() int
	Cmd() *exec.Cmd
	Resize(ws WinSize) error
	SignalGroup(sig os.Signal) error // send a signal to the process group of the command
	GroupAlive() bool                // tells if any process is left in the process group of the command
	Exited()                         // the command exited (Cmd().Wait returned)
	Close() error
}

// errSignalNotSupported is the error of PtyRunner.SignalGroup for a signal that the system does not have
var errSignalNotSupported = errors.New("the signal is not supported on this system")

// Server runs a command in a pseudo-terminals and forward pty and command channels to a socket
type Server struct {
	ptyRunner PtyRunner

	conn      io.ReadWriteCloser
	ptyStream *smux.Stream
	cmdStream *smux.Stream
	logger    *slog.Logger
	stop      *atomic.Bool
	wg        *sync.WaitGroup

	exited    chan struct{} // closed when the process has exited
	terminate sync.Once

	startLock   sync.Mutex // guards ptyRunner (set when the process started) and terminating, for Terminate
	terminating bool       // Terminate was called
}

// RunNewServer creates a new server for the given connection, and runs it until the client disconnects.
// Should be called as a goroutine.
func RunNewServer(ctx context.Context, conn io.ReadWriteCloser, logger *slog.Logger) error {
	return newServer(conn, logger).Run(ctx)
}

func newServer(conn io.ReadWriteCloser, logger *slog.Logger) *Server {
	return &Server{
		ptyRunner: nil,
		conn:      conn,
		ptyStream: nil,
		cmdStream: nil,
		logger:    logger,
		wg:        &sync.WaitGroup{},
		stop:      &atomic.Bool{},
		exited:    make(chan struct{}),
	}
}

// Run serves the connection until the client disconnects
func (s *Server) Run(ctx context.Context) error {
	s.logger.Debug("negotiating the protocol version")
	err := negotiateVersion(ctx, s.conn)
	if err != nil {
		return err
	}
	return s.Serve(ctx)
}

// processRunning tells if the process of the server has started and not exited yet
func (s *Server) processRunning() bool {
	s.startLock.Lock()
	started := s.ptyRunner != nil
	s.startLock.Unlock()
	if !started {
		return false
	}
	select {
	case <-s.exited:
		return false
	default:
		return true
	}
}

// Terminate terminates the process of the server, as when its client is gone: SIGINT, then SIGTERM and SIGKILL after
// the grace periods. When the process is not started yet, it is terminated as soon as it starts. The client gets its
// exit status as usual.
func (s *Server) Terminate() {
	s.startLock.Lock()
	s.terminating = true
	started := s.ptyRunner != nil
	s.startLock.Unlock()
	if started {
		s.terminate.Do(func() {
			go s.terminateProcess()
		})
	}
}

func (s *Server) Serve(ctx context.Context) error {
	for {
		cmd, err := RecvCmdCode(ctx, s.conn)
		if err != nil {
			return err
		}
		switch cmd {
		case MsgStartPty:
			return s.ServePty(ctx)
		default:
			return fmt.Errorf("runner.Serve: unknown command: %d", cmd)
		}
	}
}

func (s *Server) ServePty(ctx context.Context) error {
	var err error

	s.logger.Debug("starting the streams")
	session, err := smux.Server(s.conn, nil)
	if err != nil {
		return err
	}

	defer func() {
		// usually the client has closed the connection already (broken pipe), and closing the session closes the
		// connection too
		if err := session.Close(); err != nil {
			s.logger.Debug("closing the session", "error", err)
		}
		if err := s.conn.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
			s.logger.Error("could not close the connection", "error", err)
		}
		s.logger.Debug("disconnected")
	}()

	ptyStream, err := session.AcceptStream()
	if err != nil {
		return err
	}
	cmdStream, err := session.AcceptStream()
	if err != nil {
		ptyStream.Close()
		return err
	}
	s.ptyStream, s.cmdStream = ptyStream, cmdStream

	var ca CmdArgs
	err = RecvAny(ctx, s.cmdStream, &ca)
	if err != nil {
		return err
	}

	// not the environment, it may contain secrets
	s.logger.Debug("starting", "args", ca.Args, "cwd", ca.Cwd)

	if ca.InheritSysEnvs {
		ca.Env = append(os.Environ(), ca.Env...)
	}

	ptyRunner, err := s.startPty(ca)
	if err != nil {
		return err
	}
	s.startLock.Lock()
	s.ptyRunner = ptyRunner
	terminating := s.terminating
	s.startLock.Unlock()
	defer func() {
		err := ptyRunner.Close()
		if err != nil {
			s.logger.Error("PtyRunner.Close", "err", err)
		}
	}()

	s.logger.Info("started", "pid", ptyRunner.Pid(), "args", ca.Args)
	if terminating {
		s.Terminate()
	}

	s.stop.Store(false)
	s.logError(s.sendProcState(err))

	s.wg.Add(3)
	go s.connectStreams(s.ptyStream, s.ptyRunner.StdOut(), "pty-recv")
	go s.connectStreams(s.ptyRunner.StdIn(), s.ptyStream, "pty-send")
	go s.recvCommands()

	waitErr := s.ptyRunner.Cmd().Wait()
	s.ptyRunner.Exited()
	close(s.exited)
	s.logger.Info("exited", "pid", s.ptyRunner.Pid(),
		"exit_code", s.ptyRunner.Cmd().ProcessState.ExitCode(), "error", waitErr)
	s.logError(s.sendProcState(waitErr))

	s.logger.Debug("waiting for the client to close the connection")
	s.wg.Wait()
	s.logger.Debug("the client closed the connection")

	// the client may have closed the connection already (broken pipe)
	if err := s.ptyStream.Close(); err != nil {
		s.logger.Debug("closing the pty stream", "error", err)
	}
	if err := s.cmdStream.Close(); err != nil {
		s.logger.Debug("closing the command stream", "error", err)
	}

	// the exit status of the command is not an error of the server, the client gets it
	return nil
}

func (s *Server) logError(err error) {
	if err != nil {
		s.logger.Error(err.Error())
	}
}

func (s *Server) requestStop(err error) {
	s.stop.Store(true)
	if err != nil {
		s.logError(err)
		s.terminate.Do(func() {
			go s.terminateProcess()
		})
	}
}

// terminateProcess is called when nobody is served anymore (e.g. the client disconnected).
// It sends SIGINT to the process group, then SIGTERM and finally SIGKILL if the process group is not empty after
// the grace periods. Without this, a process that ignores SIGINT (e.g. an interactive shell) would keep running,
// and it would block as soon as its output buffer is full.
func (s *Server) terminateProcess() {
	steps := []struct {
		sig   os.Signal
		grace time.Duration
	}{
		{os.Interrupt, TerminateGraceInt},
		{syscall.SIGTERM, TerminateGraceTerm},
		{os.Kill, 0},
	}
	// Other processes of the group (e.g. background jobs of a script) may outlive the command itself.
	alive := func() bool {
		select {
		case <-s.exited:
			return s.ptyRunner.GroupAlive()
		default:
			return true
		}
	}
	for _, step := range steps {
		if !alive() {
			return
		}
		err := s.ptyRunner.SignalGroup(step.sig)
		if errors.Is(err, errSignalNotSupported) {
			continue // the next one (on Windows: Kill)
		}
		s.logger.Warn("terminating", "pid", s.ptyRunner.Pid(), "signal", step.sig)
		if err != nil {
			s.logger.Error("could not send signal", "signal", step.sig, "error", err)
			continue
		}
		for deadline := time.Now().Add(step.grace); time.Now().Before(deadline); {
			if !alive() {
				return
			}
			time.Sleep(100 * time.Millisecond)
		}
	}
}

var errInvalidWrite = errors.New("invalid write result")

// connectStreams is similar to io.Copy, but it can be soft-terminated by Server.stop
func (s *Server) connectStreams(dst io.Writer, src io.Reader, channel string) {
	var written int64
	var err error

	defer s.wg.Done()

	buf := make([]byte, BufSize)
	for {
		if s.stop.Load() {
			return
		}

		nr, er := src.Read(buf)

		if s.stop.Load() {
			return
		}

		if nr > 0 {
			nw, ew := dst.Write(buf[0:nr])
			if nw < 0 || nr < nw {
				nw = 0
				if ew == nil {
					ew = errInvalidWrite
				}
			}
			written += int64(nw)
			if ew != nil {
				err = ew
				break
			}
			if nr != nw {
				err = io.ErrShortWrite
				break
			}
		}
		if er != nil {
			if er != io.EOF {
				err = er
			}
			break
		}
	}
	s.logger.Debug("stream closed", "channel", channel, "transferred", bytesize.ByteSize(written))
	// Sometimes we get a "read /dev/ptmx: input/output error" error here when the program exits with errorcode=0
	// This is not io.EOF and it seems that there is no easy way to detect what kind of errors should trigger
	// a full server stop. For this reason, we request a stop with nil error, and hope that the process will
	// exit soon.
	if err != nil {
		s.logger.Debug("stream error", "channel", channel, "error", err)
	}
	s.requestStop(nil) // instead of s.requestStop(err)
}

func (s *Server) recvCommands() {
	defer s.wg.Done()

	ctx := context.Background()

	for !s.stop.Load() {
		code, err := RecvCmdCode(ctx, s.cmdStream)
		if err != nil {
			s.requestStop(fmt.Errorf("could not receive command code: %w", err))
			return
		}
		switch code {
		case MsgResize:
			s.recvResize(ctx)
		case MsgProcState:
			err = s.sendProcState(nil)
			if err != nil {
				s.requestStop(fmt.Errorf("could not send process state: %w", err))
				return
			}
		case MsgProcSignal:
			err = s.processSignalRequest()
			if err != nil {
				s.requestStop(fmt.Errorf("could process signal request: %w", err))
				return
			}
		case MsgDisconnect:
			return
		default:
			s.requestStop(fmt.Errorf("invalid command received: %d", code))
		}
	}

	s.logger.Debug("recvCommands done")
}

func (s *Server) recvResize(ctx context.Context) {
	var ws WinSize
	err := RecvAny(ctx, s.cmdStream, &ws)
	if err != nil {
		s.requestStop(fmt.Errorf("could not receive new pty size: %w", err))
		return
	}
	s.logger.Debug("resize", "size", ws)
	err = s.ptyRunner.Resize(ws)
	if err != nil {
		s.requestStop(fmt.Errorf("could not resize pty: %w", err))
		return
	}
}

func (s *Server) processSignalRequest() error {
	ctx := context.Background()
	var signame string
	err := RecvAny(ctx, s.cmdStream, &signame)
	if err != nil {
		return fmt.Errorf("could not receive signal name: %w", err)
	}

	var e *Error = nil
	for {
		sig, ok := signal.Signals[signame]
		if !ok {
			e = &Error{
				Code:    ErrorInvalidSignal,
				Message: fmt.Sprintf("Invalid signal name: %s", signame),
			}
			break
		}
		if s.ptyRunner == nil || s.ptyRunner.Pid() == 0 {
			e = &Error{
				Code:    ErrorNoProcess,
				Message: "No process is running",
			}
			break
		}
		// to the process group, like Ctrl+C in a terminal: a script and the commands that it waits for get it too
		err = s.ptyRunner.SignalGroup(sig)
		if err != nil {
			e = &Error{
				Code:    ErrorCannotSendSignal,
				Message: err.Error(),
			}
			break
		}

		break
	}
	if e != nil {
		err = s.sendError(ctx, *e)
		if err != nil {
			return fmt.Errorf("could not send signal error: %w", err)
		}
	}
	return nil
}

func (s *Server) sendError(ctx context.Context, e Error) error {
	err := SendCmdCode(ctx, s.cmdStream, MsgError)
	if err != nil {
		return err
	}
	return SendAny(ctx, s.cmdStream, e)
}

func (s *Server) sendProcState(e error) error {
	ctx := context.Background()

	err := SendCmdCode(ctx, s.cmdStream, MsgProcState)
	if err != nil {
		return err
	}
	es := ""
	if e != nil {
		es = e.Error()
	}

	// this returns the state only after the process has exited
	var ps *os.ProcessState = nil
	if s.ptyRunner != nil && s.ptyRunner.Cmd() != nil && s.ptyRunner.Cmd().ProcessState != nil {
		ps = s.ptyRunner.Cmd().ProcessState
	}
	state := GetProcState(ps, es)
	// but we may already know the PID of the process before it exits
	if s.ptyRunner != nil {
		state.Pid = s.ptyRunner.Pid()
	}
	s.logger.Debug("SendProcState", "state", state.String())
	return SendAny(ctx, s.cmdStream, &state)
}
