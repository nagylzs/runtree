package gtkui

import (
	"context"
	"log/slog"
	"os"
	"sync"

	vte "github.com/nagylzs/gotk4-vte"
	"github.com/nagylzs/runtree/internal/rt"
	"github.com/nagylzs/runtree/internal/runner"
)

// GTK4 has no signal for widget size changes, so the size of the displayed terminal is checked periodically.
const terminalSizeCheckMs = 250

// nodeTerminal is the terminal emulator that displays the remote pty of a started node
type nodeTerminal struct {
	term   *vte.Terminal
	client *runner.Client
	size   runner.WinSize // last size seen on the GUI; main gtk loop only

	lock   sync.Mutex     // guards wanted
	wanted runner.WinSize // the size that should be sent to the remote pty
	send   sync.Mutex     // serializes sending, guards sent
	sent   runner.WinSize // last size sent to the remote pty
}

// createTerminal creates the terminal emulator for a started node, and attaches it to the forwarded pty.
// It must be called from the main gtk loop.
func (ra *RunTreeApp) createTerminal(n *rt.Node, client *runner.Client, size runner.WinSize) error {
	term, err := vte.TerminalNew()
	if err != nil {
		return err
	}
	setupTerminalClipboard(term)

	nt := &nodeTerminal{term: term, client: client, size: size, wanted: size, sent: size}
	ra.lock.Lock()
	ra.terminals[n.Id] = nt
	ra.lock.Unlock()

	wd, _ := os.Getwd()
	cmd := []string{ra.bash, "-c", ra.socat + " -,raw,echo=0 UNIX-CONNECT:" + client.PtyPath}
	err = term.SpawnAsyncSimple(wd, cmd, os.Environ())
	if err != nil {
		return err
	}

	// The node may have been selected before its terminal was created
	if ra.Selected() == n {
		ra.displaySelectedNode(false)
	}
	return nil
}

// checkTerminalSize sends the size of the displayed terminal to its remote pty, when the number of columns or rows
// has changed. It is called periodically from the main gtk loop.
func (ra *RunTreeApp) checkTerminalSize() bool {
	nt := ra.currentTerm
	if nt == nil || !nt.term.Mapped() {
		return true
	}
	size := runner.WinSize{
		Cols:   nt.term.GetColumnCount(),
		Rows:   nt.term.GetRowCount(),
		Width:  uint16(nt.term.Width()),
		Height: uint16(nt.term.Height()),
	}
	if size.Cols == nt.size.Cols && size.Rows == nt.size.Rows {
		return true
	}
	nt.size = size

	// Save the last known size so that new terminals can start with this size.
	ra.lock.Lock()
	ra.lastWinSize = size
	ra.lock.Unlock()

	nt.sendSize(size)
	return true
}

// sendSize sends the size to the remote pty in the background, so that a slow network cannot block the main gtk
// loop. When sizes change quickly, the last one wins.
func (nt *nodeTerminal) sendSize(size runner.WinSize) {
	nt.lock.Lock()
	nt.wanted = size
	nt.lock.Unlock()

	go func() {
		nt.send.Lock()
		defer nt.send.Unlock()
		nt.lock.Lock()
		size := nt.wanted
		nt.lock.Unlock()
		if size == nt.sent {
			return
		}
		// If we cannot send the size then it is maybe because the process already exited, but the pty
		// was closed on the server before the "process state changed" message arrived to the client.
		// Since these happen on different computers in parallel, it is not clear how could we distinguish
		// between a real write error and a "write error after remote process exited". We just hope that
		// if there is a write error, then the process exits (soon), and the server will send the correct
		// exit code.
		ls := nt.client.LastState()
		if !ls.Alive() {
			return
		}
		err := nt.client.SendSize(context.TODO(), size, true)
		if err != nil {
			slog.Error("could not send terminal resize", "error", err)
			return
		}
		nt.sent = size
	}()
}
