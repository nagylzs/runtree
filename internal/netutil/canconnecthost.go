package netutil

import (
	"fmt"
	"net"
	"time"
)

// CanConnectHost checks if a hostname can be TCP-connected within the given timeout (seconds)
func CanConnectHost(hostname string, port uint16, timeout time.Duration) (bool, error) {
	// Resolve the hostname to an IP address
	ipAddr, err := net.ResolveIPAddr("ip", hostname)
	if err != nil {
		return false, fmt.Errorf("CanConnectHost: failed to resolve hostname: %w", err)
	}

	// Create a TCP connection to the host with the given timeout
	conn, err := net.DialTimeout("tcp", fmt.Sprintf("%s:%d", ipAddr.String(), port), timeout)
	if err != nil {
		return false, nil
	}
	_ = conn.Close()
	return true, nil
}
