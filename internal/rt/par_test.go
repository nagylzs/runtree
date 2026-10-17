package rt

import "testing"

func TestFailedRunNodes(t *testing.T) {
	tree := loadTestTree(t, `
tree:
  id: root
  par:
    - type: run
      id: a
      on_error: failed
      args: ["false"]
    - type: seq
      id: group
      nodes:
        - type: run
          id: b
          on_error: failed
          args: ["false"]
        - type: run
          id: c
          args: ["true"]
`)
	setStatus(tree, "a", StatusFailed)
	setStatus(tree, "b", StatusFailed)
	setStatus(tree, "c", StatusSuccess)
	for id, want := range map[string]int{"root": 2, "group": 1, "a": 0} {
		if got := tree.NodeById[id].FailedRunNodes(true); got != want {
			t.Errorf("%s: %d failed run nodes, want %d", id, got, want)
		}
	}
}

// The rolling upgrade example in the README: one server at a time, and the first failure freezes the servers
// that were not started yet.
func TestRollingUpgradeStopsAtFirstFailure(t *testing.T) {
	tree := loadTestTree(t, `
tree:
  id: root
  par:
    - type: par
      id: servers
      max_proc: 1
      children_on_error: [failed, freeze]
      nodes:
        - type: run
          id: server1
          args: ["ssh", "server1", "upgrade"]
        - type: run
          id: server2
          args: ["ssh", "server2", "upgrade"]
        - type: run
          id: server3
          args: ["ssh", "server3", "upgrade"]
`)
	setStatus(tree, "root", StatusRunning)
	setStatus(tree, "servers", StatusRunning)
	tree.NodeById["server1"].StartError = "failed in a test" // it becomes failed with the next status update
	tree.Root.updateStatusesDown(true)

	checkStatus(t, tree, "server1", StatusFailed)
	checkStatus(t, tree, "server2", StatusFrozen)
	checkStatus(t, tree, "server3", StatusFrozen)
	checkStatus(t, tree, "servers", StatusRunning) // until the frozen servers are thawed or skipped
	if got := tree.NodeById["servers"].FailedRunNodes(true); got != 1 {
		t.Errorf("servers: %d failed run nodes, want 1", got)
	}
	if id := runnable(tree); id != "" {
		t.Errorf("no server must be started, but %s is runnable", id)
	}
}
