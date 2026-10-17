package rt

import (
	"io"

	"github.com/nagylzs/runtree/internal/runner"
)

// TerminalHelper connects remote ptys to local terminal emulators
// Please note that OnNodeChanged is only called when the status or the expanded state is changed.
// It is **not** called when provided, blocked and other props change.
// AfterPtyStarted is called with the tree locked, from the scheduler goroutine or from the GUI. It must not block,
// and it must not wait for the GUI thread (that may be waiting for the tree lock). When connecting the terminal
// fails later, the helper must call Node.TerminalFailed.
type TerminalHelper interface {
	GetWinSize() runner.WinSize                                // Get actual window size for the terminal
	AfterPtyStarted(n *Node)                                   // Should connect the pty with the terminal/GUI here
	ProcStateChanged(n *Node, state runner.ProcState)          // Called after process state changed, should update GUI here
	OnNodeChanged(n *Node, oldStatus Status, oldExpanded bool) // Node status or expanded changed
	OnClientError(locked bool, node *Node, e runner.Error)
	OnInternalError(message string) // A recovered panic, called from any goroutine, in unlocked state
}

// OutputRecorder gets a copy of the terminal output of the started nodes, e.g. to save it. NodeOutput is called when
// a node is started (with the tree locked), start is Node.Starts. It returns nil when the output is not needed. The
// writer is called by the goroutine that copies the output to the terminal, so it should not block for long.
type OutputRecorder interface {
	NodeOutput(n *Node, start int) io.Writer
}
