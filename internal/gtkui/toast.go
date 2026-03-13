package gtkui

import (
	"github.com/diamondburned/gotk4/pkg/core/glib"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
)

func ShowToast(message string) {
	label := gtk.NewLabel(message)
	label.SetMarginTop(20)
	label.SetMarginBottom(20)
	label.SetMarginStart(20)
	label.SetMarginEnd(20)
	label.SetHAlign(gtk.AlignCenter)
	label.SetVAlign(gtk.AlignEnd)
	label.AddCSSClass("toast")

	Application.overlay.AddOverlay(label)

	// Remove the toast after 3 seconds
	glib.TimeoutAdd(3000, func() bool {
		Application.overlay.RemoveOverlay(label)
		return false
	})
}
