package rt

import (
	"time"

	"github.com/nagylzs/runtree/internal/runner"
)

// remoteGrace is the time that a process of a remote runner gets after SIGINT, and after SIGTERM, see TerminateRemote
var remoteGrace = []time.Duration{runner.TerminateGraceInt, runner.TerminateGraceTerm}

// TerminateRemote terminates the processes of the running nodes on ssh and TCP runners when runtree exits (the
// scheduler must be stopped, it must be called in unlocked state): it sends SIGINT to them, then SIGTERM and SIGKILL
// after the grace periods (a grace period ends when none of the processes that still run got the signal, e.g. SIGTERM
// on Windows), and it waits until their exits are logged, at most for timeout. (The local runner
// terminates its own processes, see runner.StopLocal.) The rtrunner terminates the rest (e.g. the other processes of
// the group) when runtree disconnects. It returns the number of processes that were running.
func (t *Tree) TerminateRemote(timeout time.Duration) int {
	deadline := time.Now().Add(timeout)
	// the nodes that are connecting now have their process soon
	for t.remoteNodes(func(n *Node) bool { return n.starting }) != nil && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}
	running := func() []*Node {
		return t.remoteNodes(func(n *Node) bool {
			if n.Client == nil || !StatusActive(n.Status) {
				return false
			}
			ps := n.Client.LastState()
			return ps.Alive()
		})
	}
	nodes := running()
	if len(nodes) == 0 {
		return 0
	}
	t.logger.Warn("terminating the processes of the remote runners", "processes", len(nodes))
	for i, sig := range []string{"SIGINT", "SIGTERM", "SIGKILL"} {
		alive := running()
		if len(alive) == 0 {
			break
		}
		sigErrors := make(map[*runner.Client]uint64, len(alive))
		t.Lock("Tree.TerminateRemote")
		for _, n := range alive {
			sigErrors[n.Client] = n.Client.SignalErrors()
			_, _ = n.SendSignal(true, sig) // it is logged
		}
		t.UnLock()
		grace := 5 * time.Second // after SIGKILL: until the exit is logged
		if i < len(remoteGrace) {
			grace = remoteGrace[i]
		}
		// notSent tells if processes still run, but none of them got the signal
		notSent := func() bool {
			var still, got int
			t.remoteNodes(func(n *Node) bool {
				if n.Client == nil || !StatusActive(n.Status) {
					return false
				}
				if ps := n.Client.LastState(); !ps.Alive() {
					return false
				}
				still++
				if e, ok := sigErrors[n.Client]; !ok || n.Client.SignalErrors() == e {
					got++
				}
				return false
			})
			return still > 0 && got == 0
		}
		for end := time.Now().Add(grace); time.Now().Before(end) && time.Now().Before(deadline); {
			// waits for the status change too, that logs the exit
			if t.remoteNodes(func(n *Node) bool { return StatusActive(n.Status) && n.Client != nil }) == nil {
				return len(nodes)
			}
			if i < len(remoteGrace) && notSent() {
				break // the next signal at once
			}
			time.Sleep(50 * time.Millisecond)
		}
	}
	return len(nodes)
}

// remoteNodes returns the run nodes on ssh and TCP runners for which match is true, it locks the tree
func (t *Tree) remoteNodes(match func(n *Node) bool) []*Node {
	t.RLock("Tree.remoteNodes")
	defer t.RUnlock()
	var res []*Node
	for _, n := range t.Nodes {
		if n.Type == TypeRun && n.Calculated.Runner != runner.LocalAddress && match(n) {
			res = append(res, n)
		}
	}
	return res
}
