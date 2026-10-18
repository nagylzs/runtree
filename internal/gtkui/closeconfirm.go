package gtkui

import (
	"fmt"
	"log/slog"
	"math/rand/v2"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"

	"github.com/diamondburned/gotk4/pkg/core/glib"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
	"github.com/nagylzs/runtree/internal/rt"
)

// handleExitSignals handles SIGINT (Ctrl+C in the terminal that started runtree) and SIGHUP (that terminal was
// closed) the same way as closing the main window: when there are unfinished nodes, the user must confirm it.
// SIGTERM is handled by runtree's main (handleTerm): it exits without the confirmation, after terminating the processes.
func handleExitSignals() {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGINT, syscall.SIGHUP)
	for sig := range ch {
		slog.Warn("received signal, closing the main window", "signal", sig)
		glib.IdleAdd(func() {
			if Application.window == nil {
				// not activated yet, nothing was started
				GtkApplication.Quit()
				return
			}
			Application.window.Present()
			Application.window.Close()
		})
	}
}

// onCloseRequest is called from the main gtk loop when the user tries to close the main window.
// It returns true to stop the window from closing.
func (ra *RunTreeApp) onCloseRequest() (stop bool) {
	defer func() {
		if r := recover(); r != nil {
			reportPanic("close request", r)
			stop = true // never close without confirmation
		}
	}()
	if ra.closeConfirmed {
		return false
	}
	counts := unfinishedRunCounts()
	if len(counts) == 0 {
		return false
	}
	ra.showCloseConfirmDialog(counts)
	return true
}

// unfinishedRunCounts locks the tree and returns rt.Tree.UnfinishedRunCounts
func unfinishedRunCounts() map[rt.Status]int {
	RunTree.RLock("unfinishedRunCounts")
	defer RunTree.RUnlock()
	return RunTree.UnfinishedRunCounts(true)
}

// showCloseConfirmDialog asks the user to confirm closing the main window, and closes it when confirmed.
// To avoid closing by accident, the user must type in the result of a random addition.
// The counts are refreshed every second while the dialog is open.
// It must be called from the main gtk loop.
func (ra *RunTreeApp) showCloseConfirmDialog(counts map[rt.Status]int) {
	if ra.closeDialog != nil {
		ra.closeDialog.Present()
		return
	}

	dialog := newDialogWindow("Confirm exit")
	question := newWrappedLabel("")

	// Unfinished run node counts, in status order
	grid := gtk.NewGrid()
	grid.SetColumnSpacing(12)
	grid.SetRowSpacing(4)
	grid.SetMarginStart(12)

	x, y := rand.IntN(90)+10, rand.IntN(90)+10
	answer := strconv.Itoa(x + y)
	challenge := newWrappedLabel(fmt.Sprintf("To terminate them, answer this: how much is %d + %d?", x, y))
	entry := gtk.NewEntry()
	entry.SetInputPurpose(gtk.InputPurposeDigits)

	btnCancel := gtk.NewButtonWithLabel("Cancel")
	btnTerminate := gtk.NewButtonWithLabel("")

	box := newDialogBox()
	box.Append(question)
	box.Append(grid)
	box.Append(challenge)
	box.Append(entry)
	box.Append(newDialogButtons(btnCancel, btnTerminate))
	dialog.SetChild(box)

	finished := false // all nodes finished since the dialog was opened, closing is safe
	answered := func() bool {
		return finished || strings.TrimSpace(entry.Text()) == answer
	}
	update := func(counts map[rt.Status]int) {
		finished = len(counts) == 0
		for child := grid.FirstChild(); child != nil; child = grid.FirstChild() {
			grid.Remove(child)
		}
		row := 0
		for st := rt.StatusWaiting; st <= rt.StatusPaused; st++ {
			cnt, ok := counts[st]
			if !ok {
				continue
			}
			lblStatus := gtk.NewLabel(rt.StatusName(st))
			lblStatus.SetXAlign(0)
			lblStatus.AddCSSClass("status-" + strings.ToLower(rt.StatusName(st)))
			lblCount := gtk.NewLabel(strconv.Itoa(cnt))
			lblCount.SetXAlign(1)
			grid.Attach(lblStatus, 0, row, 1, 1)
			grid.Attach(lblCount, 1, row, 1, 1)
			row++
		}
		grid.SetVisible(!finished)
		challenge.SetVisible(!finished)
		entry.SetVisible(!finished)
		if finished {
			question.SetText("All nodes are finished, nothing will be terminated.")
			btnTerminate.SetLabel("Close application")
			btnTerminate.RemoveCSSClass("destructive-action")
		} else {
			question.SetText("There are unfinished nodes, are you sure that you want to terminate them?")
			btnTerminate.SetLabel("Terminate")
			btnTerminate.AddCSSClass("destructive-action")
		}
		btnTerminate.SetSensitive(answered())
	}
	update(counts)

	terminate := func() {
		if !answered() {
			return
		}
		ra.closeConfirmed = true
		dialog.Close()
		ra.window.Close()
	}

	dialog.ConnectCloseRequest(func() bool {
		ra.closeDialog = nil
		return false
	})
	entry.ConnectChanged(func() {
		btnTerminate.SetSensitive(answered())
	})
	entry.ConnectActivate(terminate)
	btnCancel.ConnectClicked(func() {
		dialog.Close()
	})
	btnTerminate.ConnectClicked(terminate)

	glib.TimeoutAdd(1000, func() bool {
		if ra.closeDialog != dialog {
			return false // the dialog was closed
		}
		update(unfinishedRunCounts())
		return true
	})

	// Cancel is the default, so that an accidental Enter does not terminate anything.
	dialog.SetDefaultWidget(btnCancel)
	ra.closeDialog = dialog
	dialog.Present()
	entry.GrabFocus()
}
