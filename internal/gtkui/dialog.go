package gtkui

import (
	"github.com/diamondburned/gotk4/pkg/gio/v2"
	"github.com/diamondburned/gotk4/pkg/glib/v2"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
)

// newDialogWindow creates a modal dialog window over the main window. Escape closes it.
// It must be called from the main gtk loop.
func newDialogWindow(title string) *gtk.Window {
	dialog := gtk.NewWindow()
	dialog.SetTitle(title)
	dialog.SetTransientFor(&Application.window.Window)
	dialog.SetModal(true)
	dialog.SetDestroyWithParent(true)
	dialog.SetResizable(false)

	shortcuts := gtk.NewShortcutController()
	shortcuts.SetPropagationPhase(gtk.PhaseCapture)
	shortcuts.AddShortcut(gtk.NewShortcut(
		gtk.NewShortcutTriggerParseString("Escape"),
		gtk.NewNamedAction("window.close"),
	))
	dialog.AddController(shortcuts)
	return dialog
}

// newDialogBox creates the vertical content box of a dialog
func newDialogBox() *gtk.Box {
	box := gtk.NewBox(gtk.OrientationVertical, 12)
	box.SetMarginTop(20)
	box.SetMarginBottom(20)
	box.SetMarginStart(20)
	box.SetMarginEnd(20)
	return box
}

// newDialogButtons creates the right aligned button row of a dialog
func newDialogButtons(buttons ...*gtk.Button) *gtk.Box {
	box := gtk.NewBox(gtk.OrientationHorizontal, 12)
	box.SetHAlign(gtk.AlignEnd)
	for _, b := range buttons {
		box.Append(b)
	}
	return box
}

// newWrappedLabel creates a left aligned label that wraps long text
func newWrappedLabel(text string) *gtk.Label {
	label := gtk.NewLabel(text)
	label.SetWrap(true)
	label.SetMaxWidthChars(50)
	label.SetXAlign(0)
	return label
}

// ShowError shows an error in a window of its own, when there is no main window (e.g. the tree cannot be run, because
// it is locked), and returns when the window is closed. It must not be called while the application runs.
func ShowError(title, text string) {
	setDefaultRenderer()
	errApp := gtk.NewApplication(ApplicationId, gio.ApplicationNonUnique)
	errApp.ConnectActivate(func() {
		window := gtk.NewApplicationWindow(errApp)
		window.SetTitle("RunTree: " + title)
		window.SetResizable(false)
		shortcuts := gtk.NewShortcutController()
		shortcuts.AddShortcut(gtk.NewShortcut(gtk.NewShortcutTriggerParseString("Escape"),
			gtk.NewNamedAction("window.close")))
		window.AddController(shortcuts)

		heading := gtk.NewLabel("")
		heading.SetMarkup("<b>" + glib.MarkupEscapeText(title) + "</b>")
		heading.SetXAlign(0)
		message := newWrappedLabel(text)
		message.SetSelectable(true) // the path of the lock file, the holder
		message.SetMaxWidthChars(70)
		closeButton := gtk.NewButtonWithLabel("Close")
		closeButton.ConnectClicked(window.Close)

		box := newDialogBox()
		box.Append(heading)
		box.Append(message)
		box.Append(newDialogButtons(closeButton))
		window.SetChild(box)
		window.SetDefaultWidget(closeButton)
		window.SetFocus(closeButton) // not the label: it would select its text
		window.SetVisible(true)
	})
	errApp.Run(nil)
}
