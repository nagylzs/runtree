//go:build !windows

package runner

import (
	"io"
	"net"
	"os/exec"
	"syscall"
)

// startSSH starts ssh with its standard input and output connected to the returned connection
func startSSH(name string, args []string, stderr io.Writer) (net.Conn, *exec.Cmd, error) {
	conn, f, err := socketPairFile()
	if err != nil {
		return nil, nil, err
	}
	defer f.Close() // ssh has its copy
	cmd := exec.Command(name, args...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = f, f, stderr
	// its own session: Ctrl+C in the terminal of runtree does not reach it, and it has no terminal to ask for passwords
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		_ = conn.Close()
		return nil, nil, err
	}
	return conn, cmd, nil
}
