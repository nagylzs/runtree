package gtkui

import (
	"log/slog"
	"time"

	"github.com/diamondburned/gotk4/pkg/gdk/v4"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
)

// slowFrame is the time of a frame of the window that is logged (with -d), slowUpdate the time of an update of the
// GUI in Go (e.g. of the nodes of the navigator)
const (
	slowFrame  = 100 * time.Millisecond
	slowUpdate = 50 * time.Millisecond
)

// logSlow logs a slow update of the GUI (with -d) that began at began
func logSlow(msg string, began time.Time, args ...any) {
	if elapsed := time.Since(began); elapsed >= slowUpdate {
		slog.Debug(msg, append([]any{"elapsed", elapsed}, args...)...)
	}
}

// logSlowFrames logs the frames of the window that take long, with the time of the layout (CSS, size allocation)
// and of the paint (snapshot, rendering), to find the slow parts of the GUI. The handlers run after the ones of GTK,
// so a time is the end of that phase.
func logSlowFrames(window *gtk.ApplicationWindow) {
	setup := func() {
		fc := gdk.BaseFrameClock(window.FrameClock())
		var before, layout, paint time.Time
		fc.ConnectBeforePaint(func() { before = time.Now() })
		fc.ConnectLayout(func() { layout = time.Now() })
		fc.ConnectPaint(func() { paint = time.Now() })
		fc.ConnectAfterPaint(func() {
			if total := time.Since(before); total >= slowFrame && !layout.Before(before) && !paint.Before(layout) {
				slog.Debug("slow frame", "total", total, "layout", layout.Sub(before), "paint", time.Since(layout))
			}
		})
	}
	if window.Realized() {
		setup()
	} else {
		window.ConnectRealize(setup)
	}
}
