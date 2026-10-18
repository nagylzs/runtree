package runner

import (
	"errors"
	"net"
)

// socketPair is not implemented on Windows (runtree does not build on Windows, only rtrunner)
func socketPair() (net.Conn, net.Conn, error) {
	return nil, nil, errors.New("the local runner is not available on Windows, use an rtrunner (host:port)")
}
