//go:build !windows

package runner

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestParseAddress(t *testing.T) {
	for s, want := range map[string]string{
		"":                       "local",
		"local":                  "local",
		"127.0.0.1:5000":         "tcp://127.0.0.1:5000",
		"tcp://server01:5000":    "tcp://server01:5000",
		"[::1]:5000":             "tcp://[::1]:5000",
		"server01":               "error: invalid runner",
		":5000":                  "error: invalid runner",
		"server01:":              "error: invalid runner",
		"tcp://server01":         "error: invalid runner",
		"Local":                  "error: invalid runner",
		"udp://server01:5000:22": "error: unknown runner",
		"server01:ssh":           "error: invalid runner",
		"server01:0":             "error: invalid runner",
		"server01:65536":         "error: invalid runner",
	} {
		a, err := ParseAddress(s)
		got := a.String()
		if err != nil {
			got = "error: " + err.Error()
		}
		if !strings.HasPrefix(got, want) {
			t.Errorf("%q: got %q, want %q", s, got, want)
		}
	}
}

func newTestLocalRunner() *localRunner {
	l := newLocalRunner()
	l.handler = slog.DiscardHandler
	return l
}

var testArgs = CmdArgs{InheritSysEnvs: true, InitialPtySize: WinSize{Rows: 24, Cols: 80}}

// startLocal starts a command on the local runner l, and returns its client, its terminal output when it exits, and
// its exit status
func startLocal(t *testing.T, l *localRunner, args ...string) (*Client, <-chan string, <-chan ProcState) {
	t.Helper()
	conn, err := l.dial()
	if err != nil {
		t.Fatal(err)
	}
	return startOn(t, conn, args...)
}

func startOn(t *testing.T, conn net.Conn, args ...string) (*Client, <-chan string, <-chan ProcState) {
	t.Helper()
	logger := slog.New(slog.DiscardHandler)
	c, err := newClient(context.Background(), conn, HandshakeTimeout, logger, nil)
	if err != nil {
		t.Fatal(err)
	}
	exited := make(chan ProcState, 1)
	ca := testArgs
	ca.Name, ca.Args = args[0], args
	err = c.StartPty(context.Background(), ca, func(ps ProcState) {
		if !ps.Alive() {
			select {
			case exited <- ps:
			default:
			}
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	return c, attach(t, c), exited
}

// attach connects to the pty of the client like the terminal of the GUI, and returns its output when it is closed
func attach(t *testing.T, c *Client) <-chan string {
	t.Helper()
	term, err := net.Dial("unix", c.PtyPath)
	if err != nil {
		t.Fatal(err)
	}
	output := make(chan string, 1)
	go func() {
		b, _ := io.ReadAll(term)
		output <- string(b)
	}()
	t.Cleanup(func() { _ = term.Close() })
	return output
}

func waitOutput(t *testing.T, output <-chan string) string {
	t.Helper()
	select {
	case out := <-output:
		return out
	case <-time.After(10 * time.Second):
		t.Fatal("the terminal was not closed")
	}
	return ""
}

func waitExit(t *testing.T, exited <-chan ProcState) ProcState {
	t.Helper()
	select {
	case ps := <-exited:
		return ps
	case <-time.After(10 * time.Second):
		t.Fatal("the process did not exit")
	}
	return ProcState{}
}

func waitServers(t *testing.T, l *localRunner) {
	t.Helper()
	for deadline := time.Now().Add(10 * time.Second); l.running() > 0; time.Sleep(50 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatalf("%d servers are running", l.running())
		}
	}
}

func TestLocalRunner(t *testing.T) {
	l := newTestLocalRunner()
	_, output, exited := startLocal(t, l, "sh", "-c", "echo hello; exit 3")
	ps := waitExit(t, exited)
	if !ps.Exited || ps.ExitCode != 3 || ps.Pid == 0 {
		t.Errorf("the process state is %s", ps.String())
	}
	if out := waitOutput(t, output); out != "hello\r\n" {
		t.Errorf("the output is %q", out)
	}
	// the server returns when the client disconnects
	waitServers(t, l)
}

// StopLocal terminates the processes, the clients get their exit status, and nothing can be started afterwards
func TestStopLocal(t *testing.T) {
	l := newTestLocalRunner()
	_, _, exited1 := startLocal(t, l, "sleep", "100")
	_, _, exited2 := startLocal(t, l, "sh", "-c", "sleep 100")
	for deadline := time.Now().Add(5 * time.Second); l.processes() < 2; time.Sleep(20 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatalf("%d processes are running", l.processes())
		}
	}
	began := time.Now()
	if !l.stop(StopLocalTimeout) {
		t.Fatalf("%d servers are running", l.running())
	}
	if elapsed := time.Since(began); elapsed > TerminateGraceInt {
		t.Errorf("stopping took %s, SIGINT should be enough", elapsed)
	}
	for _, exited := range []<-chan ProcState{exited1, exited2} {
		if ps := waitExit(t, exited); ps.Exited || !strings.Contains(ps.Error, "interrupt") {
			t.Errorf("the process state is %s", ps.String())
		}
	}
	if n := l.processes(); n != 0 {
		t.Errorf("%d processes are running", n)
	}
	if _, err := l.dial(); !errors.Is(err, errLocalStopped) {
		t.Errorf("dial after stop: %v", err)
	}
}

// A server that is terminated before its process starts terminates the process as soon as it starts
func TestTerminateBeforeStart(t *testing.T) {
	clientConn, serverConn, err := socketPair()
	if err != nil {
		t.Fatal(err)
	}
	srv := newServer(serverConn, slog.New(slog.DiscardHandler))
	srv.Terminate()
	done := make(chan error, 1)
	go func() { done <- srv.Run(context.Background()) }()
	_, _, exited := startOn(t, clientConn, "sleep", "100")
	if ps := waitExit(t, exited); ps.Exited || !strings.Contains(ps.Error, "interrupt") {
		t.Errorf("the process state is %s", ps.String())
	}
	select {
	case err := <-done:
		if err != nil {
			t.Error(err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the server did not return")
	}
}

// The protocol versions must match
func TestVersionMismatch(t *testing.T) {
	clientConn, serverConn, err := socketPair()
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		_ = SendCmdCode(context.Background(), serverConn, MsgVersion)
		_ = SendString(context.Background(), serverConn, "0.0.1")
		_, _ = RecvCmdCode(context.Background(), serverConn)
		_, _ = RecvString(context.Background(), serverConn)
	}()
	_, err = newClient(context.Background(), clientConn, HandshakeTimeout, slog.New(slog.DiscardHandler), nil)
	if err == nil || err.Error() != "incompatible versions, local="+Version+" remote=0.0.1" {
		t.Errorf("the error is %v", err)
	}
}

// closedBuffer is an output writer that tells when it is closed
type closedBuffer struct {
	mu     sync.Mutex
	b      strings.Builder
	closed chan struct{}
}

func (w *closedBuffer) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.b.Write(p)
}

func (w *closedBuffer) Close() error {
	close(w.closed)
	return nil
}

// Without a terminal, the output only goes to the output writer, and it is closed at the end of the output. A process
// with a lot of output does not block, also when the writer fails.
func TestNoTerminal(t *testing.T) {
	l := newTestLocalRunner()
	for _, failing := range []bool{false, true} {
		conn, err := l.dial()
		if err != nil {
			t.Fatal(err)
		}
		c, err := newClient(context.Background(), conn, HandshakeTimeout, slog.New(slog.DiscardHandler), nil)
		if err != nil {
			t.Fatal(err)
		}
		out := &closedBuffer{closed: make(chan struct{})}
		var w io.Writer = out
		if failing {
			w = failingWriter{out}
		}
		c.SetOutput(w)
		c.SetNoTerminal()
		exited := make(chan ProcState, 1)
		ca := testArgs
		ca.Name, ca.Args = "sh", []string{"sh", "-c", "seq 1 100000; echo done"}
		err = c.StartPty(context.Background(), ca, func(ps ProcState) {
			if !ps.Alive() {
				select {
				case exited <- ps:
				default:
				}
			}
		})
		if err != nil {
			t.Fatal(err)
		}
		if c.PtyPath != "" {
			t.Errorf("there is a terminal socket: %s", c.PtyPath)
		}
		if ps := waitExit(t, exited); !ps.Exited || ps.ExitCode != 0 {
			t.Errorf("the process state is %s", ps.String())
		}
		select {
		case <-out.closed:
		case <-time.After(10 * time.Second):
			t.Fatal("the output is not closed")
		}
		if !failing && !strings.HasSuffix(out.b.String(), "100000\r\ndone\r\n") {
			t.Errorf("the end of the output is %q", out.b.String()[max(0, out.b.Len()-30):])
		}
	}
	waitServers(t, l)
}

// failingWriter writes to w, and returns an error
type failingWriter struct {
	*closedBuffer
}

func (w failingWriter) Write(p []byte) (int, error) {
	_, _ = w.closedBuffer.Write(p)
	return 0, errors.New("failing writer")
}
