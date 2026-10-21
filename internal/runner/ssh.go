package runner

import (
	"fmt"
	"log/slog"
	"net"
	"os"
	"path/filepath"
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

// sshOptions returns the options of ssh for the address
func sshOptions(a Address) []string {
	args := []string{"-T", "-o", "BatchMode=yes"}
	if dir := controlDir(); dir != "" {
		// one connection for each host (user, port): the first ssh starts a master in the background, the others use
		// it, and it exits when it was not used for a minute. Above the MaxSessions of sshd (10 by default), ssh
		// makes a new connection.
		args = append(args, "-o", "ControlMaster=auto", "-o", "ControlPath="+filepath.Join(dir, "%C"),
			"-o", "ControlPersist=60")
	}
	if a.Port != "" {
		args = append(args, "-p", a.Port)
	}
	return args
}

var sshShare = struct {
	lock     sync.Mutex
	disabled bool
	dir      *string // nil until it is created
}{}

// SetSSHShare enables or disables the sharing of one ssh connection for each host (it is enabled by default)
func SetSSHShare(enabled bool) {
	sshShare.lock.Lock()
	defer sshShare.lock.Unlock()
	sshShare.disabled = !enabled
	sshShare.dir = nil
}

// maxSocketPath is the length of the longest unix socket path (sun_path is 108 bytes on Linux, 104 on macOS)
const maxSocketPath = 103

// controlDir returns the private directory of the control sockets of the shared ssh connections, or "" when they are
// not shared (never on Windows, see sshShareSupported): $XDG_RUNTIME_DIR/runtree, or the cache directory of the user
// (e.g. on macOS)
func controlDir() string {
	sshShare.lock.Lock()
	defer sshShare.lock.Unlock()
	if sshShare.disabled || !sshShareSupported {
		return ""
	}
	if sshShare.dir != nil {
		return *sshShare.dir
	}
	dir := ""
	if run := os.Getenv("XDG_RUNTIME_DIR"); run != "" {
		dir = filepath.Join(run, "runtree")
	} else if cache, err := os.UserCacheDir(); err == nil {
		dir = filepath.Join(cache, "runtree", "ssh")
	}
	// %C is 40 characters
	if dir != "" && (len(dir)+41 > maxSocketPath || os.MkdirAll(dir, 0700) != nil || os.Chmod(dir, 0700) != nil) {
		dir = ""
	}
	sshShare.dir = &dir
	return dir
}

// remoteCommand returns the command that starts rtrunner --stdio on the host, for an address with a path (ssh passes
// it to the shell of the user)
func remoteCommand(a Address) string {
	return shellQuote(a.Path) + " --stdio"
}

// sshDest is the destination of ssh: [user@]host
func sshDest(a Address) string {
	if a.User != "" {
		return a.User + "@" + a.Host
	}
	return a.Host
}

var shellSafe = regexp.MustCompile(`^[A-Za-z0-9_./~+:-]+$`)

// doubleQuoteSafe matches the strings that mean the same in double quotes for a POSIX shell and for cmd.exe
var doubleQuoteSafe = regexp.MustCompile("^[^\"$`\\\\%!\n]+$")

// shellQuote quotes s for the shell of the user on the host, unless it is safe as it is (e.g. ~/bin/rtrunner, where ~
// is expanded). The OpenSSH server of Windows starts cmd.exe, that knows only double quotes: they are used when they
// work in both (e.g. C:/Program Files/runtree/rtrunner.exe), and single quotes for a POSIX shell otherwise.
func shellQuote(s string) string {
	if shellSafe.MatchString(s) {
		return s
	}
	if doubleQuoteSafe.MatchString(s) {
		return `"` + s + `"`
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

// dialSSH starts ssh with the command (that starts rtrunner --stdio), and returns the connection to rtrunner
func dialSSH(a Address, command string, logger *slog.Logger) (*sshConn, error) {
	args := append(sshCommand[1:len(sshCommand):len(sshCommand)], sshOptions(a)...)
	args = append(args, "--", sshDest(a), command)
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
	exitCode := -1
	select {
	case <-c.exited:
		if c.state != nil {
			exitCode = c.state.ExitCode()
		}
	default:
	}
	if exitCode == exitNotUploaded {
		if e := notUploaded(c.stderr.String()); e != nil {
			return e
		}
	}
	msg := c.stderr.lastLines(3)
	if msg == "" {
		return fmt.Errorf("ssh %s: %w", c.dest, err)
	}
	if exitCode == 127 {
		msg += " (is rtrunner installed on the host? Its path can be given: ssh://host/path/to/rtrunner)"
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

func (w *tailWriter) String() string {
	w.lock.Lock()
	defer w.lock.Unlock()
	return string(w.buf)
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
