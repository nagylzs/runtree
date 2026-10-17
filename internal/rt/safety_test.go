package rt

import (
	"strings"
	"testing"

	"github.com/nagylzs/runtree/internal/runner"
)

const safetyTree = `
tree:
  id: root
  seq:
    - type: run
      id: job
      args: ["true"]
`

// brokenTree returns a tree where status calculation panics (unknown node type).
func brokenTree(t *testing.T) (*Tree, *fakeTerminalHelper) {
	tree := loadTestTree(t, safetyTree)
	helper := &fakeTerminalHelper{}
	tree.SetTerminalHelper(helper)
	tree.NodeById["job"].Type = 99
	return tree, helper
}

func checkRecovered(t *testing.T, helper *fakeTerminalHelper, what string) {
	t.Helper()
	if len(helper.internalErrors) != 1 || !strings.Contains(helper.internalErrors[0], what) {
		t.Errorf("one internal error was expected in %s, got %v", what, helper.internalErrors)
	}
}

func TestSchedulerRecoversFromPanic(t *testing.T) {
	tree, helper := brokenTree(t)
	tree.schedule() // must not panic
	checkRecovered(t, helper, "scheduler")
	checkUnlocked(t, tree)
}

func TestProcStateChangedRecoversFromPanic(t *testing.T) {
	tree, helper := brokenTree(t)
	tree.NodeById["job"].procStateChanged(runner.ProcState{Exited: true}) // must not panic
	checkRecovered(t, helper, "process state change of job")
	checkUnlocked(t, tree)
}
