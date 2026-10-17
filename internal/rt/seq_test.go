package rt

import "testing"

// The steps set "on_error: failed", otherwise they would inherit "paused" from their seq node.
const seqTree = `
tree:
  id: root
  seq:
    - type: seq
      id: steps
      on_error: %s
      nodes:
        - type: run
          id: first
          on_error: %s
          args: ["false"]
        - type: run
          id: second
          on_error: failed
          args: ["true"]
`

// runningSeq returns the tree where the first step of the running seq node has just failed.
func runningSeq(t *testing.T, seqOnError, firstOnError string) *Tree {
	tree := loadTestTree(t, sprintf(seqTree, seqOnError, firstOnError))
	setStatus(tree, "root", StatusRunning)
	setStatus(tree, "steps", StatusRunning)
	setStatus(tree, "first", StatusFailed)
	tree.Root.updateStatusesDown(true)
	return tree
}

func runnable(tree *Tree) string {
	n := tree.findRunnable(tree.Root)
	if n == nil {
		return ""
	}
	return n.Id
}

// A seq node stops at a failed sub-node, it does not start the next one.
func TestSeqPausesAtFailedStep(t *testing.T) {
	tree := runningSeq(t, "paused", "failed")
	checkStatus(t, tree, "steps", StatusPaused)
	if id := runnable(tree); id != "" {
		t.Errorf("the next step must not be started, but %s is runnable", id)
	}
}

func TestSeqFailsAtFailedStep(t *testing.T) {
	tree := runningSeq(t, "failed", "failed")
	checkStatus(t, tree, "steps", StatusFailed)
	if id := runnable(tree); id != "" {
		t.Errorf("the next step must not be started, but %s is runnable", id)
	}
}

// A step that is allowed to fail (on_error: success) does not stop the seq node.
func TestSeqContinuesAfterAllowedFailure(t *testing.T) {
	tree := runningSeq(t, "paused", "success")
	checkStatus(t, tree, "first", StatusSuccess)
	checkStatus(t, tree, "steps", StatusRunning)
	if id := runnable(tree); id != "second" {
		t.Errorf("the next step must be runnable, got %q", id)
	}
}
