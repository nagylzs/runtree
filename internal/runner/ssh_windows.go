package runner

import (
	"io"
	"net"
	"os"
	"os/exec"
	"syscall"

	"golang.org/x/sys/windows"
)

// sshShareSupported tells whether ssh can share connections: the OpenSSH of Windows has no ControlMaster (it fails
// with "getsockname failed: Not a socket")
const sshShareSupported = false

// startSSH starts ssh with its standard input and output connected to the returned connection. There are no socket
// pairs on Windows: the connection is an in-memory pipe, copied to and from the pipes of ssh.
func startSSH(name string, args []string, stderr io.Writer) (net.Conn, *exec.Cmd, error) {
	inR, inW, err := os.Pipe()
	if err != nil {
		return nil, nil, err
	}
	outR, outW, err := os.Pipe()
	if err != nil {
		_ = inR.Close()
		_ = inW.Close()
		return nil, nil, err
	}
	cmd := sshCmd(name, args)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = inR, outW, stderr
	err = cmd.Start()
	// ssh has its copies
	_ = inR.Close()
	_ = outW.Close()
	if err != nil {
		_ = inW.Close()
		_ = outR.Close()
		return nil, nil, err
	}
	conn, end := net.Pipe()
	go func() {
		// the end of the connection is the end of the input of ssh: then rtrunner exits on the host, and ssh exits
		_, _ = io.Copy(inW, end)
		_ = inW.Close()
	}()
	go func() {
		// the end of the output of ssh (it exited) is the end of the connection
		_, _ = io.Copy(end, outR)
		_ = outR.Close()
		_ = end.Close()
	}()
	return conn, cmd, nil
}

// sshCmd returns the command of ssh. It has its own console without a window, in a new process group: Ctrl+C and
// Ctrl+Break in the console of runtree do not reach it (nor its ProxyCommand), and no console window opens.
func sshCmd(name string, args []string) *exec.Cmd {
	cmd := exec.Command(name, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP | windows.CREATE_NO_WINDOW}
	return cmd
}
