//go:build !windows

package runner

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"strings"
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
		"ssh://root@server01":    "error: unknown runner",
		"tcp://server01":         "error: invalid runner",
		"Local":                  "error: invalid runner",
		"udp://server01:5000:22": "error: unknown runner",
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
	c, err := newClient(context.Background(), conn, logger, nil)
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
	// the terminal of the GUI
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
	return c, output, exited
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
	l := newLocalRunner()
	_, output, exited := startLocal(t, l, "sh", "-c", "echo hello; exit 3")
	ps := waitExit(t, exited)
	if !ps.Exited || ps.ExitCode != 3 || ps.Pid == 0 {
		t.Errorf("the process state is %s", ps.String())
	}
	select {
	case out := <-output:
		if out != "hello\r\n" {
			t.Errorf("the output is %q", out)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the terminal was not closed")
	}
	// the server returns when the client disconnects
	waitServers(t, l)
}

// StopLocal terminates the processes, the clients get their exit status, and nothing can be started afterwards
func TestStopLocal(t *testing.T) {
	l := newLocalRunner()
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
	_, err = newClient(context.Background(), clientConn, slog.New(slog.DiscardHandler), nil)
	if err == nil || err.Error() != "incompatible versions, local="+Version+" remote=0.0.1" {
		t.Errorf("the error is %v", err)
	}
}
