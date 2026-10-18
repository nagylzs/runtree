package runner

import (
	"fmt"
	"net"
	"strings"
)

// LocalAddress is the address of the runner that is embedded in runtree, the default runner
const LocalAddress = "local"

// The networks of the runner addresses
const (
	NetworkLocal = "local" // the embedded runner
	NetworkTCP   = "tcp"   // an rtrunner daemon
)

// Address is a parsed runner address (the runner property of a node)
type Address struct {
	Network string // NetworkLocal or NetworkTCP
	Host    string // host:port, for NetworkTCP
}

// ParseAddress parses a runner address: "local" (or empty) for the embedded runner, "host:port" or
// "tcp://host:port" for an rtrunner daemon.
func ParseAddress(s string) (Address, error) {
	if s == "" || s == LocalAddress {
		return Address{Network: NetworkLocal}, nil
	}
	host := s
	if scheme, rest, ok := strings.Cut(s, "://"); ok {
		if scheme != NetworkTCP {
			return Address{}, fmt.Errorf("unknown runner %q: the runner is local, host:port or tcp://host:port", s)
		}
		host = rest
	}
	h, port, err := net.SplitHostPort(host)
	if err != nil || h == "" || port == "" {
		return Address{}, fmt.Errorf("invalid runner %q: the runner is local, host:port or tcp://host:port", s)
	}
	return Address{Network: NetworkTCP, Host: host}, nil
}

func (a Address) String() string {
	if a.Network == NetworkLocal {
		return LocalAddress
	}
	return a.Network + "://" + a.Host
}
