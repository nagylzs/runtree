package gtkui

import (
	_ "embed"
	"log/slog"
	"os"
	"runtime"

	"github.com/diamondburned/gotk4/pkg/gio/v2"
	"github.com/diamondburned/gotk4/pkg/glib/v2"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
	"github.com/nagylzs/runtree/internal/rt"
	"github.com/nagylzs/runtree/internal/runner"
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
	setDefaultRenderer()
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
	// Quitting closes the main window, with the confirmation of the unfinished nodes. On macOS, GTK binds Cmd+Q and the
	// Quit item of the application menu to app.quit; with register-session, it also asks app.quit when macOS wants to
	// terminate runtree (the Dock, logging out) instead of letting it terminate at once.
	quit := gio.NewSimpleAction("quit", nil)
	quit.ConnectActivate(func(*glib.Variant) { closeMainWindow() })
	GtkApplication.AddAction(quit)
	if runtime.GOOS == "darwin" {
		GtkApplication.SetObjectProperty("register-session", true)
	}
	go handleExitSignals()
	loadSavedSettings()
	// TODO: how to pass gtk4 command line parameters here?
	return GtkApplication.Run([]string{})
}

// DefaultRenderer is the GTK renderer of runtree when GSK_RENDERER is not set. GTK's default (OpenGL) sends the whole
// window to the X server on every frame without a GPU (VNC, a remote X server: much slower), cairo only the changed
// parts (docs/getting-started.md).
const DefaultRenderer = "cairo"

// rendererFor is the renderer that runtree sets on a system: DefaultRenderer, but nothing on macOS. Its windows are
// not sent to an X server there, and the cairo renderer clips the tops of the characters there (and leaves parts of
// them when the text changes).
func rendererFor(goos string) string {
	if goos == "darwin" {
		return ""
	}
	return DefaultRenderer
}

// setDefaultRenderer sets GSK_RENDERER to the renderer of the system (rendererFor) when it is not set, before GTK
// reads it. The processes of the nodes do not get it: it is for runtree only.
func setDefaultRenderer() {
	if os.Getenv("GSK_RENDERER") != "" || rendererFor(runtime.GOOS) == "" {
		return
	}
	if err := os.Setenv("GSK_RENDERER", DefaultRenderer); err != nil {
		slog.Warn("could not set the GTK renderer", "error", err)
		return
	}
	runner.HideEnv("GSK_RENDERER")
	slog.Debug("GTK renderer: " + DefaultRenderer + " (GSK_RENDERER is not set)")
}

func activateApplication() {
	window := gtk.NewApplicationWindow(GtkApplication)
	window.SetTitle("RunTree")
	window.SetDefaultSize(1024, 768)
	window.Maximize()
	window.SetVisible(true)
	logSlowFrames(window)
	// Nasty!
	Application.window = window
	startAppearance(window)
	window.ConnectCloseRequest(Application.onCloseRequest)

	overlay := gtk.NewOverlay()
	pane := NewMainPaned()
	overlay.SetChild(pane)
	overlay.SetVExpand(true)
	content := gtk.NewBox(gtk.OrientationVertical, 0)
	if len(Application.warnings) > 0 {
		content.Append(newWarningsBanner(Application.warnings))
	}
	if exitOnFinish {
		finish := newFinishBanner()
		content.Append(finish)
		finish.start()
	}
	content.Append(overlay)
	window.SetChild(content)

	Application.pane = pane
	Application.overlay = overlay
	Application.lblSelectANode = gtk.NewLabel("Please select a node")

	Application.Start()
}
