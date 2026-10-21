package runner

import (
	"strings"
	"testing"
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
