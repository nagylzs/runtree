//go:build !windows
// +build !windows

package runner

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"

	"github.com/creack/pty"
)

type unixPtyRunner struct {
	tty *os.File
	cmd *exec.Cmd
}

func (u unixPtyRunner) StdIn() io.WriteCloser {
	return u.tty
}

func (u unixPtyRunner) StdOut() io.Reader {
	return u.tty
}

func (u unixPtyRunner) Pty() io.Closer {
	return u.tty
}

func (u unixPtyRunner) Pid() int {
	return u.cmd.Process.Pid
}

func (u unixPtyRunner) Cmd() *exec.Cmd {
	return u.cmd
}

func (u unixPtyRunner) Resize(ws WinSize) error {
	pws := wsToPty(ws)
	return pty.Setsize(u.tty, &pws)
}

func (u unixPtyRunner) Close() error {
	if u.tty == nil {
		return nil
	}
	return u.tty.Close()
}

func (s *Server) startPty(ca CmdArgs) (PtyRunner, error) {
	cmd := exec.Command(ca.Name)
	cmd.Args = ca.Args
	cmd.Env = ca.Env
	cmd.Dir = ca.Cwd

	// Start the command
	tty, err := pty.Start(cmd)
	if err != nil {
		s.logger.Error("Cannot start pty", "error", err.Error())
		if tty != nil {
			buf := bytes.NewBuffer(nil)
			_, err2 := io.Copy(buf, tty)
			if err2 != nil {
				s.logger.Error("Could not read tty", "error", err2.Error())
			}
			s.logger.Error("tty", string(buf.Bytes()))
		}
		s.logError(s.sendProcState(err))
		return nil, err
	}
	pws := wsToPty(ca.InitialPtySize)
	err = pty.Setsize(tty, &pws)
	if err != nil {
		return nil, fmt.Errorf("could not resize pty: %w", err)
	}

	return &unixPtyRunner{
		tty: tty,
		cmd: cmd,
	}, nil
}

func wsToPty(ws WinSize) pty.Winsize {
	return pty.Winsize{
		Rows: ws.Rows,
		Cols: ws.Cols,
		X:    ws.Width,
		Y:    ws.Height,
	}
}
