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

const addressHelp = "the runner is local, host:port, tcp://host:port or ssh://[user@]host[:port][/path/to/rtrunner]"

// Address is a parsed runner address (the runner property of a node)
type Address struct {
	Network string // NetworkLocal, NetworkTCP or NetworkSSH
	Host    string // host:port for NetworkTCP, the host for NetworkSSH
	User    string // NetworkSSH: the user on the host, empty for the default of ssh
	Port    string // NetworkSSH: empty for the default of ssh
	Path    string // NetworkSSH: the rtrunner command on the host, empty when it is managed (see Managed)
}

// Managed tells if runtree uploads rtrunner to the host of the ssh runner when it is not there (the address has no
// path), see upload.go
func (a Address) Managed() bool {
	return a.Network == NetworkSSH && a.Path == ""
}

// ParseAddress parses a runner address: "local" (or empty) for the embedded runner, "host:port" or
// "tcp://host:port" for an rtrunner daemon, and ssh://[user@]host[:port][/path/to/rtrunner] for an rtrunner that is
// started with ssh (/~/ is the home directory of the user, /C:/ is a drive on Windows). Without a path, runtree uploads
// rtrunner (Managed).
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
	case a.Path == "/":
		a.Path = ""
	case strings.HasPrefix(a.Path, "/~/"):
		a.Path = a.Path[1:] // in the home directory
	case windowsDrivePath(a.Path):
		a.Path = a.Path[1:] // C:/path/to/rtrunner.exe on Windows
	}
	return a, nil
}

// windowsDrivePath tells if the path of an ssh address starts with a drive letter of Windows: /C:/...
func windowsDrivePath(p string) bool {
	return len(p) >= 4 && p[0] == '/' && (p[1] >= 'A' && p[1] <= 'Z' || p[1] >= 'a' && p[1] <= 'z') && p[2] == ':' &&
		p[3] == '/'
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
		if a.Path != "" && !strings.HasPrefix(a.Path, "/") { // ~/... or C:/...
			s += "/"
		}
		s += a.Path
		return s
	}
	return a.Network + "://" + a.Host
}
