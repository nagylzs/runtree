package rt

import "github.com/nagylzs/runtree/internal/runner"

// TerminalHelper connects remote ptys to local terminal emulators
// Please note that OnNodeChanged is only called when the status or the expanded state is changed.
// It is **not** called when provided, blocked and other props change.
type TerminalHelper interface {
	GetWinSize() runner.WinSize                                // Get actual window size for the terminal
	AfterPtyStarted(n *Node) error                             // Should connect the pty with the terminal/GUI here
	ProcStateChanged(n *Node, state runner.ProcState)          // Called after process state changed, should update GUI here
	OnNodeChanged(n *Node, oldStatus Status, oldExpanded bool) // Node status or expanded changed
	OnClientError(locked bool, node *Node, e runner.Error)
}
