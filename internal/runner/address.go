package runner

import (
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
)

// LocalAddress is the address of the runner that is embedded in runtree, the default runner
const LocalAddress = "local"

// The networks of the runner addresses
const (
	NetworkLocal = "local" // the embedded runner
	NetworkTCP   = "tcp"   // an rtrunner daemon
	NetworkSSH   = "ssh"   // rtrunner --stdio, started by ssh for each connection
)

// DefaultRemoteRunner is the command that an ssh runner starts on the host, when its address has no path
const DefaultRemoteRunner = "rtrunner"

const addressHelp = "the runner is local, host:port, tcp://host:port or ssh://[user@]host[:port][/path/to/rtrunner]"

// Address is a parsed runner address (the runner property of a node)
type Address struct {
	Network string // NetworkLocal, NetworkTCP or NetworkSSH
	Host    string // host:port for NetworkTCP, the host for NetworkSSH
	User    string // NetworkSSH: the user on the host, empty for the default of ssh
	Port    string // NetworkSSH: empty for the default of ssh
	Path    string // NetworkSSH: the rtrunner command on the host
}

// ParseAddress parses a runner address: "local" (or empty) for the embedded runner, "host:port" or
// "tcp://host:port" for an rtrunner daemon, and ssh://[user@]host[:port][/path/to/rtrunner] for an rtrunner that is
// started with ssh (the path is DefaultRemoteRunner by default, /~/ is the home directory of the user).
func ParseAddress(s string) (Address, error) {
	if s == "" || s == LocalAddress {
		return Address{Network: NetworkLocal}, nil
	}
	host := s
	if scheme, rest, ok := strings.Cut(s, "://"); ok {
		switch scheme {
		case NetworkTCP:
			host = rest
		case NetworkSSH:
			return parseSSHAddress(s)
		default:
			return Address{}, fmt.Errorf("unknown runner %q: %s", s, addressHelp)
		}
	}
	h, port, err := net.SplitHostPort(host)
	if n, perr := strconv.Atoi(port); err != nil || h == "" || perr != nil || n < 1 || n > 65535 {
		return Address{}, fmt.Errorf("invalid runner %q: %s", s, addressHelp)
	}
	return Address{Network: NetworkTCP, Host: host}, nil
}

func parseSSHAddress(s string) (Address, error) {
	u, err := url.Parse(s)
	invalid := func(why string) (Address, error) {
		return Address{}, fmt.Errorf("invalid runner %q: %s", s, why)
	}
	if err != nil {
		return invalid(addressHelp)
	}
	a := Address{Network: NetworkSSH, Host: u.Hostname(), Port: u.Port(), Path: u.Path}
	if u.User != nil {
		a.User = u.User.Username()
		if _, ok := u.User.Password(); ok {
			return invalid("passwords are not supported, use keys (ssh-agent)")
		}
	}
	if a.Host == "" || u.RawQuery != "" || u.Fragment != "" || u.Opaque != "" {
		return invalid(addressHelp)
	}
	// they would be options of ssh
	if strings.HasPrefix(a.Host, "-") || strings.HasPrefix(a.User, "-") {
		return invalid("the user and the host cannot start with -")
	}
	if strings.HasSuffix(u.Host, ":") {
		return invalid("the port is missing after :")
	}
	switch {
	case a.Path == "" || a.Path == "/":
		a.Path = DefaultRemoteRunner
	case strings.HasPrefix(a.Path, "/~/"):
		a.Path = a.Path[1:] // in the home directory
	}
	return a, nil
}

func (a Address) String() string {
	switch a.Network {
	case NetworkLocal:
		return LocalAddress
	case NetworkSSH:
		s := "ssh://"
		if a.User != "" {
			s += a.User + "@"
		}
		s += a.Host
		if a.Port != "" {
			s += ":" + a.Port
		}
		if a.Path != DefaultRemoteRunner {
			if strings.HasPrefix(a.Path, "~/") {
				s += "/"
			}
			s += a.Path
		}
		return s
	}
	return a.Network + "://" + a.Host
}
