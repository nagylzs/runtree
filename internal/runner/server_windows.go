package runner

import (
	"github.com/ActiveState/termtest/conpty"
	"io"
	"os"
	"os/exec"
	"syscall"
)

type windowsPtyRunner struct {
	cpty *conpty.ConPty
	cmd  *exec.Cmd
}

func (w windowsPtyRunner) StdIn() io.WriteCloser {
	return w.cpty.InPipe()
}

func (w windowsPtyRunner) StdOut() io.Reader {
	return w.cpty.OutPipe()
}

func (w windowsPtyRunner) Pty() io.Closer {
	return w.cpty
}

func (w windowsPtyRunner) Pid() int {
	return w.cmd.Process.Pid
}

func (w windowsPtyRunner) Cmd() *exec.Cmd {
	return w.cmd
}

func (w windowsPtyRunner) Resize(ws WinSize) error {
	if w.cpty == nil { // during load
		return nil
	}
	return w.cpty.Resize(ws.Cols, ws.Rows)
}

func (w windowsPtyRunner) Close() error {
	if w.cpty == nil {
		return nil
	}
	return w.cpty.Close()
}

func (s *Server) startPty(ca CmdArgs) (PtyRunner, error) {
	cpty, err := conpty.New(int16(ca.InitialPtySize.Cols), int16(ca.InitialPtySize.Rows))
	if err != nil {
		return nil, err
	}

	pid, _, err := cpty.Spawn(
		ca.Args[0],
		ca.Args[1:],
		&syscall.ProcAttr{
			Env: ca.Env,
			Dir: ca.Cwd,
		},
	)
	if err != nil {
		return nil, err
	}

	cmd := &exec.Cmd{}
	process, err := os.FindProcess(pid)
	if err != nil {
		return nil, err
	}
	cmd.Process = process
	pr := &windowsPtyRunner{
		cpty: cpty,
		cmd:  cmd,
	}

	return pr, nil
}
