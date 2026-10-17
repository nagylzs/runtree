package gtkui

import (
	"github.com/diamondburned/gotk4/pkg/gio/v2"
	"github.com/diamondburned/gotk4/pkg/glib/v2"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
	vte "github.com/nagylzs/gotk4-vte"
)

// setupTerminalClipboard adds copy/paste/select all actions to the terminal, with keyboard shortcuts and a
// context menu. Selecting text with the mouse and pasting with the middle mouse button is handled by vte itself.
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
		term.PasteClipboard()
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
