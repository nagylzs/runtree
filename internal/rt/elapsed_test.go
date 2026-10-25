package rt

import (
	"testing"
	"time"
)

// The elapsed time of a node is the time of its last run: a re-run starts it again (like its timeout), so it is the
// time between the start and the finish that the GUI shows. The total time of the processes is of the last run too,
// so that the speedup (total / elapsed) is of the same run: a re-run of a run node is not a speedup of 2.
func TestElapsedIsTheLastRun(t *testing.T) {
	tree := loadTestTree(t, `
tree:
  id: root
  par:
    - id: step
      args: ["true"]
    - id: other
      args: ["true"]
`)
	root, step, other := tree.NodeById["root"], tree.NodeById["step"], tree.NodeById["other"]
	start := func(n *Node) {
		t.Helper()
		n.StartError = ""
		n.starting = true
		n.updateStatusesUp(true)
		if n.Status != StatusRunning {
			t.Fatalf("%s is %s, not running", n.Id, StatusName(n.Status))
		}
	}
	finish := func(n *Node, took time.Duration) {
		t.Helper()
		n.Started = n.Started.Add(-took) // it ran for took
		n.totalProcCommitedTo = n.Started
		n.starting = false
		n.StartError = "failed in a test"
		n.updateStatusesUp(true)
		if n.Status != StatusFailed {
			t.Fatalf("%s is %s, not failed", n.Id, StatusName(n.Status))
		}
	}
	about := func(what string, got, want time.Duration) {
		t.Helper()
		if got < want || got > want+time.Second {
			t.Errorf("%s: %s, want %s", what, got, want)
		}
	}

	start(other) // the root runs while the step runs twice
	start(step)
	finish(step, 5*time.Second)
	elapsed, total := step.CalcElapsed()
	about("the elapsed time of the first run", elapsed, 5*time.Second)
	about("the total time of the first run", total, 5*time.Second)

	start(step)
	finish(step, 2*time.Second)
	elapsed, total = step.CalcElapsed()
	about("the elapsed time after the re-run", elapsed, 2*time.Second)
	if want := step.Finished.Sub(step.Started); elapsed != want {
		t.Errorf("elapsed %s, but it ran from %s to %s (%s)", elapsed, step.Started, step.Finished, want)
	}
	about("the total time after the re-run", total, 2*time.Second)
	if root.Status != StatusRunning {
		t.Fatalf("the root is %s, not running", StatusName(root.Status))
	}
	_, total = root.CalcElapsed()
	about("the total time of the root (both runs of the step, in its one run)", total, 7*time.Second)

	finish(other, 0)
	if root.Status == StatusRunning {
		t.Fatal("the root still runs")
	}
	start(step) // a re-run of the root
	finish(step, time.Second)
	_, total = root.CalcElapsed()
	about("the total time of the root after its re-run", total, time.Second)
}
