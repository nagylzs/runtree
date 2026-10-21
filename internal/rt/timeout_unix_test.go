//go:build !windows

package rt

import (
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/nagylzs/runtree/internal/runner"
)

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

// When the runner cannot send SIGTERM (Windows has no SIGTERM), SIGKILL follows at once, not after the grace period
func TestTimeoutWithoutSIGTERM(t *testing.T) {
	saved := timeoutGrace
	timeoutGrace = []time.Duration{300 * time.Millisecond, 10 * time.Second}
	t.Cleanup(func() { timeoutGrace = saved })
	runner.SetUnsupportedSignals("SIGTERM")
	t.Cleanup(func() { runner.SetUnsupportedSignals() })
	tree := loadTestTree(t, `
tree:
  id: root
  par:
    - id: stubborn
      timeout: 1s
      args: [sh, -c, "trap '' INT; sleep 100"]
`)
	tree.SetTerminalHelper(&ptyTerminalHelper{})
	began := time.Now()
	runTimeoutTree(t, tree, func() bool { return StatusFinished(tree.Root.Status) })
	if elapsed := time.Since(began); elapsed > 5*time.Second {
		t.Errorf("it was killed after %v", elapsed)
	}
	tree.RLock("TestTimeoutWithoutSIGTERM")
	events := eventTexts(tree, 0)
	tree.RUnlock()
	checkEventsInOrder(t, events, "stubborn timeout WARN timed out after 1s, typing Ctrl+C",
		"stubborn signal INFO sending SIGTERM", "stubborn runner-error WARN SIGTERM is not supported on this system",
		"stubborn signal INFO sending SIGKILL", "stubborn status WARN Running → Failed (timed out after 1s)")
}
