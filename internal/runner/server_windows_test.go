package runner

import (
	"context"
	"strings"
	"testing"
	"time"
)

// The local runner runs a command in a pseudo console, the client gets its output and exit code
func TestLocalRunnerOnWindows(t *testing.T) {
	l := newTestLocalRunner()
	conn, err := l.dial()
	if err != nil {
		t.Fatal(err)
	}
	out := newClosedBuffer()
	_, exited := startNoTerminal(t, conn, cmdArgs("cmd", "/c", "echo hello & exit /b 3"), out)
	if ps := waitExit(t, exited); !ps.Exited || ps.ExitCode != 3 || ps.Pid == 0 {
		t.Errorf("the process state is %s", ps.String())
	}
	if output := out.wait(t); !strings.Contains(output, "hello\r\n") {
		t.Errorf("the output is %q", output)
	}
	waitServers(t, l)
}

// A pseudo console cannot be created with 0 columns or rows: the command starts with the default size
func TestZeroSizeOnWindows(t *testing.T) {
	l := newTestLocalRunner()
	for _, size := range []WinSize{{}, {Cols: 100}, {Rows: 30}} {
		conn, err := l.dial()
		if err != nil {
			t.Fatal(err)
		}
		ca := cmdArgs("cmd", "/c", "exit /b 0")
		ca.InitialPtySize = size
		_, exited := startNoTerminal(t, conn, ca, newClosedBuffer())
		if ps := waitExit(t, exited); !ps.Exited || ps.ExitCode != 0 {
			t.Errorf("size %+v: the process state is %s", size, ps.String())
		}
	}
	waitServers(t, l)
}

// A command stops on the Ctrl+C typed into its pseudo console also when the runner ignores Ctrl+C (as under sshd):
// otherwise the command would inherit that
func TestCtrlCWhenIgnoredOnWindows(t *testing.T) {
	if r, _, err := procSetConsoleCtrlHandler.Call(0, 1); r == 0 {
		t.Fatal(err)
	}
	l := newTestLocalRunner()
	conn, err := l.dial()
	if err != nil {
		t.Fatal(err)
	}
	c, exited := startNoTerminal(t, conn, cmdArgs("ping", "-n", "30", "127.0.0.1"), newClosedBuffer())
	defer c.Close()
	// a Ctrl+C typed before ping started may be lost: type it until ping stops
	for range 20 {
		select {
		case ps := <-exited:
			if !ps.Exited || uint32(ps.ExitCode) != 0xC000013A {
				t.Errorf("the process state is %s", ps.String())
			}
			return
		case <-time.After(500 * time.Millisecond):
			if err := c.Type([]byte{3}); err != nil {
				t.Fatal(err)
			}
		}
	}
	t.Fatal("ping did not stop on Ctrl+C")
}

// SIGINT types Ctrl+C into the pseudo console: ping stops; SIGTERM is not supported, the client counts the error
func TestSignalsOnWindows(t *testing.T) {
	l := newTestLocalRunner()
	conn, err := l.dial()
	if err != nil {
		t.Fatal(err)
	}
	c, exited := startNoTerminal(t, conn, cmdArgs("ping", "-n", "30", "127.0.0.1"), newClosedBuffer())
	defer c.Close()
	ctx := context.Background()
	if err := c.SendSignal(ctx, "SIGTERM", false); err != nil {
		t.Fatal(err)
	}
	for deadline := time.Now().Add(5 * time.Second); c.SignalErrors() == 0; time.Sleep(20 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("no error for SIGTERM")
		}
	}
	// a Ctrl+C typed before ping started may be lost: send it until ping stops
	for range 20 {
		if err := c.SendSignal(ctx, "SIGINT", false); err != nil {
			t.Fatal(err)
		}
		select {
		case ps := <-exited:
			if !ps.Exited || uint32(ps.ExitCode) != 0xC000013A {
				t.Errorf("the process state is %s", ps.String())
			}
			if n := c.SignalErrors(); n != 1 {
				t.Errorf("%d signal errors", n)
			}
			return
		case <-time.After(500 * time.Millisecond):
		}
	}
	t.Fatal("ping did not stop on SIGINT")
}
