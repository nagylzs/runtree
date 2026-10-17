package logging

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
)

func TestGlibInfoIsDebug(t *testing.T) {
	for _, tc := range []struct {
		level slog.Level
		want  []string // in the output
		not   []string
	}{
		{slog.LevelInfo, []string{"level=INFO msg=runtree"}, []string{"vulkan"}},
		{slog.LevelDebug, []string{"level=INFO msg=runtree", "level=DEBUG msg=vulkan glib_domain=Gdk"}, nil},
	} {
		var buf bytes.Buffer
		l := slog.New(glibHandler{slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: tc.level})})
		l.Info("runtree")
		l.Info("vulkan", "glib_domain", "Gdk")
		l.Warn("warning", "glib_domain", "Gtk") // only Info is lowered
		out := buf.String()
		for _, w := range append(tc.want, "level=WARN msg=warning glib_domain=Gtk") {
			if !strings.Contains(out, w) {
				t.Errorf("%s: %q is missing from\n%s", tc.level, w, out)
			}
		}
		for _, w := range tc.not {
			if strings.Contains(out, w) {
				t.Errorf("%s: %q is in\n%s", tc.level, w, out)
			}
		}
	}
}

func TestJSONFormat(t *testing.T) {
	var buf bytes.Buffer
	slog.New(NewHandler(&buf, slog.LevelInfo, "json")).Info("hello", "node", "n1")
	if out := buf.String(); !strings.HasPrefix(out, "{") || !strings.Contains(out, `"node":"n1"`) {
		t.Errorf("the output is %s", out)
	}
}
