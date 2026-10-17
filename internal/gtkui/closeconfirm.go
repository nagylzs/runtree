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
// SIGTERM is not handled, it still terminates runtree immediately.
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
func (ra *RunTreeApp) onCloseRequest() bool {
	if ra.closeConfirmed {
		return false
	}
	RunTree.RLock("onCloseRequest")
	counts := RunTree.UnfinishedRunCounts(true)
	RunTree.RUnlock()
	if len(counts) == 0 {
		return false
	}
	ra.showCloseConfirmDialog(counts)
	return true
}

// showCloseConfirmDialog asks the user to confirm closing the main window, and closes it when confirmed.
// To avoid closing by accident, the user must type in the result of a random addition.
// It must be called from the main gtk loop.
func (ra *RunTreeApp) showCloseConfirmDialog(counts map[rt.Status]int) {
	if ra.closeDialog != nil {
		ra.closeDialog.Present()
		return
	}

	dialog := gtk.NewWindow()
	dialog.SetTitle("Confirm exit")
	dialog.SetTransientFor(&ra.window.Window)
	dialog.SetModal(true)
	dialog.SetDestroyWithParent(true)
	dialog.SetResizable(false)

	question := gtk.NewLabel("There are unfinished nodes, are you sure that you want to terminate them?")
	question.SetWrap(true)
	question.SetMaxWidthChars(50)
	question.SetXAlign(0)

	// Unfinished run node counts, in status order
	grid := gtk.NewGrid()
	grid.SetColumnSpacing(12)
	grid.SetRowSpacing(4)
	grid.SetMarginStart(12)
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

	x, y := rand.IntN(90)+10, rand.IntN(90)+10
	answer := strconv.Itoa(x + y)
	challenge := gtk.NewLabel(fmt.Sprintf("To terminate them, answer this: how much is %d + %d?", x, y))
	challenge.SetWrap(true)
	challenge.SetMaxWidthChars(50)
	challenge.SetXAlign(0)
	entry := gtk.NewEntry()
	entry.SetInputPurpose(gtk.InputPurposeDigits)

	btnCancel := gtk.NewButtonWithLabel("Cancel")
	btnTerminate := gtk.NewButtonWithLabel("Terminate")
	btnTerminate.AddCSSClass("destructive-action")
	btnTerminate.SetSensitive(false)

	buttons := gtk.NewBox(gtk.OrientationHorizontal, 12)
	buttons.SetHAlign(gtk.AlignEnd)
	buttons.Append(btnCancel)
	buttons.Append(btnTerminate)

	box := gtk.NewBox(gtk.OrientationVertical, 12)
	box.SetMarginTop(20)
	box.SetMarginBottom(20)
	box.SetMarginStart(20)
	box.SetMarginEnd(20)
	box.Append(question)
	box.Append(grid)
	box.Append(challenge)
	box.Append(entry)
	box.Append(buttons)
	dialog.SetChild(box)

	// Escape closes the dialog, same as Cancel
	shortcuts := gtk.NewShortcutController()
	shortcuts.SetPropagationPhase(gtk.PhaseCapture)
	shortcuts.AddShortcut(gtk.NewShortcut(
		gtk.NewShortcutTriggerParseString("Escape"),
		gtk.NewNamedAction("window.close"),
	))
	dialog.AddController(shortcuts)

	answered := func() bool {
		return strings.TrimSpace(entry.Text()) == answer
	}
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

	// Cancel is the default, so that an accidental Enter does not terminate anything.
	dialog.SetDefaultWidget(btnCancel)
	ra.closeDialog = dialog
	dialog.Present()
	entry.GrabFocus()
}
