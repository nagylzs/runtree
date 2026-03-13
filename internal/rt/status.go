package rt

import (
	"fmt"
	"time"

	"github.com/nagylzs/runtree/internal/runner"
)

// updateStatusesDown updates the status for the node and all of its sub-nodes
// It is called periodically by the scheduler, it will never trigger a new schedule.
// It must be called in locked state.
// Also see https://gitlab.mess.hu/devops/runtree#node-states
func (n *Node) updateStatusesDown(locked bool) {
	if !locked {
		panic("must be called in locked state")
	}

	for _, c := range n.Nodes {
		c.updateStatusesDown(locked)
	}
	n.updateStatus(locked)
}

// UpdateStatusesUp updates the status for the node and all of its parents
// It must be called in locked state.
// It is called when the pty server sends a process state change event.
// If at least one node changes status, then it triggers a single new schedule.
func (n *Node) updateStatusesUp(locked bool) {
	if !locked {
		panic("must be called in locked state")
	}

	changed, t := false, n.Tree
	for n != nil {
		statusChanged, _ := n.updateStatus(locked)
		if statusChanged {
			changed = true
		}
		n = n.Parent
	}
	println("updateStatusesUp", changed)
	if changed {
		t.Awake()
	}
}

// updateStatus updates Node.Status to reflect its current state, returns true if the status was changed
// It **must** be called from updateStatusesDown **after** calling on its sub-nodes
// This method **must not** alter the statuses of its children.
// This method **may** perform operations on its siblings.
// This method **may** alter the Expanded property of the node.
func (n *Node) updateStatus(locked bool) (statusChanged bool, expandedChanged bool) {
	if !locked {
		panic("must be called in locked state")
	}
	prevStatus, prevExpanded := n.Status, n.Expanded
	var s Status
	var oe bool
	switch n.Type {
	case TypeRun:
		s, oe = n.calcRunStatus()
	case TypeSeq:
		s, oe = n.calcParSeqStatus()
	case TypePar:
		s, oe = n.calcParSeqStatus()
	default:
		panic("Node.UpdateStatus: unknown node type")
	}
	statusChanged = prevStatus != s
	n.Status = s

	// After a manual node restart, we return to auto status updates.
	if n.WantManualStatus && n.Status != StatusSuccess && n.Status != StatusWaiting {
		n.WantManualStatus = false
	}

	/*
		// e.g. when the process exits
		// if you leave "statusChanged &&" here then manually started nodes will always have WantManualStatus,
		// and their parents will never be completed.
		if n.WantManualStatus && n.Status == StatusRunning {
			n.WantManualStatus = false
		}
	*/

	// when the node transitions from not finished to finished, then it provides
	if !StatusFinished(prevStatus) && StatusFinished(s) && !StatusHasError(s) {
		n.registerProvided(true)
	}
	// when the node transitions from inactive to active or back, then it xlocks or xunlocks
	oldLocker, newLocker := StatusHoldsLocks(prevStatus), StatusHoldsLocks(s)
	if !oldLocker && newLocker {
		n.acquireLocks(true)
	}
	if oldLocker && !newLocker {
		n.releaseLocks(true)
	}
	oldActive, newActive := StatusActive(prevStatus), StatusActive(s)
	if !oldActive && newActive {
		if n.ExpandOnActive && !n.Expanded {
			println("expanding", n.Id)
			n.Expanded = true
			expandedChanged = true
			/*
				we do not need to traverse up, because the node is made active when pty/terminal says so,
				and it already calls updateStatusesUp()
			*/
		}
	}
	if !oldActive && newActive {
		n.Started = time.Now()
		n.totalProcCommitedTo = n.Started
		n.Finished = time.Time{}
	}
	if oldActive && !newActive {
		n.Finished = time.Now()
		n.elapsed += n.Finished.Sub(n.Started)
		if n.Type == TypeRun && !n.totalProcCommitedTo.IsZero() && !n.Finished.IsZero() {
			n.addTotalElapsed(n.Finished.Sub(n.totalProcCommitedTo))
			n.totalProcCommitedTo = n.Finished
		}
	}
	oldFinished, newFinished := StatusFinished(prevStatus), StatusFinished(s)
	if !oldFinished && newFinished {
		if n.CollapseOnFinished && n.Expanded && n.Parent != nil {
			println("collapsing", n.Id)
			n.Expanded = false
			expandedChanged = true
		}
	}

	// If the new status is coming from OnError, then we process siblings
	if statusChanged && !StatusHasError(prevStatus) && oe && n.OnError.Siblings != OpNone && n.Parent != nil {
		n.performOnErrorSiblings(true)
	}

	// provided and blocked can also change here, but we dot not notify about those.
	if statusChanged || expandedChanged {
		if n.Tree.terminalHelper != nil {
			n.Tree.terminalHelper.OnNodeChanged(n, prevStatus, prevExpanded)
		}
	}

	return statusChanged, expandedChanged
}

// calcRunStatus calculates current status of a TypeRun node, and it tells if it comes from OnError
func (n *Node) calcRunStatus() (Status, bool) {
	if n.WantManualStatus {
		return n.ManualStatus, false
	}

	if n.Status == StatusCancelled || n.Status == StatusFrozen {
		return n.Status, false
	}

	// special case: there is no client, because the client could not be started
	// this results in failed status without a client "last state" being present
	// warning: if you change this, then you may need to change Node.hasError()
	var hasError bool
	var ls runner.ProcState
	if n.StartError != "" {
		hasError = true
	} else {
		c := n.Client
		if c == nil {
			return StatusWaiting, false
		}
		ls = c.LastState()
		hasError = !ls.Alive() && (ls.Error != "" || ls.ExitCode != 0)
	}

	// when the user manually restarts the Run node in Paused state, then it calls
	// updateStatus, and it calls calcRunStatus. But in that case, hasError is false (because
	// restarting the node clears the error)...
	if n.Status == StatusPaused && hasError {
		// ...so here we already know that the node was not restarted, but the process was
		// finished with error, this is why it is paused, and it should remain so until it is
		// manually restarted
		return n.Status, false
	}

	if hasError {
		return n.OnError.Status, true
	}

	if !ls.Alive() && ls.ExitCode == 0 {
		return StatusSuccess, false
	}
	if !ls.Alive() {
		panic("internal error in Node.calcRunStatus")
	}
	return StatusRunning, false
}

// calcParStatus calculates current status of a TypePar or TypeSeq node, and it tells if it is coming from OnError
func (n *Node) calcParSeqStatus() (Status, bool) {
	/* ???
	if n.Status == StatusPaused {
		panic("internal error in Node.calcParSeqStatus: par and seq nodes cannot be paused, yet it is?")
	}
	*/
	if n.WantManualStatus {
		panic(fmt.Sprintf(
			"internal error in Node.calcParSeqStatus: par and seq nodes must not WantManualStatus, but got %s",
			StatusName(n.ManualStatus)))
	}

	if n.Status == StatusCancelled || n.Status == StatusFrozen {
		return n.Status, false
	}

	hasActive, hasError, hasNotFinished := false, false, false
	for _, c := range n.Nodes {
		if StatusActive(c.Status) {
			hasActive = true
		}
		if StatusHasError(c.Status) {
			hasError = true
		}
		if !StatusFinished(c.Status) {
			hasNotFinished = true
		}
	}
	// If the par node is already running, then it will stay that way until all sub-nodes are finished.
	// This ensures that the par node holds its acquired locks until its sub-nodes are all finished.
	if n.Status == StatusRunning {
		if hasNotFinished {
			return StatusRunning, false
		}
	}
	if hasActive {
		return StatusRunning, false
	}
	if hasError {
		return n.OnError.Status, true
	}
	if hasNotFinished {
		return StatusWaiting, false
	}
	return StatusSuccess, false
}

// commit elapsed time to the total, called periodically by the scheduler
func (n *Node) commitTotalElapsed(locked bool) {
	if !locked {
		panic("must be called in locked state")
	}
	if n.Type != TypeRun || !StatusActive(n.Status) || n.Started.IsZero() {
		return
	}
	now := time.Now()
	elapsed := now.Sub(n.totalProcCommitedTo)
	n.addTotalElapsed(elapsed)
	n.totalProcCommitedTo = now
}
