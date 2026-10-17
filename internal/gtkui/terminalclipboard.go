package gtkui

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/diamondburned/gotk4/pkg/gio/v2"
	"github.com/diamondburned/gotk4/pkg/glib/v2"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
	vte "github.com/nagylzs/gotk4-vte"
)

// Max. number of lines shown in the paste confirmation dialog
const pastePreviewLines = 200

// setupTerminalClipboard adds copy/paste/select all actions to the terminal, with keyboard shortcuts and a
// context menu. Selecting text with the mouse and pasting with the middle mouse button is handled by vte itself,
// so pasting with the middle mouse button is not confirmed.
func setupTerminalClipboard(term *vte.Terminal) {
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
		pasteClipboard(term)
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
}

// pasteClipboard pastes the text from the clipboard into the terminal. Text with line breaks must be confirmed,
// because pasting it into a shell may run commands, immediately or when the running command finishes.
func pasteClipboard(term *vte.Terminal) {
	clipboard := term.Clipboard()
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
			showPasteConfirmDialog(term, text)
		} else {
			pasteText(term, text)
		}
	})
}

// pasteText pastes the text into the terminal, as if it was pasted from the clipboard (e.g. bracketed paste
// mode is applied).
func pasteText(term *vte.Terminal, text string) {
	if !term.PasteText(text) {
		term.PasteClipboard() // vte older than 0.68
	}
}

// showPasteConfirmDialog shows the text with line breaks, and pastes it into the terminal when confirmed.
// It must be called from the main gtk loop.
func showPasteConfirmDialog(term *vte.Terminal, text string) {
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
		pasteText(term, text)
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
