// Package logging sets up the default slog logger of the programs.
package logging

import (
	"context"
	"errors"
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

// Multi returns a handler that sends the records to all the handlers (that are enabled for their level)
func Multi(handlers ...slog.Handler) slog.Handler {
	return multiHandler(handlers)
}

type multiHandler []slog.Handler

func (m multiHandler) Enabled(ctx context.Context, level slog.Level) bool {
	for _, h := range m {
		if h.Enabled(ctx, level) {
			return true
		}
	}
	return false
}

func (m multiHandler) Handle(ctx context.Context, r slog.Record) error {
	var errs []error
	for _, h := range m {
		if h.Enabled(ctx, r.Level) {
			if err := h.Handle(ctx, r.Clone()); err != nil {
				errs = append(errs, err)
			}
		}
	}
	return errors.Join(errs...)
}

func (m multiHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	result := make(multiHandler, len(m))
	for i, h := range m {
		result[i] = h.WithAttrs(attrs)
	}
	return result
}

func (m multiHandler) WithGroup(name string) slog.Handler {
	result := make(multiHandler, len(m))
	for i, h := range m {
		result[i] = h.WithGroup(name)
	}
	return result
}
