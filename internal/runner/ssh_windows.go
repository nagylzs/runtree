package runner

import (
	"errors"
	"io"
	"net"
	"os/exec"
)

// startSSH is not implemented on Windows (runtree does not build on Windows, only rtrunner)
func startSSH(string, []string, io.Writer) (net.Conn, *exec.Cmd, error) {
	return nil, nil, errors.New("the ssh runner is not available on Windows")
}

func sshCmd(name string, args []string) *exec.Cmd {
	return exec.Command(name, args...)
}
