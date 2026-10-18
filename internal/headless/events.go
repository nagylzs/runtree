package headless

import (
	"context"
	"io"
	"log/slog"
	"strconv"
	"strings"
	"sync"

	"github.com/nagylzs/runtree/internal/rt"
)

// textHandler writes the log records of the tree for humans, one line each: the time, the level (when it is not
// Info), the label of the node, and the message, e.g. "12:00:01 web01 apt update: Running → Success". The message of
// an event is complete (see rt.EventStatus...), the other records have their attributes after the message.
type textHandler struct {
	mu    *sync.Mutex
	w     io.Writer
	level slog.Level
	node  *rt.Node
	attrs []slog.Attr // of WithAttrs, without the node
	group string      // the prefix of the attribute keys, from WithGroup ("a.b.")
}

func newTextHandler(w io.Writer, level slog.Level) *textHandler {
	return &textHandler{mu: &sync.Mutex{}, w: w, level: level}
}

func (h *textHandler) Enabled(_ context.Context, level slog.Level) bool {
	return level >= h.level
}

func (h *textHandler) Handle(_ context.Context, r slog.Record) error {
	node, event := h.node, false
	attrs := h.attrs
	r.Attrs(func(a slog.Attr) bool {
		switch {
		case h.group == "" && a.Key == "node":
			if n, ok := a.Value.Any().(*rt.Node); ok {
				node = n
				return true
			}
		case h.group == "" && a.Key == "event":
			event = true
			return true
		}
		attrs = append(attrs, slog.Attr{Key: h.group + a.Key, Value: a.Value})
		return true
	})
	var b strings.Builder
	if !r.Time.IsZero() {
		b.WriteString(r.Time.Format("15:04:05 "))
	}
	if r.Level != slog.LevelInfo {
		b.WriteString(r.Level.String() + " ")
	}
	if node != nil {
		b.WriteString(label(node) + ": ")
	}
	b.WriteString(r.Message)
	if !event {
		for _, a := range attrs {
			writeAttr(&b, "", a)
		}
	}
	b.WriteByte('\n')
	h.mu.Lock()
	defer h.mu.Unlock()
	_, err := io.WriteString(h.w, b.String())
	return err
}

// writeAttr writes " key=value", the keys of a group are prefixed with its name
func writeAttr(b *strings.Builder, prefix string, a slog.Attr) {
	v := a.Value.Resolve()
	if v.Kind() == slog.KindGroup {
		if a.Key != "" {
			prefix += a.Key + "."
		}
		for _, ga := range v.Group() {
			writeAttr(b, prefix, ga)
		}
		return
	}
	if a.Key == "" {
		return
	}
	s := v.String()
	if s == "" || strings.ContainsAny(s, " \t\n\"=") {
		s = strconv.Quote(s)
	}
	b.WriteString(" " + prefix + a.Key + "=" + s)
}

func (h *textHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	h2 := *h
	h2.attrs = append([]slog.Attr(nil), h.attrs...)
	for _, a := range attrs {
		if n, ok := a.Value.Any().(*rt.Node); ok && h.group == "" && a.Key == "node" {
			h2.node = n
			continue
		}
		h2.attrs = append(h2.attrs, slog.Attr{Key: h.group + a.Key, Value: a.Value})
	}
	return &h2
}

func (h *textHandler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	h2 := *h
	h2.group += name + "."
	return &h2
}
