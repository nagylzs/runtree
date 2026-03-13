package gtkui

import (
	_ "embed"

	"github.com/diamondburned/gotk4/pkg/gio/v2"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
	"github.com/diamondburned/gotkit/gtkutil/cssutil"
	"github.com/nagylzs/runtree/internal/rt"
)

// This code below connects the parsed RunTree, the RunTreeApp with Gtk Application

//go:embed main.css
var mainCss string

func InitApplication(runTree *rt.Tree) error {
	var err error
	RunTree = runTree
	Application, err = NewRunTreeApp(runTree)
	return err
}

func RunApplication() int {
	GtkApplication = gtk.NewApplication(ApplicationId, gio.ApplicationFlagsNone)
	GtkApplication.ConnectActivate(activateApplication)
	cssutil.WriteCSS(mainCss)
	// TODO: how to pass gtk4 command line parameters here?
	return GtkApplication.Run([]string{})
}

func activateApplication() {
	cssutil.ApplyGlobalCSS()

	window := gtk.NewApplicationWindow(GtkApplication)
	window.SetTitle("RunTree")
	window.SetDefaultSize(1024, 768)
	window.Maximize()
	window.SetVisible(true)
	// Nasty!
	Application.window = window

	overlay := gtk.NewOverlay()
	pane := NewMainPaned()
	overlay.SetChild(pane)
	window.SetChild(overlay)

	Application.pane = pane
	Application.overlay = overlay
	Application.lblSelectANode = gtk.NewLabel("Please select a node")

	Application.Start()
}
