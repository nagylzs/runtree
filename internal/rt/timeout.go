package rt

import (
	"fmt"
	"strings"
	"time"

	"github.com/nagylzs/runtree/internal/runner"
)

// timeoutGrace is the time that the process of a node that timed out gets after Ctrl+C, and after SIGTERM, see
// stopTimedOut
var timeoutGrace = []time.Duration{runner.TerminateGraceInt, runner.TerminateGraceTerm}

// DurationText is a duration as it is written in tree files, e.g. 90s, 30m or 1h30m (without the zero minutes and
// seconds of time.Duration.String)
func DurationText(d time.Duration) string {
	s := d.String()
	if strings.HasSuffix(s, "m0s") {
		s = strings.TrimSuffix(s, "0s")
	}
	if strings.HasSuffix(s, "h0m") {
		s = strings.TrimSuffix(s, "0m")
	}
	return s
}

// checkTimeout stops the process of a run node when the node runs longer than its timeout (since its start, like its
// elapsed time in the GUI). The node fails when the process exits (see procStatus). The scheduler calls it in every
// cycle, in locked state.
func (n *Node) checkTimeout() {
	if n.Type != TypeRun || n.Timeout <= 0 || n.TimedOut || n.Status != StatusRunning || n.Started.IsZero() {
		return
	}
	c := n.Client
	if c == nil || time.Since(n.Started) < n.Timeout {
		return // still connecting to its runner
	}
	if ps := c.LastState(); !ps.Alive() {
		return // it exited already
	}
	n.TimedOut = true
	n.log().Warn(fmt.Sprintf("timed out after %s, typing Ctrl+C", DurationText(n.Timeout)), attrEvent,
		EventTimeout, "timeout", n.Timeout)
	go n.stopTimedOut(c)
}

// stopTimedOut stops the process of a node that timed out, like a user would: it types Ctrl+C into its terminal
// (with ssh -t, it reaches the remote command too), then it sends SIGTERM and SIGKILL to its process group, after the
// grace periods, while the process of client c still runs. It is called in unlocked state.
func (n *Node) stopTimedOut(c *runner.Client) {
	defer n.Tree.recoverPanic("timeout of " + n.Id)
	if err := c.Type(runner.CtrlC); err != nil {
		n.log().Warn("could not type Ctrl+C: "+err.Error(), attrEvent, EventTimeout, "error", err)
	}
	running := func() bool {
		n.RLockTree("Node.stopTimedOut")
		defer n.RUnlockTree()
		ps := c.LastState()
		return n.Client == c && ps.Alive() // the node may be started again meanwhile
	}
	for i, sig := range []string{"SIGTERM", "SIGKILL"} {
		for end := time.Now().Add(timeoutGrace[i]); time.Now().Before(end); time.Sleep(100 * time.Millisecond) {
			if !running() {
				return
			}
		}
		n.LockTree("Node.stopTimedOut")
		if n.Client == c {
			_, _ = n.SendSignal(true, sig) // it is logged
		}
		n.UnLockTree()
	}
}
