package rt

import "fmt"

// Unblock can be called manually to request a new lock calculation cycle.
// This is a debounced call, if you call it several times within a short amount of time, then
// possibly only some of the calls will be actually performed.
func (t *Tree) Unblock() {
	t.unblocker.Call(0)
}

func (t *Tree) debouncedUnblock(_ int) {
	t.unblock()
}

// unblock will recalculate required, provided, locked and unlocked resources for all nodes,
// update Node.Blocked and finally awake the scheduler if something was unblocked
func (t *Tree) unblock() {
	t.Lock("Tree.unblock")
	unblocked := t.Root.updateBlockedAll(true)
	t.UnLock()

	// If something was unblocked, then we awake the scheduler
	if unblocked {
		t.Awake()
	}
}

// updateBlockedAll updates Blocked, MissingRequired, CannotAcquire for the node and all of its sub-nodes
// returns true if at least one node was changed
func (n *Node) updateBlockedAll(locked bool) bool {
	if !locked {
		panic("updateBlockedAll must be called locked")
	}
	result := n.updateBlocked()
	for _, e := range n.Nodes {
		if e.updateBlockedAll(locked) {
			result = true
		}
	}
	return result
}

// updateBlocked will update Blocked, MissingRequired, CannotAcquire
// it returns true if any of these where changed
func (n *Node) updateBlocked() bool {
	// missing required: we require it, and it was not yet provided
	mr := n.Calculated.Requires.Difference(n.Tree.provided)
	// cannot xacquire: we list it after xlocks but the tree already have it xacquired, OR racquired
	cxl := n.Calculated.XLocks.Intersection(n.Tree.xAcquired.Union(n.Tree.rAcquired))
	// except the ones that we already hold
	for _, res := range cxl.List() {
		if n.Tree.xAcquiredBy[res] == n {
			cxl.Remove(res)
		}
	}
	// cannot racquire: we list it after rlocks, and we are the rlock-root,
	// but it is xacquired or rackuired by a different node
	crl := n.Calculated.RLocks.Intersection(n.Tree.xAcquired.Union(n.Tree.rAcquired))
	// except the ones that we already hold, OR the ones that we are note the rlock-root for
	for _, res := range crl.List() {
		ab, ok := n.Tree.rAcquiredBy[res]
		// the node is already the rlock holder
		if ok && ab == n {
			crl.Remove(res)
		}
		// the lock is held by our rlock-root
		if ok && n.getRLockRoot(res, true) == ab {
			crl.Remove(res)
		}
	}

	changed := !n.MissingRequired.Equals(mr) || !n.CannotXAcquire.Equals(cxl) || !n.CannotRAcquire.Equals(crl)

	n.MissingRequired = mr
	n.CannotXAcquire = cxl
	n.CannotRAcquire = crl
	n.Blocked = !mr.Empty() || !cxl.Empty() || !crl.Empty()

	return changed
}

// registerProvided adds the node's provided keys to the tree's provided keys
// used for provides/requires
func (n *Node) registerProvided(locked bool) {
	if !locked {
		panic("registerProvided must be called locked")
	}

	a := n.Calculated.Provides.Difference(n.Tree.provided)
	if a.Empty() {
		return
	}

	// register the new provided resources
	n.Tree.provided.UnionInPlace(a)
	for _, key := range a.List() {
		n.Tree.providedBy[key] = n
	}
	n.Tree.Unblock()
}

func (n *Node) CanAcquireLocks(locked bool) (bool, string) {
	if !locked {
		panic("CanAcquireLocks must be called tree-locked")
	}
	t := n.Tree
	for _, res := range n.Calculated.XLocks.List() {
		a, ok := t.xAcquiredBy[res]
		if ok && a != n {
			return false, fmt.Sprintf("cannot acquire xlock %s in node %s, already acquired by node %s",
				res, n.Id, a.Id)
		}
	}
	for _, res := range n.Calculated.RLocks.List() {
		rn := n.getRLockRoot(res, locked)
		a, ok := t.rAcquiredBy[res]
		// It is acquired by a different lock-root
		if ok && a != rn {
			return false, fmt.Sprintf("cannot acquire rlock %s in node %s, already acquired by node %s",
				res, n.Id, a.Id)
		}
	}
	return true, ""
}

// getRLockHolder returns the topmost node that lists the given r-lock, relative to this node.
// If n has a (direct or indirect) parent that lists the lock after rlocks, then it is returned.
// Otherwise, this n is returned. Caution: you must call this method for a lock that is listed in
// n.rlocks
// TODO: cache this after building the runtree! This should be much faster!
func (n *Node) getRLockRoot(lockName string, locked bool) *Node {
	if !locked {
		panic("CanAcquireLocks must be called tree-locked")
	}
	if !n.Calculated.RLocks.Contains(lockName) {
		panic(fmt.Sprintf("getRLockHolder: node %s does not hold rlock %s", n.Id, lockName))
	}
	result, n2 := n, n
	for n2.Parent != nil {
		n2 = n2.Parent
		if n2.Calculated.RLocks.Contains(lockName) {
			result = n2
		}
	}
	return result
}

func (n *Node) acquireLocks(locked bool) {
	if !locked {
		panic("acquireLocks must be called tree-locked")
	}
	t := n.Tree
	for _, res := range n.Calculated.XLocks.List() {
		a, ok := t.xAcquiredBy[res]
		if ok {
			panic(fmt.Sprintf("acquireLocks: cannot acquire lock %s in node %s, already acquired by node %s",
				res, n.Id, a.Id))
		}
		t.xAcquiredBy[res] = n
		t.xAcquired.Add(res)
	}
	for _, res := range n.Calculated.RLocks.List() {
		rn := n.getRLockRoot(res, locked)
		a, ok := t.rAcquiredBy[res]
		// It is acquired by a different lock-root
		if ok && a != rn {
			panic(fmt.Sprintf("cannot acquire rlock %s in node %s, already acquired by node %s",
				res, n.Id, a.Id))
		}
		if !ok {
			t.rAcquiredBy[res] = rn
			t.rAcquired.Add(res)
		}
	}
}

func (n *Node) releaseLocks(locked bool) {
	if !locked {
		panic("releaseLocks must be called tree-locked")
	}
	t := n.Tree
	unlocked := false
	for _, res := range n.Calculated.XLocks.List() {
		n2, ok := t.xAcquiredBy[res]
		/*
			if !ok {
				panic("releaseLocks: cannot release lock, not acquired")
			}
			if n2 != n {
				panic("releaseLocks: cannot release lock, acquired by a different node")
			}
		*/
		if ok && n2 == n {
			delete(t.xAcquiredBy, res)
			t.xAcquired.Remove(res)
		}
		unlocked = true
	}
	for _, res := range n.Calculated.RLocks.List() {
		n2, ok := t.rAcquiredBy[res]
		if ok && n2 == n {
			delete(t.rAcquiredBy, res)
			t.rAcquired.Remove(res)
		}
		unlocked = true
	}
	// if something was unlocked, then possibly something can be started
	if unlocked {
		t.Awake()
	}
}
