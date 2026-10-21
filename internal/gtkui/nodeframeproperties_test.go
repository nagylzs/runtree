package gtkui

import (
	"testing"
	"time"

	"github.com/nagylzs/runtree/internal/rt"
	"github.com/nagylzs/runtree/internal/runner"
)

// The result of a run node: the exit code, the signal, or why it could not start
func TestResultText(t *testing.T) {
	for _, tc := range []struct {
		name                       string
		startError                 string
		ps                         runner.ProcState
		started, success, timedOut bool
		want, state                string
	}{
		{name: "not started", want: ""},
		{name: "running", ps: runner.ProcState{Pid: 5}, started: true, want: ""},
		{name: "start error of the client", startError: "could not connect", want: "could not start: could not connect",
			state: "failed"},
		{name: "start error of the runner", started: true,
			ps:   runner.ProcState{Exited: true, Error: `exec: "nothing": executable file not found in $PATH`},
			want: `could not start: exec: "nothing": executable file not found in $PATH`, state: "failed"},
		{name: "success", ps: runner.ProcState{Pid: 5, Exited: true}, started: true, success: true,
			want: "exit code 0", state: "success"},
		{name: "failed", ps: runner.ProcState{Pid: 5, Exited: true, ExitCode: 3, Error: "exit status 3"},
			started: true, want: "exit code 3", state: "failed"},
		{name: "allowed exit code", ps: runner.ProcState{Pid: 5, Exited: true, ExitCode: 3, Error: "exit status 3"},
			started: true, success: true, want: "exit code 3", state: "success"},
		{name: "signal", ps: runner.ProcState{Pid: 5, Exited: true, ExitCode: -1, Error: "signal: interrupt"},
			started: true, timedOut: true, want: "signal: interrupt, timed out after 3s", state: "failed"},
	} {
		got, state := resultText(tc.startError, tc.ps, tc.started, tc.success, tc.timedOut, 3*time.Second)
		if got != tc.want || state != tc.state {
			t.Errorf("%s: got %q, %q; want %q, %q", tc.name, got, state, tc.want, tc.state)
		}
	}
}

func TestPropertyTexts(t *testing.T) {
	now := time.Date(2026, 10, 21, 15, 0, 0, 0, time.Local)
	started := time.Date(2026, 10, 21, 10, 53, 17, 0, time.Local)
	finished := started.Add(92 * time.Second)
	yesterday := started.Add(-24 * time.Hour)
	for _, tc := range []struct{ got, want string }{
		{progressText(runCounts{total: 7, running: 2, finished: 4, failed: 1}, 3),
			"2 running (at most 3) · 4 of 7 finished · 1 failed"},
		{progressText(runCounts{total: 3, finished: 3}, -1), "3 of 3 finished"},
		{progressText(runCounts{total: 2, running: 1}, -1), "1 running · 0 of 2 finished"},
		{timeText(time.Time{}, time.Time{}, 0, false, now), ""},
		{timeText(started, time.Time{}, 33*time.Second, true, now), "since 10:53:17 (00:33)"},
		{timeText(started, finished, 92*time.Second, false, now), "10:53:17 → 10:54:49 (01:32)"},
		{timeText(yesterday, finished, 24*time.Hour, false, now), "2026-10-20 10:53:17 → 10:54:49 (24:00:00)"},
		// re-run: running again, the finish time is of the previous run
		{timeText(finished, started, time.Second, true, now), "since 10:54:49 (00:01)"},
		{onErrorText(rt.DefaultOnError), "failed"},
		{onErrorText(rt.OnError{Status: rt.StatusPaused, Siblings: rt.OpFreeze}), "paused, freeze the siblings"},
		{locksText([]string{"db"}, []string{"deploy", "dns"}), "read: db · exclusive: deploy, dns"},
		{locksText(nil, nil), ""},
		{intsText([]int{0, 1, 2}), "0, 1, 2"},
	} {
		if tc.got != tc.want {
			t.Errorf("got %q, want %q", tc.got, tc.want)
		}
	}
}

// The settings row lists only what differs from the default
func TestSettingsText(t *testing.T) {
	run := &rt.Node{Type: rt.TypeRun, OnError: rt.DefaultOnError, ChildrenOnError: rt.DefaultOnError, MaxProc: -1,
		SuccessExitCodes: []int{0}}
	if s := settingsText(run); s != "" {
		t.Errorf("the default settings are %q", s)
	}
	run.Timeout = 3 * time.Second
	run.OnError = rt.OnError{Status: rt.StatusPaused}
	run.SuccessExitCodes = []int{0, 1}
	run.MaxProc = 2 // not for a run node
	if s, want := settingsText(run), "timeout 3s · on_error paused · success_exit_codes 0, 1"; s != want {
		t.Errorf("got %q, want %q", s, want)
	}
	par := &rt.Node{Type: rt.TypePar, OnError: rt.DefaultOnError,
		ChildrenOnError: rt.OnError{Status: rt.StatusFailed, Siblings: rt.OpFreeze}, MaxProc: 3,
		SuccessExitCodes: []int{0}}
	if s, want := settingsText(par), "children_on_error failed, freeze the siblings · max_proc 3"; s != want {
		t.Errorf("got %q, want %q", s, want)
	}
}
