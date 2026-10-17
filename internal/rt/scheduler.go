package rt

import (
	"time"

	"github.com/nagylzs/runtree/internal/signal"
)

// HZ - when no event happens, then the scheduler will still try to schedule in HZ intervals.
// It is also used to update total elapsed times, so if you reduce this from one second, then
// the gui may not update total elapsed and speedup ratios every second!
const HZ = time.Second

// Run is the main scheduler loop. You need to call this as a goroutine, to start scheduling.
// Within this loop, the scheduler keeps processing nodes until there is nothing to be processed.
// Then it stops until it is awakened by a Schedule() call, or HZ elapses.
func (t *Tree) Run() {
	t.logger.Info("scheduler started", attrEvent, EventSchedulerStarted, "nodes", len(t.Nodes))
	for !signal.IsStopping() {
		t.Awake()
		time.Sleep(HZ)
	}
}

// Awake can be called manually to request a new schedule cycle.
// This is a debounced call, if you call it several times within a short amount of time, then
// possibly only some of the calls will be actually performed.
func (t *Tree) Awake() {
	t.awaker.Call(0)
}

func (t *Tree) debouncedSchedule(_ int) {
	t.schedule()
}

// schedule must be called in unlocked state
func (t *Tree) schedule() {
	defer t.recoverPanic("scheduler")
	began := time.Now()
	var started *Node
	defer func() {
		t.logger.Debug("schedule cycle", "elapsed", time.Since(began), "started", started)
	}()

	t.Lock("schedule")
	defer t.UnLock()

	// commit to total elapsed
	for _, node := range t.Nodes {
		node.commitTotalElapsed(true)
	}

	// update all statuses dependencies and resource locks
	t.Root.updateStatusesDown(true)
	t.Root.updateBlockedAll(true)

	// find a runnable node
	n := t.findRunnable(t.Root)
	if n == nil {
		return
	}
	started = n
	_ = n.Start(true)

	t.Awake() // probably not needed, starting the node will awake the scheduler anyway
}

// findRunnable must be called locked
func (t *Tree) findRunnable(n *Node) *Node {
	if n == nil {
		return nil
	}

	if StatusFinished(n.Status) {
		n.SchedulerMessage = "finished"
		return nil
	}

	if n.Status == StatusFrozen {
		n.SchedulerMessage = "won't schedule"
		return nil
	}

	if n.Status == StatusPaused {
		n.SchedulerMessage = "won't schedule"
		return nil
	}

	// TODO: there might be a node type that does not create a process when it is started,
	// so maybe we should move this to findRunnableRun ? but then it won't be optimized that much
	if n.MaxProc >= 0 && n.NProc >= n.MaxProc {
		n.SchedulerMessage = "max proc reached"
		return nil
	}

	if n.Blocked {
		n.SchedulerMessage = "blocked"
		return nil
	}

	n.SchedulerMessage = ""

	switch n.Type {
	case TypeSeq:
		return t.findRunnableSeq(n)
	case TypePar:
		return t.findRunnablePar(n)
	case TypeRun:
		return t.findRunnableRun(n)
	}
	return nil
}

// findRunnableSeq returns the first sub-node of a seq node that is runnable
func (t *Tree) findRunnableSeq(n *Node) *Node {
	for _, c := range n.Nodes {
		// Even if c is active, it may contain par nodes that can have runnable items
		// so we look for runnable items inside...
		sn := t.findRunnable(c)
		if sn != nil {
			return sn
		}
		// but we do not go to the next item unless this one is finished
		if !StatusFinished(c.Status) {
			return nil
		}
	}
	return nil
}

// findRunnablePar returns the first sub-node of a par node that is runnable
func (t *Tree) findRunnablePar(n *Node) *Node {
	for _, c := range n.Nodes {
		// we go over all items, and try to find any runnable
		// please note that c might already be active, but it may contain other par nodes
		// that are runnable
		sn := t.findRunnable(c)
		if sn != nil {
			return sn
		}
	}
	return nil
}

// findRunnableRun returns the node if it can be started by the scheduler
func (t *Tree) findRunnableRun(n *Node) *Node {
	if n.Status == StatusWaiting {
		return n
	}
	return nil
}
