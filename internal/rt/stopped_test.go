package rt

import "testing"

const stoppedTree = `
tree:
  id: root
  seq:
    - id: pre
      args: ["true"]
    - id: hosts
      par:
        - id: web1
          seq:
            - id: web1-upgrade
              args: ["true"]
        - id: web2
          seq:
            - id: web2-upgrade
              args: ["true"]
        - id: web3
          seq:
            - id: web3-upgrade
              args: ["true"]
    - id: post
      args: ["true"]
`

// A running seq or par node is stopped when nothing runs below it, and the scheduler stops at paused or frozen nodes
// below it: a failed host of a rollout is paused, the hosts after it are frozen.
func TestStopped(t *testing.T) {
	rollout := func(t *testing.T) *Tree {
		tree := loadTestTree(t, stoppedTree)
		setStatus(tree, "root", StatusRunning)
		setStatus(tree, "pre", StatusSuccess)
		setStatus(tree, "hosts", StatusRunning)
		setStatus(tree, "web1", StatusSuccess)
		setStatus(tree, "web1-upgrade", StatusSuccess)
		setStatus(tree, "web2", StatusPaused)
		setStatus(tree, "web2-upgrade", StatusFailed)
		setStatus(tree, "web3", StatusFrozen)
		setStatus(tree, "web3-upgrade", StatusFrozen)
		return tree
	}
	stopped := func(tree *Tree, id string) string {
		return tree.NodeById[id].Stopped(true)
	}

	tree := rollout(t)
	for _, id := range []string{"root", "hosts"} {
		if got, want := stopped(tree, id), "stopped: 1 paused, 1 frozen"; got != want {
			t.Errorf("%s: %q, want %q", id, got, want)
		}
	}
	// only running seq and par nodes are stopped
	for _, id := range []string{"web2", "web3", "web2-upgrade", "post"} {
		if got := stopped(tree, id); got != "" {
			t.Errorf("%s: %q, want nothing", id, got)
		}
	}

	t.Run("a process runs below it", func(t *testing.T) {
		tree := rollout(t)
		setStatus(tree, "web3", StatusRunning)
		setStatus(tree, "web3-upgrade", StatusRunning)
		tree.NodeById["web3-upgrade"].IncProc(true)
		if got := stopped(tree, "hosts"); got != "" {
			t.Errorf("hosts: %q, want nothing", got)
		}
	})
	t.Run("a node can be started below it", func(t *testing.T) {
		tree := rollout(t)
		setStatus(tree, "web3", StatusWaiting)
		setStatus(tree, "web3-upgrade", StatusWaiting)
		if got := stopped(tree, "hosts"); got != "" {
			t.Errorf("hosts: %q, want nothing", got)
		}
	})
	t.Run("a node waits for a lock below it", func(t *testing.T) {
		tree := rollout(t)
		setStatus(tree, "web3", StatusWaiting)
		setStatus(tree, "web3-upgrade", StatusWaiting)
		tree.NodeById["web3"].Blocked = true
		if got := stopped(tree, "hosts"); got != "" {
			t.Errorf("hosts: %q, want nothing", got)
		}
	})
	t.Run("only paused", func(t *testing.T) {
		tree := rollout(t)
		setStatus(tree, "web3", StatusSuccess)
		setStatus(tree, "web3-upgrade", StatusSuccess)
		if got, want := stopped(tree, "root"), "stopped: 1 paused"; got != want {
			t.Errorf("root: %q, want %q", got, want)
		}
	})
	t.Run("a seq does not look past its first unfinished node", func(t *testing.T) {
		tree := loadTestTree(t, stoppedTree)
		setStatus(tree, "root", StatusRunning)
		setStatus(tree, "pre", StatusPaused)
		if got, want := stopped(tree, "root"), "stopped: 1 paused"; got != want {
			t.Errorf("root: %q, want %q (post and the hosts are not started after pre)", got, want)
		}
	})
}
