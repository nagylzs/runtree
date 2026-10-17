package rt

import (
	"testing"
)

// upgradeTree is like a real tree: the reboot node fails because ssh returns 255 when the server reboots.
const upgradeTree = `
tree:
  id: root
  seq:
    - type: seq
      id: upgrade
      on_error: paused
      provides: ["upgraded"]
      nodes:
        - type: run
          id: reboot
          on_error: failed # on_error is inherited, so it would be paused otherwise
          args: ["ssh", "host", "shutdown", "-r", "+5"]
        - type: run
          id: check
          args: ["true"]
    - type: run
      id: after
      requires: ["upgraded"]
      args: ["true"]
`

// pausedUpgrade returns the tree where the reboot node failed, so the upgrade node is paused.
func pausedUpgrade(t *testing.T) *Tree {
	tree := loadTestTree(t, upgradeTree)
	setStatus(tree, "reboot", StatusFailed)
	tree.Root.updateStatusesDown(true)
	if st := tree.NodeById["upgrade"].Status; st != StatusPaused {
		t.Fatalf("upgrade should be paused, it is %s", StatusName(st))
	}
	return tree
}

func performOp(t *testing.T, tree *Tree, id string, op NodeOp, recursive bool) {
	t.Helper()
	if _, err := tree.NodeById[id].PerformOp(op, true, recursive, true, nil); err != nil {
		t.Fatalf("%s on %s: %v", OpName(op), id, err)
	}
}

func checkStatus(t *testing.T, tree *Tree, id string, want Status) {
	t.Helper()
	if st := tree.NodeById[id].Status; st != want {
		t.Errorf("%s is %s, want %s", id, StatusName(st), StatusName(want))
	}
}

func TestPausedContainerSuccess(t *testing.T) {
	tree := pausedUpgrade(t)
	performOp(t, tree, "upgrade", OpSuccess, false)
	checkStatus(t, tree, "upgrade", StatusSuccess)
	if !tree.provided.Contains("upgraded") {
		t.Errorf("upgrade succeeded, it must provide its resources")
	}
	// the manual status must survive status recalculations
	tree.Root.updateStatusesDown(true)
	checkStatus(t, tree, "upgrade", StatusSuccess)
	checkStatus(t, tree, "reboot", StatusFailed) // children are not changed
}

func TestPausedContainerFail(t *testing.T) {
	tree := pausedUpgrade(t)
	performOp(t, tree, "upgrade", OpFail, false)
	checkStatus(t, tree, "upgrade", StatusFailed)
	tree.Root.updateStatusesDown(true)
	checkStatus(t, tree, "upgrade", StatusFailed)
	if tree.provided.Contains("upgraded") {
		t.Errorf("upgrade failed, it must not provide its resources")
	}
}

func TestPausedRunNodeFail(t *testing.T) {
	tree := loadTestTree(t, `
tree:
  id: root
  seq:
    - type: run
      id: job
      on_error: paused
      args: ["false"]
`)
	setStatus(tree, "job", StatusFailed)
	tree.Root.updateStatusesDown(true)
	checkStatus(t, tree, "job", StatusPaused)
	performOp(t, tree, "job", OpFail, false)
	tree.Root.updateStatusesDown(true)
	checkStatus(t, tree, "job", StatusFailed)
}

// The manual status of a seq/par node ends when an operation is performed on it, or on a node below it.
func TestManualStatusEndsOnOperation(t *testing.T) {
	// operation below the node
	tree := pausedUpgrade(t)
	performOp(t, tree, "upgrade", OpSuccess, false)
	performOp(t, tree, "reboot", OpResetToWaiting, false)
	checkStatus(t, tree, "reboot", StatusWaiting)
	checkStatus(t, tree, "upgrade", StatusWaiting)
	if tree.NodeById["upgrade"].WantManualStatus {
		t.Errorf("upgrade must not have manual status anymore")
	}

	// recursive operation on the node itself
	tree = pausedUpgrade(t)
	performOp(t, tree, "upgrade", OpSuccess, false)
	performOp(t, tree, "upgrade", OpResetToWaiting, true)
	checkStatus(t, tree, "upgrade", StatusWaiting)
	checkStatus(t, tree, "reboot", StatusWaiting)
}

func TestContainerFailSuccessOnlyWhenPaused(t *testing.T) {
	tree := loadTestTree(t, upgradeTree)
	tree.Root.updateStatusesDown(true)
	ops := tree.NodeById["upgrade"].AllowedManualOps(true, false)
	if _, ok := ops[OpSuccess]; ok {
		t.Errorf("success must not be allowed on a waiting seq node")
	}
	tree = pausedUpgrade(t)
	ops = tree.NodeById["upgrade"].AllowedManualOps(true, false)
	_, okSuccess := ops[OpSuccess]
	_, okFail := ops[OpFail]
	if !okSuccess || !okFail {
		t.Errorf("success and fail must be allowed on a paused seq node")
	}
}
