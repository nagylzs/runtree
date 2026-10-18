package runner

import (
	"net"
	"time"
)

// socketPair returns the two ends of a loopback TCP connection (there are no socket pairs on Windows). Only the
// connection of the returned client end is accepted: another process that connects meanwhile does not get a runner.
func socketPair() (net.Conn, net.Conn, error) {
	l, err := net.ListenTCP("tcp", &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		return nil, nil, err
	}
	defer l.Close()
	if err := l.SetDeadline(time.Now().Add(HandshakeTimeout)); err != nil {
		return nil, nil, err
	}
	client, err := net.DialTCP("tcp", nil, l.Addr().(*net.TCPAddr))
	if err != nil {
		return nil, nil, err
	}
	for {
		server, err := l.AcceptTCP()
		if err != nil {
			_ = client.Close()
			return nil, nil, err
		}
		if server.RemoteAddr().String() == client.LocalAddr().String() {
			return client, server, nil
		}
		_ = server.Close()
	}
}
