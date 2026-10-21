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
	"runtime"
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

// unsupportedSignals are the signals that the runners of this process act as if they did not have, see
// SetUnsupportedSignals
var unsupportedSignals sync.Map

// SetUnsupportedSignals makes the runners of this process answer the requests of the clients for these signals (e.g.
// "SIGTERM") as if the system did not have them, like Windows. It is for the tests of the clients.
func SetUnsupportedSignals(names ...string) {
	unsupportedSignals.Clear()
	for _, name := range names {
		unsupportedSignals.Store(name, true)
	}
}

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
		ca.Env = append(environ(), ca.Env...)
	}

	err = checkDir(ca.Cwd)
	var ptyRunner PtyRunner
	if err == nil {
		ptyRunner, err = s.startPty(ca)
	}
	if err != nil {
		// the error of the start is not an error of the server, the client gets it
		s.startFailed(ca.Args, err)
		return nil
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

// checkDir returns an error when the working directory of the command does not exist: the error of the start would not
// tell it (on Unix it is "fork/exec PROGRAM: no such file or directory", on Windows "The directory name is invalid.").
// os.StartProcess checks it the same way, but not with the SysProcAttr of the pty.
func checkDir(dir string) error {
	if dir == "" {
		return nil
	}
	fi, err := os.Stat(dir)
	if err != nil {
		var pe *os.PathError
		if errors.As(err, &pe) {
			pe.Op = "chdir"
		}
		return err
	}
	if !fi.IsDir() {
		// not syscall.ENOTDIR: on Windows that is "The system cannot find the path specified."
		return fmt.Errorf("chdir %s: not a directory", dir)
	}
	return nil
}

// startFailed sends the error of the start to the client as the state of the process, and receives its commands until
// it disconnects: as after an exit, the server must not close the connection first (see recvCommands)
func (s *Server) startFailed(args []string, err error) {
	s.logger.Error("could not start the command", "args", args, "error", err)
	s.logError(s.sendProcState(err))
	s.wg.Add(1)
	go s.recvCommands()
	s.wg.Wait()
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
	if s.ptyRunner == nil { // it could not be started
		return
	}
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

// connectStreams is similar to io.Copy, but it can be soft-terminated by Server.stop: it returns after the read that
// finds it set. It reads before checking: the end of the output of a process that exits quickly may set it before
// the input (pty-send) starts, and that one must still wait for the client to close the pty stream (see ServePty).
func (s *Server) connectStreams(dst io.Writer, src io.Reader, channel string) {
	var written int64
	var err error

	defer s.wg.Done()

	buf := make([]byte, BufSize)
	for {
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

// recvCommands receives the commands of the client until it disconnects (MsgDisconnect, or the stream is closed),
// also after the process exited: the server must not close the connection before the client, or the client may lose
// the end of the output and the exit status (smux drops the data that it has not read yet when the connection is
// closed).
func (s *Server) recvCommands() {
	defer s.wg.Done()

	ctx := context.Background()

	for {
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
			s.logger.Debug("the client disconnects")
			return
		default:
			s.requestStop(fmt.Errorf("invalid command received: %d", code))
			return
		}
	}
}

func (s *Server) recvResize(ctx context.Context) {
	var ws WinSize
	err := RecvAny(ctx, s.cmdStream, &ws)
	if err != nil {
		s.requestStop(fmt.Errorf("could not receive new pty size: %w", err))
		return
	}
	s.logger.Debug("resize", "size", ws)
	if s.ptyRunner == nil { // it could not be started
		return
	}
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
		if _, ok := unsupportedSignals.Load(signame); ok {
			err = errSignalNotSupported
		} else {
			err = s.ptyRunner.SignalGroup(sig)
		}
		if errors.Is(err, errSignalNotSupported) {
			e = &Error{
				Code:    ErrorSignalNotSupported,
				Message: signame + " is not supported on this system",
			}
			break
		}
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
	state.System = runtime.GOOS
	// but we may already know the PID of the process before it exits
	if s.ptyRunner != nil {
		state.Pid = s.ptyRunner.Pid()
	}
	s.logger.Debug("SendProcState", "state", state.String())
	return SendAny(ctx, s.cmdStream, &state)
}
