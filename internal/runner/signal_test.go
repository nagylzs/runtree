//go:build !windows

package runner

import (
	"context"
	"strings"
	"testing"
	"time"
)

// A signal goes to the process group, like Ctrl+C in a terminal: bash waits for its foreground command when only bash
// gets SIGINT (and goes on when the command does not die of it), so the script stops only when the command gets it too
func TestSignalProcessGroup(t *testing.T) {
	l := newTestLocalRunner()
	c, output, exited := startLocal(t, l, "bash", "-c", "echo ready; sleep 100; echo after")
	time.Sleep(500 * time.Millisecond) // bash waits for sleep
	if err := c.SendSignal(context.Background(), "SIGINT", false); err != nil {
		t.Fatal(err)
	}
	select {
	case ps := <-exited:
		if ps.Exited && ps.ExitCode == 0 {
			t.Errorf("the script did not stop: %s", ps.String())
		}
	case <-time.After(3 * time.Second):
		_ = c.SendSignal(context.Background(), "SIGKILL", true)
		t.Fatal("the script did not stop")
	}
	if out := waitOutput(t, output); strings.Contains(out, "after") {
		t.Errorf("the script went on: %q", out)
	}
	waitServers(t, l)
}

// Type writes to the terminal of the process, as if it was typed: a line for read, Ctrl+C for a running command
func TestType(t *testing.T) {
	l := newTestLocalRunner()
	c, output, exited := startLocal(t, l, "sh", "-c", "read line; echo got=$line; sleep 100; echo after")
	if err := c.Type([]byte("abc\n")); err != nil {
		t.Fatal(err)
	}
	time.Sleep(500 * time.Millisecond) // sh waits for sleep
	if err := c.Type(CtrlC); err != nil {
		t.Fatal(err)
	}
	select {
	case <-exited:
	case <-time.After(3 * time.Second):
		_ = c.SendSignal(context.Background(), "SIGKILL", true)
		t.Fatal("Ctrl+C did not stop the script")
	}
	out := waitOutput(t, output)
	if !strings.Contains(out, "got=abc") || strings.Contains(out, "after") {
		t.Errorf("the output is %q", out)
	}
	waitServers(t, l)
}
