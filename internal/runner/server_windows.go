package runner

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/ActiveState/termtest/conpty"
	"golang.org/x/sys/windows"
)

type windowsPtyRunner struct {
	cpty  *conpty.ConPty
	cmd   *exec.Cmd
	close sync.Once
}

// conptyDrain is the time for the pseudo console to write the rest of the output after the process exited
const conptyDrain = 500 * time.Millisecond

var procSetConsoleCtrlHandler = windows.NewLazySystemDLL("kernel32.dll").NewProc("SetConsoleCtrlHandler")

// handleCtrlC clears the flag of this process that it ignores Ctrl+C. The processes it starts inherit the flag, and then
// the Ctrl+C typed into their pseudo console (a timeout types it) does not stop them. Under sshd every process has it.
func handleCtrlC() error {
	if r, _, err := procSetConsoleCtrlHandler.Call(0, 0); r == 0 {
		return err
	}
	return nil
}

func (w *windowsPtyRunner) StdIn() io.WriteCloser {
	return w.cpty.InPipe()
}

func (w *windowsPtyRunner) StdOut() io.Reader {
	return w.cpty.OutPipe()
}

func (w *windowsPtyRunner) Pty() io.Closer {
	return w.cpty
}

func (w *windowsPtyRunner) Pid() int {
	return w.cmd.Process.Pid
}

func (w *windowsPtyRunner) Cmd() *exec.Cmd {
	return w.cmd
}

func (w *windowsPtyRunner) Resize(ws WinSize) error {
	if w.cpty == nil { // during load
		return nil
	}
	return w.cpty.Resize(ws.Cols, ws.Rows)
}

// SignalGroup supports SIGINT and SIGKILL on Windows: SIGINT types Ctrl+C into the pseudo console (the console sends
// CTRL_C_EVENT to its processes, it stops most of the console programs), SIGKILL kills the process itself. Windows has
// no other signals.
func (w *windowsPtyRunner) SignalGroup(sig os.Signal) error {
	switch sig {
	case os.Interrupt:
		_, err := w.cpty.InPipe().Write(CtrlC)
		return err
	case os.Kill:
		return w.cmd.Process.Kill()
	}
	return fmt.Errorf("%v: %w", sig, errSignalNotSupported)
}

// GroupAlive always returns false on Windows: there are no process groups, only the process itself is tracked.
func (w *windowsPtyRunner) GroupAlive() bool {
	return false
}

// Exited closes the pseudo console a bit later: its output does not end while it is open (the server waits for the
// end of the output)
func (w *windowsPtyRunner) Exited() {
	go func() {
		time.Sleep(conptyDrain)
		_ = w.Close()
	}()
}

func (w *windowsPtyRunner) Close() error {
	if w.cpty == nil {
		return nil
	}
	var err error
	w.close.Do(func() { err = w.cpty.Close() })
	return err
}

func (s *Server) startPty(ca CmdArgs) (PtyRunner, error) {
	// conpty.New fails with 0 columns or rows (a pty on Unix does not), e.g. when the terminal of the GUI has no size
	cols, rows := ca.InitialPtySize.Cols, ca.InitialPtySize.Rows
	if cols == 0 {
		cols = 80
	}
	if rows == 0 {
		rows = 24
	}
	cpty, err := conpty.New(int16(cols), int16(rows))
	if err != nil {
		return nil, err
	}

	// CreateProcess does not look for the program in the PATH (a path is relative to the working directory, Spawn
	// joins them), and the command line has the program too
	name := ca.Args[0]
	if !strings.ContainsAny(name, `/\:`) {
		if name, err = exec.LookPath(name); err != nil {
			_ = cpty.Close()
			return nil, err
		}
	}
	if err := handleCtrlC(); err != nil {
		s.logger.Warn("the processes will ignore Ctrl+C", "error", err)
	}
	pid, _, err := cpty.Spawn(
		name,
		ca.Args,
		&syscall.ProcAttr{
			Env: ca.Env,
			Dir: ca.Cwd,
		},
	)
	if err != nil {
		_ = cpty.Close()
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
