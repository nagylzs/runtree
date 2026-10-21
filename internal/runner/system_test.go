package runner

import (
	"runtime"
	"testing"

	"github.com/vmihailenco/msgpack/v5"
)

// The process state tells the system of the runner; the field is optional both ways: an older client skips it, and
// the state of an older runner has none
func TestProcStateSystem(t *testing.T) {
	type oldProcState struct { // the ProcState of v0.1.0
		Pid      int    `msgpack:"p,omitempty"`
		Exited   bool   `msgpack:"e,omitempty"`
		ExitCode int    `msgpack:"c,omitempty"`
		Error    string `msgpack:"err,omitempty"`
	}
	data, err := msgpack.Marshal(&ProcState{Pid: 7, Exited: true, ExitCode: 3, System: "windows"})
	if err != nil {
		t.Fatal(err)
	}
	var old oldProcState
	if err := msgpack.Unmarshal(data, &old); err != nil || old.Pid != 7 || old.ExitCode != 3 {
		t.Errorf("an older client: %+v, %v", old, err)
	}
	if data, err = msgpack.Marshal(&oldProcState{Pid: 8}); err != nil {
		t.Fatal(err)
	}
	var ps ProcState
	if err := msgpack.Unmarshal(data, &ps); err != nil || ps.Pid != 8 || ps.System != "" {
		t.Errorf("an older runner: %+v, %v", ps, err)
	}
}

// The client knows the system of the runner from its process states
func TestRunnerSystem(t *testing.T) {
	l := newTestLocalRunner()
	conn, err := l.dial()
	if err != nil {
		t.Fatal(err)
	}
	args := cmdArgs("sh", "-c", "exit 0")
	if runtime.GOOS == "windows" {
		args = cmdArgs("cmd", "/c", "exit /b 0")
	}
	c, exited := startNoTerminal(t, conn, args, newClosedBuffer())
	waitExit(t, exited)
	if s := c.RunnerSystem(); s != runtime.GOOS {
		t.Errorf("the system of the runner is %q", s)
	}
	waitServers(t, l)
}
