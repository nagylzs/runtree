package rt

import "fmt"

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
		addOps(OpFreeze, OpMelt, OpCancel)
		if !StatusFinished(n.Status) {
			addOps(OpSignal, OpFail, OpSuccess)
		}
		return ops
	}

	// non-recursive operations, node does not have children
	if n.HasChild() {
		if n.Status == StatusFrozen {
			addOps(OpMelt, OpCancel)
		}
		if !StatusFinished(n.Status) {
			addOps(OpFail, OpSuccess)
		}
		return ops
	}

	// default: non-recursive operation, node doesn't have children
	if n.Status == StatusWaiting {
		addOps(OpFreeze, OpCancel)
	}
	if n.Status == StatusFrozen {
		addOps(OpMelt, OpCancel)
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
		addOps(OpFail, OpCancel, OpSuccess)
	}
	return ops
}

// PerformOp should only be called manually, the scheduler uses its own ways.
func (n *Node) PerformOp(op NodeOp, locked bool, recursive bool, manual bool) (bool, error) {
	if !locked {
		panic("must be called in locked state")
	}

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
		case OpMelt:
			ms = StatusWaiting
		case OpRun:
			start = true
			ms = StatusRunning
		case OpFail:
			ms = StatusFailed
		case OpSignal:
			return false, fmt.Errorf("internal error: signal operation must be sent using Node.Client.Signal")
		case OpCancel:
			ms = StatusCancelled
		case OpSuccess:
			ms = StatusSuccess
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
		// Manually fail/success on paused seq/par node
		if n.Status == StatusPaused && (op == OpFail || op == OpSuccess) {
			n.WantManualStatus = true
			n.ManualStatus = ms
			n.SchedulerMessage = ""
			changed = true
		}
	}

	if recursive {
		for i := range n.Nodes {
			sub := n.Nodes[i]
			ch, _ := sub.PerformOp(op, locked, sub.HasChild(), manual)
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
	for _, sib := range n.Parent.Nodes {
		if sib == n {
			continue
		}
		// Here we ignore all errors, because the only reason that it can fail is because the operation is not allowed
		// in the siblings current state.
		println(sib.Id+" "+StatusName(sib.Status), "->", OpName(n.OnError.Siblings))
		_, err := sib.PerformOp(n.OnError.Siblings, true, sib.HasChild(), false)
		if err != nil {
			sib.Tree.logger.Error("performOnErrorSiblings: %w", err)
		}
	}
}
