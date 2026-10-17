package gtkui

import (
	"fmt"

	"github.com/diamondburned/gotk4/pkg/gtk/v4"
	"github.com/nagylzs/runtree/internal/rt"
)

// newWarningsBanner creates the banner above the main pane that tells that the tree files have warnings, until it is
// closed. It must be called from the main gtk loop.
func newWarningsBanner(warnings rt.ParseErrors) *gtk.Box {
	text := "The tree files have 1 warning."
	if len(warnings) > 1 {
		text = fmt.Sprintf("The tree files have %d warnings.", len(warnings))
	}
	label := gtk.NewLabel(text)
	label.SetHExpand(true)
	label.SetXAlign(0)

	btnShow := gtk.NewButtonWithLabel("Show warnings")
	btnShow.ConnectClicked(func() {
		showWarningsDialog(warnings)
	})
	btnHide := gtk.NewButtonFromIconName("window-close-symbolic")
	btnHide.SetTooltipText("Hide this message")

	banner := gtk.NewBox(gtk.OrientationHorizontal, 12)
	banner.AddCSSClass("warnings-banner")
	banner.Append(label)
	banner.Append(btnShow)
	banner.Append(btnHide)
	btnHide.ConnectClicked(func() {
		banner.SetVisible(false)
	})
	return banner
}

// showWarningsDialog shows the warnings of the tree files, like runtree --check prints them.
// It must be called from the main gtk loop.
func showWarningsDialog(warnings rt.ParseErrors) {
	dialog := newDialogWindow("Warnings")
	dialog.SetResizable(true)
	dialog.SetDefaultSize(900, 500)

	view := gtk.NewTextView()
	view.SetEditable(false)
	view.SetCursorVisible(false)
	view.SetMonospace(true)
	view.SetLeftMargin(8)
	view.SetTopMargin(8)
	view.Buffer().SetText(warnings.Error())
	scroll := gtk.NewScrolledWindow()
	scroll.SetChild(view)
	scroll.SetHExpand(true)
	scroll.SetVExpand(true)

	btnCopy := gtk.NewButtonWithLabel("Copy")
	btnCopy.ConnectClicked(func() {
		view.Clipboard().SetText(warnings.Error())
		ShowToast("Copied to clipboard")
	})
	btnClose := gtk.NewButtonWithLabel("Close")
	btnClose.ConnectClicked(dialog.Close)

	box := newDialogBox()
	box.Append(newWrappedLabel("These warnings were found when the tree was loaded. They do not prevent running " +
		"the tree, but they usually point to mistakes (e.g. a variable that is not set). The tree files can be " +
		"checked with runtree --check."))
	box.Append(scroll)
	box.Append(newDialogButtons(btnCopy, btnClose))
	dialog.SetChild(box)
	dialog.Present()
}
