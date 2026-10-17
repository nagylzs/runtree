package rt

import (
	"slices"
	"strings"
	"testing"

	"github.com/nagylzs/runtree/internal/runner"
)

func TestSuccessExitCodes(t *testing.T) {
	tree := loadTestTree(t, `
tree:
  id: root
  seq:
    - type: run
      id: plain
      args: ["true"]
    - type: seq
      id: reboots
      success_exit_codes: [0, 255]
      nodes:
        - type: run
          id: reboot
          args: ["ssh", "host", "shutdown", "-r", "+5"]
        - type: run
          id: strict
          success_exit_codes: [0]
          args: ["true"]
`)
	for id, want := range map[string][]int{
		"root":    {0},
		"plain":   {0},
		"reboots": {0, 255},
		"reboot":  {0, 255},
		"strict":  {0},
	} {
		if got := tree.NodeById[id].SuccessExitCodes; !slices.Equal(got, want) {
			t.Errorf("%s success exit codes: %v, want %v", id, got, want)
		}
	}
}

func TestSuccessExitCodesInvalid(t *testing.T) {
	for _, value := range []string{`[]`, `["0"]`, `[256]`, `[-1]`, `0`} {
		_, err := parseTestTree(t, `
tree:
  id: root
  type: run
  args: ["true"]
  success_exit_codes: `+value+`
`)
		if err == nil || !strings.Contains(err.Error(), "success_exit_codes") {
			t.Errorf("success_exit_codes: %s must be rejected, got error %v", value, err)
		}
	}
}

func TestExitedSuccessfully(t *testing.T) {
	n := &Node{SuccessExitCodes: []int{0, 255}}
	for _, tc := range []struct {
		ls   runner.ProcState
		want bool
	}{
		{runner.ProcState{Exited: true, ExitCode: 0}, true},
		{runner.ProcState{Exited: true, ExitCode: 255, Error: "exit status 255"}, true},
		{runner.ProcState{Exited: true, ExitCode: 3, Error: "exit status 3"}, false},
		{runner.ProcState{Exited: false, ExitCode: -1, Error: "signal: killed"}, false},
		{runner.ProcState{Error: "could not start pty"}, false},
		{runner.ProcState{Exited: true, ExitCode: -1, Error: "connection lost"}, false},
		{runner.ProcState{Exited: true, ExitCode: 0, Error: "something went wrong"}, false},
	} {
		if got := n.ExitedSuccessfully(tc.ls); got != tc.want {
			t.Errorf("%s: got %v, want %v", tc.ls.String(), got, tc.want)
		}
	}

	n = &Node{SuccessExitCodes: []int{0}}
	if n.ExitedSuccessfully(runner.ProcState{Exited: true, ExitCode: 255, Error: "exit status 255"}) {
		t.Errorf("255 is not a success exit code by default")
	}
}

// procStatus calculates the status of a run node from the state of its process
func TestProcStatus(t *testing.T) {
	n := &Node{
		SuccessExitCodes: []int{0, 255},
		OnError:          OnError{Status: StatusFailed},
		Status:           StatusRunning,
	}
	for _, tc := range []struct {
		ls   runner.ProcState
		want Status
	}{
		{runner.ProcState{Pid: 1}, StatusRunning},
		{runner.ProcState{Exited: true, ExitCode: 0}, StatusSuccess},
		{runner.ProcState{Exited: true, ExitCode: 255, Error: "exit status 255"}, StatusSuccess},
		{runner.ProcState{Exited: true, ExitCode: 3, Error: "exit status 3"}, StatusFailed},
		{runner.ProcState{ExitCode: -1, Error: "signal: killed"}, StatusFailed},
	} {
		if got, _ := n.procStatus(tc.ls); got != tc.want {
			t.Errorf("%s: got %s, want %s", tc.ls.String(), StatusName(got), StatusName(tc.want))
		}
	}

	// a paused node stays paused until it is restarted
	n.OnError.Status, n.Status = StatusPaused, StatusPaused
	if got, _ := n.procStatus(runner.ProcState{Exited: true, ExitCode: 3, Error: "exit status 3"}); got != StatusPaused {
		t.Errorf("paused node: got %s", StatusName(got))
	}
}
