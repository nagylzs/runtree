package runner

import (
	"context"
	"fmt"
	"io"
	"os"
	"time"
)

type CmdCode uint16

var Version = "1.0.1"

const MsgVersion CmdCode = 53455

const (
	MsgStartPty CmdCode = iota
	MsgResize
	MsgProcState
	MsgDisconnect
	MsgProcSignal
	MsgError
)

// WinSize represents the size of a pseudo-terminal
type WinSize struct {
	Rows   uint16 `msgpack:"r,omitempty"`
	Cols   uint16 `msgpack:"c,omitempty"`
	Width  uint16 `msgpack:"w,omitempty"`
	Height uint16 `msgpack:"h,omitempty"`
}

func (w WinSize) String() string {
	return fmt.Sprintf("WinSize(%dx%d,%dx%d)", w.Cols, w.Rows, w.Width, w.Height)
}

type ErrorCode = int

const (
	ErrorInvalidSignal ErrorCode = iota
	ErrorNoProcess
	ErrorCannotSendSignal
)

type Error struct {
	Code    ErrorCode
	Message string
}

type CmdArgs struct {
	Name           string
	Args           []string
	Env            []string
	InheritSysEnvs bool
	Cwd            string

	// pty size for startup
	InitialPtySize WinSize
}

type ProcState struct {
	Pid        int           `msgpack:"p,omitempty"`
	Exited     bool          `msgpack:"e,omitempty"`
	ExitCode   int           `msgpack:"c,omitempty"`
	SystemTime time.Duration `msgpack:"s,omitempty"`
	UserTime   time.Duration `msgpack:"u,omitempty"`
	Error      string        `msgpack:"err,omitempty"`
}

// Alive tells if the underlying process is still alive.
// Please note that Exited is false when the process exited using a signal.
// It is also not considered alive, if the process could not be started at all.
// in that case, Error!="" and technically it is not Exited and there is no ExitCode
func (p *ProcState) Alive() bool {
	return !p.Exited && p.ExitCode == 0 && p.Error == ""
}

func (p *ProcState) String() string {
	return fmt.Sprintf("ProcState(Pid=%d Exited=%v ExitCode=%v SystemTime=%v UserTime=%v Error=%q)",
		p.Pid, p.Exited, p.ExitCode, p.SystemTime, p.UserTime, p.Error)
}

func GetProcState(ps *os.ProcessState, err string) ProcState {
	if ps == nil {
		return ProcState{
			Error: err,
		}
	}
	return ProcState{
		Pid:        ps.Pid(),
		Exited:     ps.Exited(),
		ExitCode:   ps.ExitCode(),
		SystemTime: ps.SystemTime(),
		UserTime:   ps.UserTime(),
		Error:      err,
	}
}

func negotiateVersion(ctx context.Context, stream io.ReadWriter) error {
	// handshake
	err := SendCmdCode(ctx, stream, MsgVersion)
	if err != nil {
		return err
	}
	code, err := RecvCmdCode(ctx, stream)
	if err != nil {
		return err
	}
	if code != MsgVersion {
		return fmt.Errorf("protocol error")
	}

	err = SendString(ctx, stream, Version)
	if err != nil {
		return err
	}
	remoteVersion, err := RecvString(ctx, stream)
	if err != nil {
		return err
	}
	if Version != remoteVersion {
		return fmt.Errorf("incompatible versions, local=%s remote=%s", remoteVersion, Version)
	}
	return nil
}
