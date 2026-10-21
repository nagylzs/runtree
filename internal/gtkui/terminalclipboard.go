package gtkui

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/diamondburned/gotk4/pkg/gdk/v4"
	"github.com/diamondburned/gotk4/pkg/gio/v2"
	"github.com/diamondburned/gotk4/pkg/glib/v2"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
	vte "github.com/nagylzs/gotk4-vte"
)

// Max. number of lines shown in the paste confirmation dialog
const pastePreviewLines = 200

// setupTerminalClipboard adds copy/paste/select all actions to the terminal, with keyboard shortcuts and a
// context menu, and pastes the primary selection with the middle mouse button, unless the process uses the mouse
// (mouseTracking, see runner.Client.MouseTracking). Selecting text with the mouse is handled by vte itself.
func setupTerminalClipboard(term *vte.Terminal, mouseTracking func() bool) {
	actions := gio.NewSimpleActionGroup()

	copyAction := gio.NewSimpleAction("copy", nil)
	copyAction.ConnectActivate(func(_ *glib.Variant) {
		term.CopyClipboardFormat(vte.FormatText)
	})
	copyAction.SetEnabled(false)
	term.ConnectSelectionChanged(func() {
		copyAction.SetEnabled(term.GetHasSelection())
	})
	actions.AddAction(copyAction)

	pasteAction := gio.NewSimpleAction("paste", nil)
	pasteAction.ConnectActivate(func(_ *glib.Variant) {
		pasteClipboard(term, false)
	})
	actions.AddAction(pasteAction)

	selectAllAction := gio.NewSimpleAction("select-all", nil)
	selectAllAction.ConnectActivate(func(_ *glib.Variant) {
		term.SelectAll()
	})
	actions.AddAction(selectAllAction)

	term.InsertActionGroup("term", actions)

	// The shortcuts always consume the key press, even when the action is disabled (e.g. copy without a
	// selection). Otherwise vte would send Ctrl+C to the process.
	shortcuts := gtk.NewShortcutController()
	shortcuts.SetPropagationPhase(gtk.PhaseCapture)
	addShortcut := func(trigger string, action string) {
		shortcuts.AddShortcut(gtk.NewShortcut(
			gtk.NewShortcutTriggerParseString(trigger),
			gtk.NewCallbackAction(func(_ gtk.Widgetter, _ *glib.Variant) bool {
				actions.ActivateAction(action, nil)
				return true
			}),
		))
	}
	addShortcut("<Control><Shift>c", "copy")
	addShortcut("<Control><Shift>v", "paste")
	term.AddController(shortcuts)

	menu := gio.NewMenu()
	addMenuItem := func(label string, action string, accel string) {
		item := gio.NewMenuItem(label, "term."+action)
		if accel != "" {
			item.SetAttributeValue("accel", glib.NewVariantString(accel))
		}
		menu.AppendItem(item)
	}
	addMenuItem("Copy", "copy", "<Control><Shift>c")
	addMenuItem("Paste", "paste", "<Control><Shift>v")
	addMenuItem("Select All", "select-all", "")
	term.SetContextMenuModel(menu)

	// The middle mouse button pastes the primary selection. vte would paste it without a confirmation, so the press is
	// claimed before vte gets it. A process that uses the mouse gets the click from vte, Shift+click pastes there.
	middle := gtk.NewGestureClick()
	middle.SetButton(gdk.BUTTON_MIDDLE)
	middle.SetPropagationPhase(gtk.PhaseCapture)
	middle.ConnectPressed(func(_ int, _, _ float64) {
		if mouseTracking() && middle.CurrentEventState()&gdk.ShiftMask == 0 {
			return
		}
		middle.SetState(gtk.EventSequenceClaimed)
		term.GrabFocus()
		pasteClipboard(term, true)
	})
	term.AddController(middle)
}

// pasteClipboard pastes the text from the clipboard (or from the primary selection) into the terminal. Text with line
// breaks must be confirmed, because pasting it into a shell may run commands, immediately or when the running command
// finishes.
func pasteClipboard(term *vte.Terminal, primary bool) {
	clipboard := term.Clipboard()
	if primary {
		clipboard = term.PrimaryClipboard()
	}
	clipboard.ReadTextAsync(context.Background(), func(res gio.AsyncResulter) {
		text, err := clipboard.ReadTextFinish(res)
		if err != nil {
			slog.Warn("could not read the clipboard", "error", err)
			return
		}
		if text == "" {
			return
		}
		if strings.ContainsAny(text, "\r\n") {
			showPasteConfirmDialog(term, text, primary)
		} else {
			pasteText(term, text, primary)
		}
	})
}

// pasteText pastes the text into the terminal, as if it was pasted from the clipboard (e.g. bracketed paste
// mode is applied).
func pasteText(term *vte.Terminal, text string, primary bool) {
	if term.PasteText(text) {
		return
	}
	// vte older than 0.68
	if primary {
		term.PastePrimary()
	} else {
		term.PasteClipboard()
	}
}

// showPasteConfirmDialog shows the text with line breaks, and pastes it into the terminal when confirmed.
// It must be called from the main gtk loop.
func showPasteConfirmDialog(term *vte.Terminal, text string, primary bool) {
	lines := strings.Split(strings.NewReplacer("\r\n", "\n", "\r", "\n").Replace(text), "\n")
	if lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1] // the text ends with a line break
	}
	preview := lines
	if len(preview) > pastePreviewLines {
		preview = preview[:pastePreviewLines]
	}
	previewText := strings.Join(preview, "\n")
	if len(lines) > len(preview) {
		previewText += fmt.Sprintf("\n\n... and %d more lines", len(lines)-len(preview))
	}

	dialog := newDialogWindow("Confirm paste")
	dialog.SetResizable(true)

	question := newWrappedLabel(fmt.Sprintf("The text to paste has %d line(s). Pasting it into a shell can run "+
		"commands, immediately or when the running command finishes. Are you sure that you want to paste it?",
		len(lines)))

	view := gtk.NewTextView()
	view.SetEditable(false)
	view.SetCursorVisible(false)
	view.SetMonospace(true)
	view.Buffer().SetText(previewText)
	scroll := gtk.NewScrolledWindow()
	scroll.SetPolicy(gtk.PolicyAutomatic, gtk.PolicyAutomatic)
	scroll.SetMinContentWidth(500)
	scroll.SetMinContentHeight(200)
	scroll.SetVExpand(true)
	scroll.SetChild(view)

	btnCancel := gtk.NewButtonWithLabel("Cancel")
	btnPaste := gtk.NewButtonWithLabel("Paste")
	btnPaste.AddCSSClass("destructive-action")

	box := newDialogBox()
	box.Append(question)
	box.Append(scroll)
	box.Append(newDialogButtons(btnCancel, btnPaste))
	dialog.SetChild(box)

	btnCancel.ConnectClicked(func() {
		dialog.Close()
	})
	btnPaste.ConnectClicked(func() {
		dialog.Close()
		pasteText(term, text, primary)
	})
	dialog.ConnectCloseRequest(func() bool {
		term.GrabFocus()
		return false
	})

	// Cancel is the default, so that an accidental Enter does not paste anything.
	dialog.SetDefaultWidget(btnCancel)
	dialog.Present()
	btnCancel.GrabFocus()
}
