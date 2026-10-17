package rt

import (
	"fmt"
	"strings"
)

// AllowedManualOps tells the direct operations that can be performed on the node in its current state.
func (n *Node) AllowedManualOps(locked bool, recursive bool) map[int]struct{} {
	if !locked {
		panic("must be called in locked state")
	}

	ops := make(map[int]struct{})
	// recursive operations not allowed for nodes without children
	if recursive && !n.HasChild() {
		return ops
	}

	addOps := func(add ...NodeOp) {
		for _, op := range add {
			ops[op] = struct{}{}
		}
	}

	// recursive operations, node has children
	if recursive {
		addOps(OpFreeze, OpThaw, OpSkip, OpResetToWaiting)
		if !StatusFinished(n.Status) {
			addOps(OpSignal, OpFail, OpSuccess)
		}
		return ops
	}

	// non-recursive operations, node does not have children
	if n.HasChild() {
		if n.Status == StatusFrozen {
			addOps(OpThaw, OpSkip, OpResetToWaiting)
		}
		if n.Status == StatusSkipped {
			addOps(OpResetToWaiting)
		}
		if n.Status == StatusPaused {
			addOps(OpFail, OpSuccess)
		}
		return ops
	}

	// default: non-recursive operation, node doesn't have children
	if n.Status == StatusWaiting {
		addOps(OpFreeze, OpSkip)
	}
	if n.Status == StatusFrozen {
		addOps(OpThaw, OpSkip, OpResetToWaiting)
	}
	if n.Type != TypeRun {
		return ops
	}
	if n.Status == StatusRunning {
		addOps(OpSignal)
	}
	if !StatusActive(n.Status) && n.Type == TypeRun {
		addOps(OpRun)
	}
	if n.Status == StatusPaused {
		addOps(OpFail, OpSuccess, OpResetToWaiting)
	}
	if StatusFinished(n.Status) {
		addOps(OpResetToWaiting)
	}
	return ops
}

// OpArgs holds the optional arguments of an operation. A nil *OpArgs means the default arguments.
type OpArgs struct {
	// Unprovide is used by OpResetToWaiting and OpRun: the names of the provided resources that may be unprovided.
	// nil means all of them. See Node.PlanOp.
	Unprovide []string
}

// PerformOp should only be called manually, the scheduler uses its own ways. args can be nil.
//
// OpResetToWaiting and OpRun also unprovide the resources that were provided by the nodes that are reset or run again
// (or become unfinished), unless they are still provided by other nodes. args.Unprovide can limit the unprovided
// resources.
func (n *Node) PerformOp(op NodeOp, locked bool, recursive bool, manual bool, args *OpArgs) (bool, error) {
	if !locked {
		panic("must be called in locked state")
	}
	if _, allowed := n.AllowedManualOps(locked, recursive)[op]; !allowed {
		return false, fmt.Errorf("this operation is not allowed in %s state", StatusName(n.Status))
	}
	if manual {
		n.log().Info("operation: "+opLabel(op, recursive), attrEvent, EventOperation, "op", OpName(op),
			"recursive", recursive)
	}
	// An operation on a node ends the manual status of the seq/par nodes above it (see calcParSeqStatus), so their
	// statuses are calculated from their sub-nodes again.
	for p := n.Parent; p != nil; p = p.Parent {
		p.WantManualStatus = false
	}
	var before map[*Node]bool
	if unprovides(op) {
		before = n.Tree.providerNodes()
	}
	changed, err := n.performOp(op, locked, recursive, manual)
	// also when the operation failed: a finished node that could not be started again is failed
	if unprovides(op) {
		var names []string = nil
		if args != nil {
			names = args.Unprovide
		}
		if removed := n.Tree.unprovideLost(before, names); len(removed) > 0 {
			n.log().Info("unprovided "+strings.Join(removed, ", "), attrEvent, EventUnprovided,
				"resources", removed)
		}
	}
	if err != nil {
		if manual {
			n.log().Warn(fmt.Sprintf("operation %s failed: %s", opLabel(op, recursive), err), attrEvent,
				EventOperation, "op", OpName(op), "recursive", recursive, "error", err)
		}
		return changed, err
	}
	n.updateStatusesUp(true)
	return changed, nil
}

func (n *Node) performOp(op NodeOp, locked bool, recursive bool, manual bool) (bool, error) {
	_, allowed := n.AllowedManualOps(locked, recursive)[op]
	// TODO: if the operation is recursive, then it might be allowed on child nodes?
	if !allowed {
		return false, fmt.Errorf("this operation is not allowed in %s state", StatusName(n.Status))
	}

	var ms Status
	sm := ""
	changed, start := false, false
	// Set status manually on run node
	if n.Type == TypeRun {
		switch op {
		case OpFreeze:
			ms = StatusFrozen
		case OpThaw:
			ms = StatusWaiting
		case OpRun:
			start = true
			ms = StatusRunning
		case OpFail:
			ms = StatusFailed
		case OpSignal:
			return false, fmt.Errorf("internal error: signal operation must be sent using Node.Client.Signal")
		case OpSkip:
			ms = StatusSkipped
		case OpSuccess:
			ms = StatusSuccess
		case OpResetToWaiting:
			ms = StatusWaiting
			n.StartError = "" // from a previous start
		default:
			return false, fmt.Errorf("internal error: unknown operation: %s", OpName(op))
		}
		if manual {
			sm = "manually"
		}
		if StatusHoldsLocks(ms) {
			ok, msg := n.CanAcquireLocks(true)
			if !ok {
				return false, fmt.Errorf("cannot acquire locks: %s", msg)
			}
		}
		if start {
			n.StartError = ""
			err := n.Start(locked)
			if err != nil {
				return false, err
			}
		}
		n.WantManualStatus = true
		n.ManualStatus = ms
		n.SchedulerMessage = sm
		changed = true
	}
	// set status manually on seq/par node
	if n.Type == TypeSeq || n.Type == TypePar {
		// An operation on the node ends its manual status (fail/success below sets it again).
		if n.WantManualStatus {
			n.WantManualStatus = false
			changed = true
		}
		// Manually fail/success on paused seq/par node. It lasts until an operation is performed on this node, or on
		// a node below it.
		if n.Status == StatusPaused && (op == OpFail || op == OpSuccess) {
			ms = StatusFailed
			if op == OpSuccess {
				ms = StatusSuccess
			}
			n.WantManualStatus = true
			n.ManualStatus = ms
			n.SchedulerMessage = ""
			changed = true
		}
		// When a paused node is reset to waiting mode,  then we do not want it to have manual status anymore.
		// calcParStatus/calSeqStatus will calculate it
		if n.Status == StatusPaused && (op == OpResetToWaiting) {
			n.WantManualStatus = false
			n.SchedulerMessage = ""
			changed = true
		}
		// Set waiting / thaw frozen seq/par node, reset skipped seq/par node
		// This is **not** manual status, so later calcParStatus/calSeqStatus can change it.
		// However, calcParStatus/calSeqStatus will refrain from changing it from frozen or skipped to anything.
		if (n.Status == StatusFrozen && (op == OpResetToWaiting || op == OpThaw)) ||
			(n.Status == StatusSkipped && op == OpResetToWaiting) {
			n.WantManualStatus = false
			n.Status = StatusWaiting
			n.SchedulerMessage = ""
			changed = true
		}
	}

	if recursive {
		for i := range n.Nodes {
			sub := n.Nodes[i]
			ch, _ := sub.performOp(op, locked, sub.HasChild(), manual)
			if ch {
				changed = true
			}
		}
	}
	if changed {
		n.updateStatusesDown(true)
		n.updateStatusesUp(true)
	}
	return changed, nil
}

func (n *Node) performOnErrorSiblings(locked bool) {
	if !locked {
		panic("must be called in locked state")
	}

	if n.Parent == nil || n.OnError.Siblings == OpNone {
		return
	}
	op := n.OnError.Siblings
	n.log().Info(fmt.Sprintf("on_error: %s the siblings", strings.ToLower(OpName(op))), attrEvent, EventOnError,
		"op", OpName(op))
	for _, sib := range n.Parent.Nodes {
		if sib == n {
			continue
		}
		// Here we ignore all errors, because the only reason that it can fail is because the operation is not allowed
		// in the siblings current state.
		_, err := sib.PerformOp(op, true, sib.HasChild(), false, nil)
		if err != nil {
			sib.log().Debug("on_error operation of a sibling", "op", OpName(op), "error", err)
		}
	}
}

// opLabel is the name of an operation for messages, like on the buttons of the GUI
func opLabel(op NodeOp, recursive bool) string {
	label := OpName(op)
	if recursive {
		label += " all"
	}
	return label
}
