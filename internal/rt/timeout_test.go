package rt

import (
	"regexp"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// timeout is inherited: on a group it is the limit of each run node below it, a node can change it, 0 is no limit
func TestTimeoutInherited(t *testing.T) {
	tree := loadTestTree(t, `
tree:
  id: root
  timeout: 1h
  seq:
    - {id: plain, args: ["true"]}
    - id: group
      timeout: 1h30m
      par:
        - {id: inner, args: ["true"]}
        - {id: short, timeout: 90s, args: ["true"]}
        - {id: unlimited, timeout: 0, args: ["true"]}
        - {id: unlimited2, timeout: "0", args: ["true"]}
`)
	for id, want := range map[string]time.Duration{
		"root": time.Hour, "plain": time.Hour, "group": 90 * time.Minute, "inner": 90 * time.Minute,
		"short": 90 * time.Second, "unlimited": 0, "unlimited2": 0,
	} {
		if got := tree.NodeById[id].Timeout; got != want {
			t.Errorf("%s timeout: %v, want %v", id, got, want)
		}
	}
	if got := loadTestTree(t, "tree:\n  args: [\"true\"]\n").Root.Timeout; got != 0 {
		t.Errorf("the default timeout is %v", got)
	}
}

func TestDurationText(t *testing.T) {
	for d, want := range map[time.Duration]string{
		90 * time.Second: "1m30s", 30 * time.Minute: "30m", time.Hour: "1h", 90 * time.Minute: "1h30m",
		time.Hour + time.Second: "1h0m1s", 2 * time.Second: "2s", 1500 * time.Millisecond: "1.5s",
	} {
		if got := DurationText(d); got != want {
			t.Errorf("DurationText(%v) = %q, want %q", d, got, want)
		}
	}
}

// runTimeoutTree runs the tree with the local runner until done returns true (it is called with the tree locked)
func runTimeoutTree(t *testing.T, tree *Tree, done func() bool) {
	t.Helper()
	for deadline := time.Now().Add(15 * time.Second); ; time.Sleep(50 * time.Millisecond) {
		tree.schedule()
		tree.RLock("runTimeoutTree")
		ok := done()
		tree.RUnlock()
		if ok {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("the tree did not get there")
		}
	}
}

// When a run node runs longer than its timeout, Ctrl+C is typed into its terminal (that stops most programs), and the
// node fails. Also when nobody watches the terminal (runtree-headless).
func TestTimeoutCtrlC(t *testing.T) {
	for _, unattended := range []bool{false, true} {
		tree := loadTestTree(t, `
tree:
  id: root
  seq:
    - {id: slow, timeout: 1s, args: [sleep, "30"]}
    - {id: next, args: ["true"]}
`)
		tree.SetTerminalHelper(&ptyTerminalHelper{})
		if unattended {
			tree.SetUnattended()
		}
		began := time.Now()
		runTimeoutTree(t, tree, func() bool { return StatusFinished(tree.Root.Status) })
		elapsed := time.Since(began)
		tree.RLock("TestTimeoutCtrlC")
		slow := tree.NodeById["slow"]
		status, timedOut := StatusName(tree.Root.Status)+" "+StatusName(slow.Status), slow.TimedOut
		next := tree.NodeById["next"].Starts
		events := eventTexts(tree, 0)
		tree.RUnlock()
		if status != "Failed Failed" || !timedOut || next != 0 {
			t.Errorf("unattended %v: root and slow: %s, timed out: %v, next started %d times", unattended, status,
				timedOut, next)
		}
		if elapsed < time.Second || elapsed > 5*time.Second {
			t.Errorf("unattended %v: the tree ran for %v", unattended, elapsed)
		}
		checkEventsInOrder(t, events, "slow timeout WARN timed out after 1s, typing Ctrl+C",
			"slow status WARN Running → Failed (timed out after 1s)")
		for _, e := range events {
			if strings.Contains(e, "sending SIG") {
				t.Errorf("unattended %v: Ctrl+C was not enough: %s", unattended, e)
			}
		}
	}
}

// A process that does not stop on Ctrl+C gets SIGTERM after the grace period, and nothing of it is left running (the
// commands that its script started stop too)
func TestTimeoutEscalates(t *testing.T) {
	saved := timeoutGrace
	timeoutGrace = []time.Duration{300 * time.Millisecond, 300 * time.Millisecond}
	t.Cleanup(func() { timeoutGrace = saved })
	tree := loadTestTree(t, `
tree:
  id: root
  par:
    - id: stubborn
      timeout: 1s
      args: [sh, -c, "trap '' INT; sleep 100 & echo child=$!; wait"]
`)
	tree.SetTerminalHelper(&ptyTerminalHelper{})
	recorder := &testRecorder{}
	tree.SetOutputRecorder(recorder)
	runTimeoutTree(t, tree, func() bool { return StatusFinished(tree.Root.Status) })
	tree.RLock("TestTimeoutEscalates")
	status := StatusName(tree.NodeById["stubborn"].Status)
	events := eventTexts(tree, 0)
	tree.RUnlock()
	if status != "Failed" {
		t.Errorf("stubborn is %s", status)
	}
	checkEventsInOrder(t, events, "stubborn timeout WARN timed out after 1s, typing Ctrl+C",
		"stubborn signal INFO sending SIGTERM", "stubborn status WARN Running → Failed (timed out after 1s)")
	for _, e := range events {
		if strings.Contains(e, "SIGKILL") {
			t.Errorf("SIGTERM was not enough: %s", e)
		}
	}

	out, _ := recorder.get("stubborn 1")
	m := regexp.MustCompile(`child=(\d+)`).FindStringSubmatch(out)
	if m == nil {
		t.Fatalf("no child pid in the output: %q", out)
	}
	pid, _ := strconv.Atoi(m[1])
	for deadline := time.Now().Add(3 * time.Second); syscall.Kill(pid, 0) == nil; time.Sleep(20 * time.Millisecond) {
		if time.Now().After(deadline) {
			_ = syscall.Kill(pid, syscall.SIGKILL)
			t.Fatalf("the child of the script (%d) is still running", pid)
		}
	}
}

// A process that exits with 0 after Ctrl+C still fails: it did not finish its work. on_error decides what happens
// then. Running it again starts the clock again.
func TestTimeoutFailsAfterExitZero(t *testing.T) {
	tree := loadTestTree(t, `
tree:
  id: root
  seq:
    - id: polite
      timeout: 1s
      on_error: paused
      args: [sh, -c, "trap 'echo bye; exit 0' INT; while :; do sleep 0.1; done"]
`)
	tree.SetTerminalHelper(&ptyTerminalHelper{})
	polite := tree.NodeById["polite"]
	runTimeoutTree(t, tree, func() bool { return polite.Status == StatusPaused })
	tree.Lock("TestTimeoutFailsAfterExitZero")
	exitCode, timedOut := polite.Client.LastState().ExitCode, polite.TimedOut
	events := eventTexts(tree, 0)
	if _, err := polite.PerformOp(OpRun, true, false, true, nil); err != nil {
		t.Error(err)
	}
	again := polite.TimedOut
	tree.UnLock()
	t.Cleanup(func() { killNode(t, polite) })
	if exitCode != 0 || !timedOut {
		t.Errorf("exit code %d, timed out %v", exitCode, timedOut)
	}
	checkEventsInOrder(t, events, "polite exited INFO exited with code 0",
		"polite status WARN Running → Paused (timed out after 1s)")
	if again {
		t.Error("the node is still timed out after it is run again")
	}
}

// checkEventsInOrder checks that the events are in the list (by their beginning), in this order
func checkEventsInOrder(t *testing.T, events []string, want ...string) {
	t.Helper()
	i := 0
	for _, e := range events {
		if i < len(want) && strings.HasPrefix(e, want[i]) {
			i++
		}
	}
	if i < len(want) {
		t.Errorf("event %q is missing (in this order) from:\n%s", want[i], strings.Join(slices.Clone(events), "\n"))
	}
}
