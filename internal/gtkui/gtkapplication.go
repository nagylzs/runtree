package gtkui

import (
	_ "embed"
	"log/slog"

	"github.com/diamondburned/gotk4/pkg/gio/v2"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
	"github.com/diamondburned/gotkit/gtkutil/cssutil"
	"github.com/nagylzs/runtree/internal/rt"
)

// This code below connects the parsed RunTree, the RunTreeApp with Gtk Application

//go:embed main.css
var mainCss string

// InitApplication creates the application for the tree. The warnings of the tree files are shown in the main window.
func InitApplication(runTree *rt.Tree, warnings rt.ParseErrors) error {
	var err error
	RunTree = runTree
	Application, err = NewRunTreeApp(runTree)
	if err == nil {
		Application.warnings = warnings
	}
	return err
}

func RunApplication() int {
	var err error
	if mainQueue, err = newMainLoopQueue(); err != nil {
		slog.Error("cannot create the queue of the main loop", "error", err)
		return 1
	}
	mainQueue.watch()
	// every runtree process runs its own tree: without NonUnique, a second runtree would only open another window of
	// the first one (with the first tree) and exit
	GtkApplication = gtk.NewApplication(ApplicationId, gio.ApplicationNonUnique)
	GtkApplication.ConnectActivate(activateApplication)
	go handleExitSignals()
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
	logSlowFrames(window)
	// Nasty!
	Application.window = window
	window.ConnectCloseRequest(Application.onCloseRequest)

	overlay := gtk.NewOverlay()
	pane := NewMainPaned()
	overlay.SetChild(pane)
	overlay.SetVExpand(true)
	content := gtk.NewBox(gtk.OrientationVertical, 0)
	if len(Application.warnings) > 0 {
		content.Append(newWarningsBanner(Application.warnings))
	}
	content.Append(overlay)
	window.SetChild(content)

	Application.pane = pane
	Application.overlay = overlay
	Application.lblSelectANode = gtk.NewLabel("Please select a node")

	Application.Start()
}
