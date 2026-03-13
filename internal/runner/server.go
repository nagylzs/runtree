package runner

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"sync"
	"sync/atomic"

	"github.com/inhies/go-bytesize"
	"github.com/nagylzs/runtree/internal/signal"
	"github.com/xtaci/smux"
)

const BufSize = 32768 // L3 cache size...

type PtyRunner interface {
	StdIn() io.WriteCloser
	StdOut() io.Reader
	Pty() io.Closer
	Pid() int
	Cmd() *exec.Cmd
	Resize(ws WinSize) error
	Close() error
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
}

// RunNewServer creates a new server for the given connection, and runs it until the client disconnects.
// Should be called as a goroutine.
func RunNewServer(ctx context.Context, conn io.ReadWriteCloser, logger *slog.Logger) error {
	logger.Info("NEGOTIATE")
	err := negotiateVersion(ctx, conn)
	if err != nil {
		return err
	}

	srv := &Server{
		ptyRunner: nil,
		conn:      conn,
		ptyStream: nil,
		cmdStream: nil,
		logger:    logger,
		wg:        &sync.WaitGroup{},
		stop:      &atomic.Bool{},
	}
	err = srv.Serve(ctx)
	if err != nil {
		logger.Error(err.Error())
		return err
	}

	return nil
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

	s.logger.Info("MUX")
	session, err := smux.Server(s.conn, nil)
	if err != nil {
		return err
	}

	defer func() {
		err := session.Close()
		if err != nil {
			s.logger.Error(err.Error())
		}
		err = s.conn.Close()
		if err != nil {
			s.logger.Error(err.Error())
		}
		defer s.logger.Info("DISCONNECT")
	}()

	ptyStream, err := session.AcceptStream()
	if err != nil {
		return err
	}
	cmdStream, err := session.AcceptStream()
	if err != nil {
		ptyStream.Close()
		s.logger.Error(err.Error())
		return err
	}
	s.ptyStream, s.cmdStream = ptyStream, cmdStream

	var ca CmdArgs
	err = RecvAny(ctx, s.cmdStream, &ca)
	if err != nil {
		return err
	}

	s.logger.Debug("ServePty", "cmd", ca)

	if ca.InheritSysEnvs {
		ca.Env = append(os.Environ(), ca.Env...)
	}

	ptyRunner, err := s.startPty(ca)
	if err != nil {
		return err
	}
	s.ptyRunner = ptyRunner
	defer func() {
		err := ptyRunner.Close()
		if err != nil {
			s.logger.Error("PtyRunner.Close", "err", err)
		}
	}()

	s.logger.Info("started", "pid", ptyRunner.Pid())

	s.stop.Store(false)
	s.logError(s.sendProcState(err))

	s.wg.Add(3)
	go s.connectStreams(s.ptyStream, s.ptyRunner.StdOut(), "pty-recv")
	go s.connectStreams(s.ptyRunner.StdIn(), s.ptyStream, "pty-send")
	go s.recvCommands()

	err = s.ptyRunner.Cmd().Wait()
	s.logger.Warn("exited", "pid", s.ptyRunner.Pid(),
		"exitcode", s.ptyRunner.Cmd().ProcessState.ExitCode(), "error", err)
	s.logError(s.sendProcState(err))

	s.logger.Info("waiting for client to close connection...")
	s.wg.Wait()
	s.logger.Info("client closed")

	s.logger.Debug("closing Serve()...")
	s.logError(s.ptyStream.Close())
	s.logError(s.cmdStream.Close())

	return err
}

func (s *Server) logError(err error) {
	if err != nil {
		s.logger.Error(err.Error())
	}
}

func (s *Server) requestStop(err error) {
	s.stop.Store(true)
	if err != nil {
		_ = s.ptyRunner.Cmd().Process.Signal(os.Interrupt)
		s.logError(err)
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
		s.logger.Warn("connectStreams", "error", err.Error())
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
	slog.Info("resize", "size", ws)
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
				Message: fmt.Sprintf("No process is running, pid=%d", s.ptyRunner.Pid()),
			}
			break
		}
		process, err := os.FindProcess(s.ptyRunner.Pid())
		if err != nil {
			e = &Error{
				Code: ErrorNoProcess,
				Message: fmt.Sprintf("Clould not find process, pid=%d, err=%s",
					s.ptyRunner.Pid(), err.Error()),
			}
			break
		}
		err = process.Signal(sig)
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
