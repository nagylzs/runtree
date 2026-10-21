package rt

import (
	"testing"
	"time"
)

// The elapsed time of a node is the time of its last run: a re-run starts it again (like its timeout), so it is the
// time between the start and the finish that the GUI shows. The total time of the processes adds up the runs.
func TestElapsedIsTheLastRun(t *testing.T) {
	tree := loadTestTree(t, `
tree:
  id: root
  seq:
    - id: step
      args: ["true"]
`)
	step := tree.NodeById["step"]
	run := func(took time.Duration) {
		t.Helper()
		step.StartError = ""
		step.starting = true
		step.updateStatus(true)
		if step.Status != StatusRunning {
			t.Fatalf("the step is %s, not running", StatusName(step.Status))
		}
		step.Started = step.Started.Add(-took) // it ran for took
		step.totalProcCommitedTo = step.Started
		step.starting = false
		step.StartError = "failed in a test"
		step.updateStatus(true)
		if step.Status != StatusFailed {
			t.Fatalf("the step is %s, not failed", StatusName(step.Status))
		}
	}
	run(5 * time.Second)
	elapsed, total := step.CalcElapsed()
	if elapsed < 5*time.Second || elapsed > 6*time.Second {
		t.Errorf("after the first run: elapsed %s, want 5s", elapsed)
	}
	run(2 * time.Second)
	elapsed, total = step.CalcElapsed()
	if elapsed < 2*time.Second || elapsed > 3*time.Second {
		t.Errorf("after the re-run: elapsed %s, want 2s (the last run)", elapsed)
	}
	if want := step.Finished.Sub(step.Started); elapsed != want {
		t.Errorf("elapsed %s, but it ran from %s to %s (%s)", elapsed, step.Started, step.Finished, want)
	}
	if total < 7*time.Second || total > 8*time.Second {
		t.Errorf("the total time of the processes is %s, want 7s (both runs)", total)
	}
}
