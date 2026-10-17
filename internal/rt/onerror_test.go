package rt

import (
	"strings"
	"testing"
)

const onErrorTree = `
tree:
  id: root
  seq:
    - type: run
      id: plain
      args: ["true"]
    - type: seq
      id: upgrade
      on_error: paused
      children_on_error: [failed, freeze]
      nodes:
        - type: run
          id: step
          args: ["true"]
        - type: seq
          id: sub
          nodes:
            - type: run
              id: deep
              args: ["true"]
        - type: run
          id: allowed
          on_error: success
          args: ["true"]
    - type: seq
      id: classic
      on_error: paused
      nodes:
        - type: run
          id: child
          args: ["true"]
`

func TestChildrenOnError(t *testing.T) {
	tree := loadTestTree(t, onErrorTree)
	failedFreeze := OnError{Status: StatusFailed, Siblings: OpFreeze}
	for id, want := range map[string]OnError{
		"root":    DefaultOnError,
		"plain":   DefaultOnError,
		"upgrade": {Status: StatusPaused, Siblings: OpNone},
		"step":    failedFreeze, // from children_on_error
		"sub":     failedFreeze,
		"deep":    failedFreeze, // sub has no children_on_error, it passes its on_error down
		"allowed": {Status: StatusSuccess, Siblings: OpNone},
		"classic": {Status: StatusPaused, Siblings: OpNone},
		"child":   {Status: StatusPaused, Siblings: OpNone}, // inherited on_error, like before
	} {
		if got := tree.NodeById[id].OnError; got != want {
			t.Errorf("%s on_error is %+v, want %+v", id, got, want)
		}
	}
	if got := tree.NodeById["upgrade"].ChildrenOnError; got != failedFreeze {
		t.Errorf("upgrade children_on_error is %+v", got)
	}
}

func TestChildrenOnErrorInvalid(t *testing.T) {
	_, err := parseTestTree(t, `
tree:
  id: root
  children_on_error: [failed, paused]
  seq:
    - type: run
      args: ["true"]
`)
	if err == nil || !strings.Contains(err.Error(), "children_on_error") {
		t.Errorf("invalid children_on_error must be rejected, got error %v", err)
	}
}

// The step fails (it does not pause), and the seq node pauses, so the next step is not started.
func TestChildrenOnErrorWithSeq(t *testing.T) {
	tree := loadTestTree(t, `
tree:
  id: root
  seq:
    - type: seq
      id: steps
      on_error: paused
      children_on_error: failed
      nodes:
        - type: run
          id: first
          args: ["false"]
        - type: run
          id: second
          args: ["true"]
`)
	setStatus(tree, "root", StatusRunning)
	setStatus(tree, "steps", StatusRunning)
	setStatus(tree, "first", StatusFailed)
	tree.Root.updateStatusesDown(true)
	checkStatus(t, tree, "first", StatusFailed)
	checkStatus(t, tree, "steps", StatusPaused)
	if id := runnable(tree); id != "" {
		t.Errorf("the next step must not be started, but %s is runnable", id)
	}
}
