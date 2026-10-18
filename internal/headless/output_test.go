package headless

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"
)

// syncBuffer is a bytes.Buffer for several goroutines
type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

func testWriter(out *syncBuffer, flush time.Duration) (*console, *lineWriter) {
	c := &console{w: out, flushAfter: flush}
	c.open.Add(1)
	return c, &lineWriter{c: c, prefix: "node | "}
}

// The lines of the terminal end with \r\n, a \r alone ends a line too (without empty lines), the colors are reset at
// the end of a line, and the line that has not ended is written when the output is closed
func TestLineWriter(t *testing.T) {
	for _, tc := range []struct {
		name   string
		writes []string
		want   string
	}{
		{"lines", []string{"one\r\ntwo\r\n"}, "node | one\nnode | two\n"},
		{"empty lines", []string{"\r\n\r\nthree\n\n"}, "node | \nnode | \nnode | three\nnode | \n"},
		{"split", []string{"on", "e\r", "\ntw", "o\r\n"}, "node | one\nnode | two\n"},
		{"progress", []string{"\r 10%\r 50%", "\r100%\r\n"}, "node |  10%\nnode |  50%\nnode | 100%\n"},
		{"colors", []string{"\x1b[32mgreen\r\n"}, "node | \x1b[32mgreen\x1b[0m\n"},
		{"no end", []string{"last line"}, "node | last line\n"},
		{"cr at the end", []string{"last\r"}, "node | last\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out := &syncBuffer{}
			c, w := testWriter(out, time.Hour)
			for _, s := range tc.writes {
				if n, err := w.Write([]byte(s)); n != len(s) || err != nil {
					t.Fatalf("Write: %d, %v", n, err)
				}
			}
			_ = w.Close()
			if got := out.String(); got != tc.want {
				t.Errorf("the output is %q, want %q", got, tc.want)
			}
			if !c.wait(time.Second) {
				t.Error("the output is not closed")
			}
		})
	}
}

// A line that does not end (e.g. a prompt) is written after a while, its end does not make an empty line
func TestLineWriterFlush(t *testing.T) {
	out := &syncBuffer{}
	_, w := testWriter(out, 50*time.Millisecond)
	_, _ = w.Write([]byte("first\r\nPassword: "))
	if got := out.String(); got != "node | first\n" {
		t.Fatalf("the output is %q", got)
	}
	for deadline := time.Now().Add(5 * time.Second); out.String() == "node | first\n"; time.Sleep(10 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("the prompt is not written")
		}
	}
	_, _ = w.Write([]byte("\r\nnext\r\nrest of a "))
	time.Sleep(100 * time.Millisecond)
	_, _ = w.Write([]byte("line\r\n"))
	_ = w.Close()
	_, _ = w.Write([]byte("after close\r\n"))
	want := "node | first\nnode | Password: \nnode | next\nnode | rest of a \nnode | line\n"
	if got := out.String(); got != want {
		t.Errorf("the output is %q, want %q", got, want)
	}
}

// The events are written with the label of the node, the other records with their attributes
func TestTextHandler(t *testing.T) {
	out := &syncBuffer{}
	tree := loadTree(t, `
tree:
  seq:
    - {id: build, title: "build {name}", vars: {name: app}, args: ["true"]}
`)
	logger := slog.New(newTextHandler(out, slog.LevelInfo))
	node := logger.With("node", tree.NodeById["build"])
	node.Info("Running → Success", "event", "status", "from", "Running", "to", "Success")
	node.Warn("could not connect", "error", "connection refused", "port", 22)
	logger.Info("scheduler started", "event", "scheduler-started", "nodes", 2)
	logger.WithGroup("g").Info("grouped", "key", "a value")
	logger.Debug("not written")
	var lines []string
	for _, l := range strings.Split(strings.TrimSuffix(out.String(), "\n"), "\n") {
		lines = append(lines, l[len("15:04:05 "):]) // without the time
	}
	want := []string{
		"build app: Running → Success",
		`WARN build app: could not connect error="connection refused" port=22`,
		"scheduler started",
		`grouped g.key="a value"`,
	}
	if got := strings.Join(lines, "\n"); got != strings.Join(want, "\n") {
		t.Errorf("the log is\n%s\nwant\n%s", got, strings.Join(want, "\n"))
	}
	if newTextHandler(out, slog.LevelInfo).Enabled(context.Background(), slog.LevelDebug) {
		t.Error("debug is enabled")
	}
}
