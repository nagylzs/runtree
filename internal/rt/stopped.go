package rt

import (
	"fmt"
	"strings"
)

// Stopped tells why a running seq or par node does not go on: nothing runs below it, and the scheduler stops at
// paused or frozen nodes below it, that wait for an operation (e.g. a failed host of a rollout is paused, the hosts
// after it are frozen). It returns e.g. "stopped: 1 paused, 1 frozen", or "" when the node is not stopped: something
// runs below it, or something can be started, or something waits for a lock or a resource (it goes on without an
// operation). It must be called in locked state.
func (n *Node) Stopped(locked bool) string {
	if !locked {
		panic("must be called in locked state")
	}
	if n.Type == TypeRun || n.Status != StatusRunning || n.NProc > 0 {
		return ""
	}
	paused, frozen, other := 0, 0, 0
	// the nodes where the scheduler stops (see findRunnableHold)
	var visit func(p *Node)
	visit = func(p *Node) {
		for _, c := range p.Nodes {
			switch {
			case StatusFinished(c.Status):
			case c.Status == StatusPaused:
				paused++
			case c.Status == StatusFrozen:
				frozen++
			case c.Type == TypeRun || c.Blocked:
				other++ // it can be started, or it waits for a lock or a resource
			default:
				visit(c)
			}
			// a seq node does not go further than its first unfinished sub-node
			if p.Type == TypeSeq && !StatusFinished(c.Status) {
				return
			}
		}
	}
	visit(n)
	if other > 0 || paused+frozen == 0 {
		return ""
	}
	var holds []string
	if paused > 0 {
		holds = append(holds, fmt.Sprintf("%d paused", paused))
	}
	if frozen > 0 {
		holds = append(holds, fmt.Sprintf("%d frozen", frozen))
	}
	return "stopped: " + strings.Join(holds, ", ")
}
