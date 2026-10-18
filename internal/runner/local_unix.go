//go:build !windows

package runner

import (
	"net"
	"os"
	"syscall"
)

// socketPair returns the two ends of a connected unix socket pair
func socketPair() (net.Conn, net.Conn, error) {
	// like os/exec, so that the processes started meanwhile do not inherit them
	syscall.ForkLock.RLock()
	fds, err := syscall.Socketpair(syscall.AF_UNIX, syscall.SOCK_STREAM, 0)
	if err == nil {
		syscall.CloseOnExec(fds[0])
		syscall.CloseOnExec(fds[1])
	}
	syscall.ForkLock.RUnlock()
	if err != nil {
		return nil, nil, os.NewSyscallError("socketpair", err)
	}
	c1, err := fileConn(fds[0], "runner-client")
	if err != nil {
		_ = syscall.Close(fds[1])
		return nil, nil, err
	}
	c2, err := fileConn(fds[1], "runner-server")
	if err != nil {
		_ = c1.Close()
		return nil, nil, err
	}
	return c1, c2, nil
}

// fileConn returns a net.Conn for the socket fd, and closes fd (net.FileConn uses a copy of it)
func fileConn(fd int, name string) (net.Conn, error) {
	f := os.NewFile(uintptr(fd), name)
	defer f.Close()
	return net.FileConn(f)
}
