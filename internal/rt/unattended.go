package rt

import (
	"fmt"
	"slices"
	"strings"
)

// An unattended tree has nobody to watch it and decide (runtree-headless): a node that would pause fails instead (see
// onErrorStatus), the processes have no terminals (their output only goes to the OutputRecorder), and the run ends
// when the root is finished, or when nothing runs and nothing can be started anymore (e.g. frozen nodes, that nobody
// thaws). Nothing else changes the tree then: there are no operations, and only the running processes can change
// the statuses.

// SetUnattended makes the tree unattended, see above. It must be called before the tree runs.
func (t *Tree) SetUnattended() {
	t.unattended = true
	t.ended = make(chan struct{})
}

// End is how an unattended run of the tree ended
type End struct {
	Status  Status // the status of the root
	Stopped bool   // the root is not finished: nothing runs, and nothing can be started
	Holds   []Hold // when it is stopped: the nodes where the scheduler stops, in the order of the tree
}

// Hold is an unfinished node where the scheduler stops: nothing below it can be started
type Hold struct {
	Node   *Node
	Reason string // e.g. "frozen (status: frozen)", "requires db (nothing has provided it)"
}

// Ended is closed when an unattended run of the tree ends (it is nil for a tree that is not unattended), then End
// tells how it ended.
func (t *Tree) Ended() <-chan struct{} {
	return t.ended
}

// End tells how an unattended run of the tree ended, after Ended is closed
func (t *Tree) End() End {
	<-t.ended
	return t.end
}

// checkEnd ends the unattended run when the root is finished, or when nothing runs and nothing can be started. It is
// called by the scheduler when it found nothing to start, in locked state.
//
// The decision is deterministic: no process runs or starts (only they can change the tree without an operation), and
// the statuses are a fixed point (updating them once more changes nothing, so the next schedule cycles would find the
// same: nothing to start).
func (t *Tree) checkEnd() {
	select {
	case <-t.ended:
		return
	default:
	}
	for _, n := range t.Nodes {
		if n.Type == TypeRun && (StatusActive(n.Status) || n.starting) {
			return
		}
	}
	if StatusFinished(t.Root.Status) {
		t.setEnd(End{Status: t.Root.Status})
		return
	}
	before := t.schedState()
	t.Root.updateStatusesDown(true)
	t.Root.updateBlockedAll(true)
	if !slices.Equal(before, t.schedState()) {
		t.Awake() // something has changed: the next cycle checks again
		return
	}
	var holds []Hold
	if n := t.findRunnableHold(t.Root, func(n *Node) {
		holds = append(holds, Hold{Node: n, Reason: n.holdReason()})
	}); n != nil {
		t.Awake() // cannot happen: the statuses have not changed since the scheduler found nothing to start
		return
	}
	t.setEnd(End{Status: t.Root.Status, Stopped: true, Holds: holds})
}

func (t *Tree) setEnd(end End) {
	t.end = end
	close(t.ended)
	if end.Stopped {
		t.logger.Warn("the run stopped: nothing runs, and nothing can be started", attrEvent, EventStopped)
	}
}

// schedState is the state of the nodes that the scheduler looks at (see findRunnable), and the numbers of the
// provided resources and the locks
func (t *Tree) schedState() []int {
	state := make([]int, 0, 2*len(t.Nodes)+3)
	for _, n := range t.Nodes {
		blocked := 0
		if n.Blocked {
			blocked = 1
		}
		state = append(state, int(n.Status), blocked)
	}
	return append(state, t.provided.Size(), len(t.xAcquiredBy), len(t.rAcquiredBy))
}

// holdReason tells why the scheduler stops at the node (see findRunnableHold)
func (n *Node) holdReason() string {
	switch {
	case n.Status == StatusFrozen:
		return "frozen (" + n.frozenBy() + ")"
	case n.Status == StatusPaused:
		return "paused"
	case n.MaxProc >= 0 && n.NProc >= n.MaxProc:
		return fmt.Sprintf("max_proc is %d", n.MaxProc)
	}
	var reasons []string
	for _, name := range sorted(n.MissingRequired.List()) {
		reasons = append(reasons, fmt.Sprintf("requires %s (nothing has provided it)", name))
	}
	for _, name := range sorted(append(n.CannotXAcquire.List(), n.CannotRAcquire.List()...)) {
		holder := n.Tree.xAcquiredBy[name]
		if holder == nil {
			holder = n.Tree.rAcquiredBy[name]
		}
		by := ""
		if holder != nil {
			by = fmt.Sprintf(" (%s holds it, it is %s)", holder.DisplayLabel(), StatusName(holder.Status))
		}
		reasons = append(reasons, "cannot lock "+name+by)
	}
	if len(reasons) == 0 {
		return "blocked"
	}
	return strings.Join(reasons, ", ")
}

// frozenBy tells where the frozen status of a node comes from: the tree file, or an operation (in an unattended tree,
// only on_error can freeze nodes). A seq/par node can be frozen because its sub-nodes are frozen.
func (n *Node) frozenBy() string {
	if n.Type == TypeRun || n.held == StatusFrozen {
		if n.frozenInFile {
			return "status: frozen"
		}
		return "by on_error"
	}
	for _, c := range n.Nodes {
		if c.Status == StatusFrozen {
			return c.frozenBy()
		}
	}
	return "its sub-nodes are frozen"
}

func sorted(names []string) []string {
	slices.Sort(names)
	return slices.Compact(names)
}
