//go:build !windows

package runner

import (
	"net"
	"os"
	"syscall"
)

// socketPair returns the two ends of a connected unix socket pair
func socketPair() (net.Conn, net.Conn, error) {
	c1, f, err := socketPairFile()
	if err != nil {
		return nil, nil, err
	}
	c2, err := fileConn(f)
	if err != nil {
		_ = c1.Close()
		return nil, nil, err
	}
	return c1, c2, nil
}

// socketPairFile returns the two ends of a connected unix socket pair, the second one as a file, e.g. for the standard
// input and output of a process
func socketPairFile() (net.Conn, *os.File, error) {
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
	f := os.NewFile(uintptr(fds[1]), "runner-server")
	c, err := fileConn(os.NewFile(uintptr(fds[0]), "runner-client"))
	if err != nil {
		_ = f.Close()
		return nil, nil, err
	}
	return c, f, nil
}

// fileConn returns a net.Conn for the socket file, and closes the file (net.FileConn uses a copy of it)
func fileConn(f *os.File) (net.Conn, error) {
	defer f.Close()
	return net.FileConn(f)
}
