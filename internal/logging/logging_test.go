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
		l := slog.New(glibHandler(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: tc.level})))
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

func TestInfoAsDebug(t *testing.T) {
	for level, want := range map[slog.Level]string{
		slog.LevelInfo:  "level=WARN msg=terminating runner=local\n",
		slog.LevelDebug: "level=DEBUG msg=started runner=local pid=7\nlevel=WARN msg=terminating runner=local\n",
	} {
		var buf bytes.Buffer
		h := slog.NewTextHandler(&buf, &slog.HandlerOptions{
			Level: level,
			ReplaceAttr: func(_ []string, a slog.Attr) slog.Attr {
				if a.Key == slog.TimeKey {
					return slog.Attr{}
				}
				return a
			},
		})
		l := slog.New(InfoAsDebug(h)).With("runner", "local")
		l.Info("started", "pid", 7)
		l.Warn("terminating")
		if got := buf.String(); got != want {
			t.Errorf("%s: got\n%swant\n%s", level, got, want)
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

func TestMulti(t *testing.T) {
	var warn, info bytes.Buffer
	l := slog.New(Multi(
		slog.NewTextHandler(&warn, &slog.HandlerOptions{Level: slog.LevelWarn}),
		slog.NewTextHandler(&info, &slog.HandlerOptions{Level: slog.LevelInfo}),
	)).With("node", "n1").WithGroup("g")
	l.Info("info", "a", 1)
	l.Warn("warning")
	l.Debug("debug")
	if out := warn.String(); strings.Contains(out, "msg=info") || !strings.Contains(out, "msg=warning node=n1") {
		t.Errorf("the warning handler got\n%s", out)
	}
	if out := info.String(); !strings.Contains(out, "msg=info node=n1 g.a=1") || !strings.Contains(out, "msg=warning") ||
		strings.Contains(out, "debug") {
		t.Errorf("the info handler got\n%s", out)
	}
}
