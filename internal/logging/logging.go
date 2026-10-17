// Package logging sets up the default slog logger of the programs.
package logging

import (
	"context"
	"io"
	"log/slog"
	"os"

	"github.com/lmittmann/tint"
	"github.com/mattn/go-isatty"
)

// Setup makes the default logger write to stderr, at Warn level, Info with verbose, Debug with debug. format is
// "text" (colored on a terminal) or "json" (one object per line).
func Setup(verbose, debug bool, format string) {
	level := slog.LevelWarn
	if debug {
		level = slog.LevelDebug
	} else if verbose {
		level = slog.LevelInfo
	}
	slog.SetDefault(slog.New(glibHandler{NewHandler(os.Stderr, level, format)}))
}

// NewHandler returns a text or JSON handler for w.
func NewHandler(w io.Writer, level slog.Level, format string) slog.Handler {
	if format == "json" {
		return slog.NewJSONHandler(w, &slog.HandlerOptions{Level: level})
	}
	noColor := true
	if f, ok := w.(*os.File); ok {
		noColor = !isatty.IsTerminal(f.Fd())
	}
	return tint.NewHandler(w, &tint.Options{NoColor: noColor, Level: level})
}

// glibHandler logs the Info messages of GLib and GTK at Debug level: they are about GTK itself (e.g. the Vulkan
// loader), not about runtree. gotk4 logs them with a glib_domain attribute.
type glibHandler struct {
	slog.Handler
}

func (h glibHandler) Handle(ctx context.Context, r slog.Record) error {
	if r.Level == slog.LevelInfo && hasAttr(r, "glib_domain") {
		if !h.Handler.Enabled(ctx, slog.LevelDebug) {
			return nil
		}
		debug := slog.NewRecord(r.Time, slog.LevelDebug, r.Message, r.PC)
		r.Attrs(func(a slog.Attr) bool {
			debug.AddAttrs(a)
			return true
		})
		r = debug
	}
	return h.Handler.Handle(ctx, r)
}

func (h glibHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return glibHandler{h.Handler.WithAttrs(attrs)}
}

func (h glibHandler) WithGroup(name string) slog.Handler {
	return glibHandler{h.Handler.WithGroup(name)}
}

func hasAttr(r slog.Record, key string) bool {
	found := false
	r.Attrs(func(a slog.Attr) bool {
		found = a.Key == key
		return !found
	})
	return found
}
