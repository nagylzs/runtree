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

func newTestLocalRunner() *localRunner {
	l := newLocalRunner()
	l.handler = slog.DiscardHandler
	return l
}

var testArgs = CmdArgs{InheritSysEnvs: true, InitialPtySize: WinSize{Rows: 24, Cols: 80}}

// cmdArgs returns testArgs with the command args
func cmdArgs(args ...string) CmdArgs {
	ca := testArgs
	ca.Name, ca.Args = args[0], args
	return ca
}

// startNoTerminal starts a command on conn without a terminal (like runtree-headless), its output goes to output.
// It returns the client, and the exit status of the command.
func startNoTerminal(t *testing.T, conn net.Conn, ca CmdArgs, output io.Writer) (*Client, <-chan ProcState) {
	t.Helper()
	c, err := newClient(context.Background(), conn, HandshakeTimeout, slog.New(slog.DiscardHandler), nil)
	if err != nil {
		t.Fatal(err)
	}
	c.SetOutput(output)
	c.SetNoTerminal()
	exited := make(chan ProcState, 1)
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
	return c, exited
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

// waitDone waits until done is closed, it fails with msg after 10 seconds
func waitDone(t *testing.T, done <-chan struct{}, msg string) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal(msg)
	}
}

func waitServers(t *testing.T, l *localRunner) {
	t.Helper()
	for deadline := time.Now().Add(10 * time.Second); l.running() > 0; time.Sleep(50 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatalf("%d servers are running", l.running())
		}
	}
}

// closedBuffer is an output writer that tells when it is closed
type closedBuffer struct {
	mu     sync.Mutex
	b      strings.Builder
	closed chan struct{}
}

func newClosedBuffer() *closedBuffer {
	return &closedBuffer{closed: make(chan struct{})}
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

// wait waits until the buffer is closed (the end of the output), and returns what was written
func (w *closedBuffer) wait(t *testing.T) string {
	t.Helper()
	select {
	case <-w.closed:
	case <-time.After(10 * time.Second):
		t.Fatal("the output is not closed")
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.b.String()
}

// failingWriter writes to w, and returns an error
type failingWriter struct {
	*closedBuffer
}

func (w failingWriter) Write(p []byte) (int, error) {
	_, _ = w.closedBuffer.Write(p)
	return 0, errors.New("failing writer")
}
