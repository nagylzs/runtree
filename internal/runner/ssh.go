package runner

import (
	"fmt"
	"log/slog"
	"net"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"
)

// The ssh runner: for each connection, ssh starts rtrunner --stdio on the host. ssh uses ~/.ssh/config, the keys and
// ssh-agent of the user, but it cannot ask for passwords or passphrases (BatchMode), and the host key must be known.

// sshCommand is the ssh client (the tests replace it)
var sshCommand = []string{"ssh"}

// SSHHandshakeTimeout is the time for an ssh runner to connect, log in and answer the protocol handshake
const SSHHandshakeTimeout = 2 * time.Minute

// sshArgs returns the arguments of ssh that start rtrunner --stdio for the address
func sshArgs(a Address) []string {
	args := []string{"-T", "-o", "BatchMode=yes"}
	if a.Port != "" {
		args = append(args, "-p", a.Port)
	}
	// ssh passes the command to the shell of the user
	return append(args, "--", sshDest(a), shellQuote(a.Path), "--stdio")
}

// sshDest is the destination of ssh: [user@]host
func sshDest(a Address) string {
	if a.User != "" {
		return a.User + "@" + a.Host
	}
	return a.Host
}

var shellSafe = regexp.MustCompile(`^[A-Za-z0-9_./~+-]+$`)

// shellQuote quotes s for a POSIX shell, unless it is safe as it is (e.g. ~/bin/rtrunner, where ~ is expanded)
func shellQuote(s string) string {
	if shellSafe.MatchString(s) {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// sshConn is a connection to rtrunner --stdio, through the standard input and output of ssh
type sshConn struct {
	net.Conn
	dest   string
	stderr *tailWriter
	exited chan struct{}    // closed when ssh has exited
	state  *os.ProcessState // the exit status of ssh, after exited is closed
}

// dialSSH starts ssh, and returns the connection to rtrunner
func dialSSH(a Address, logger *slog.Logger) (*sshConn, error) {
	args := append(sshCommand[1:len(sshCommand):len(sshCommand)], sshArgs(a)...)
	stderr := &tailWriter{max: 4096, logger: logger}
	conn, cmd, err := startSSH(sshCommand[0], args, stderr)
	if err != nil {
		return nil, fmt.Errorf("could not start ssh: %w", err)
	}
	c := &sshConn{Conn: conn, dest: sshDest(a), stderr: stderr, exited: make(chan struct{})}
	go func() {
		_ = cmd.Wait()
		c.state = cmd.ProcessState
		close(c.exited)
	}()
	return c, nil
}

// failure returns the error of a failed handshake, with the error messages of ssh (e.g. "Permission denied")
func (c *sshConn) failure(err error) error {
	// ssh exits soon when it could not connect or start rtrunner
	select {
	case <-c.exited:
	case <-time.After(5 * time.Second):
	}
	msg := c.stderr.lastLines(3)
	if msg == "" {
		return fmt.Errorf("ssh %s: %w", c.dest, err)
	}
	select {
	case <-c.exited:
		if c.state != nil && c.state.ExitCode() == 127 {
			msg += " (is rtrunner installed on the host? Its path can be given: ssh://host/path/to/rtrunner)"
		}
	default:
	}
	return fmt.Errorf("ssh %s: %s", c.dest, msg)
}

// tailWriter keeps the end of what is written to it (the standard error of ssh), and logs it at debug level
type tailWriter struct {
	lock   sync.Mutex
	buf    []byte
	max    int
	logger *slog.Logger
}

func (w *tailWriter) Write(p []byte) (int, error) {
	w.logger.Debug("ssh", "stderr", strings.TrimRight(string(p), "\r\n"))
	w.lock.Lock()
	defer w.lock.Unlock()
	w.buf = append(w.buf, p...)
	if len(w.buf) > w.max {
		w.buf = w.buf[len(w.buf)-w.max:]
	}
	return len(p), nil
}

// lastLines returns the last n non-empty lines, separated by "; "
func (w *tailWriter) lastLines(n int) string {
	w.lock.Lock()
	defer w.lock.Unlock()
	var lines []string
	for _, line := range strings.Split(string(w.buf), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			lines = append(lines, line)
		}
	}
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "; ")
}
