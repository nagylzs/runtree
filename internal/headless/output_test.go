package headless

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nagylzs/runtree/internal/rt"
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
	c := &console{w: out, flushAfter: flush, writers: make(map[*rt.Node]*lineWriter)}
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

// With --output failed, the exit status of a failed node may arrive before the end of its output: its lines are
// written at the end of the output, together (the lines of other nodes do not get between them), only the end of a
// long output
func TestOutputFailedAtTheEnd(t *testing.T) {
	tree := loadTree(t, `
tree:
  par:
    - {id: long, title: long, args: ["true"]}
    - {id: next, title: next, args: ["true"]}
`)
	out := &syncBuffer{}
	c := newConsole(out, tree, OutputFailed)
	long, next := tree.NodeById["long"], tree.NodeById["next"]
	wl, wn := c.NodeOutput(long, 0), c.NodeOutput(next, 0)
	_, _ = wl.Write([]byte("0\r\n"))
	c.finished(long, true)
	if got := out.String(); got != "" {
		t.Fatalf("the output is written before its end: %q", got)
	}
	_, _ = wn.Write([]byte("next line\r\n"))
	c.finished(next, true)
	_ = wn.(io.Closer).Close()
	const last = 300000 // the lines are 0...last, more than maxHeld
	for i := 1; i <= last; i++ {
		_, _ = wl.Write(fmt.Appendf(nil, "%d\r\n", i))
	}
	_ = wl.(io.Closer).Close()
	if !c.wait(time.Second) {
		t.Error("the outputs are not closed")
	}

	first, size := last, len(strconv.Itoa(last)) // the first line that is kept: the end of the output is maxHeld
	for size+len(strconv.Itoa(first-1)) <= maxHeld {
		first--
		size += len(strconv.Itoa(first))
	}
	var want strings.Builder
	want.WriteString("next | next line\n")
	fmt.Fprintf(&want, "long | (%d earlier lines are not shown)\n", first)
	for i := first; i <= last; i++ {
		fmt.Fprintf(&want, "long | %d\n", i)
	}
	if got := out.String(); got != want.String() {
		i := 0
		for i < len(got) && i < len(want.String()) && got[i] == want.String()[i] {
			i++
		}
		t.Errorf("the output differs from byte %d:\n%.200s\nwant\n%.200s", i, got[i:], want.String()[i:])
	}
	if len(c.pending) != 0 {
		t.Errorf("%d outputs are pending", len(c.pending))
	}
}

// At the end of the run (finishAll), the lines of the failed nodes are written when their output did not end too (the
// later lines are written at once), and of the nodes that did not finish and failed; the other ones are dropped
func TestOutputFailedFinishAll(t *testing.T) {
	tree := loadTree(t, `
tree:
  par:
    - {id: a, title: a, args: ["true"]}
    - {id: b, title: b, args: ["true"]}
    - {id: c, title: c, args: ["true"]}
    - {id: d, title: d, args: ["true"]}
`)
	out := &syncBuffer{}
	con := newConsole(out, tree, OutputFailed)
	w := func(id string) io.Writer { return con.NodeOutput(tree.NodeById[id], 0) }
	wa, wb, wc, wd := w("a"), w("b"), w("c"), w("d")
	_, _ = wa.Write([]byte("a1\r\n")) // a failed, its output does not end
	con.finished(tree.NodeById["a"], true)
	_, _ = wa.Write([]byte("a2\r\n"))
	_, _ = wb.Write([]byte("b1\r\n")) // b did not finish, it failed at the end of the run
	_, _ = wc.Write([]byte("c1\r\n")) // the output of c ends, then c fails
	_ = wc.(io.Closer).Close()
	con.finished(tree.NodeById["c"], true)
	_, _ = wd.Write([]byte("d1\r\n")) // d succeeds
	con.finished(tree.NodeById["d"], false)
	_, _ = wd.Write([]byte("d2\r\n"))
	_ = wd.(io.Closer).Close()
	if got := out.String(); got != "c | c1\n" {
		t.Fatalf("the output before the end of the run is %q", got)
	}

	con.finishAll(func(n *rt.Node) bool { return n.Id != "d" })
	_, _ = wa.Write([]byte("a3\r\n"))
	got := out.String()
	for _, block := range []string{"c | c1\n", "a | a1\na | a2\n", "b | b1\n"} {
		if !strings.Contains(got, block) {
			t.Errorf("the output does not have %q: %q", block, got)
		}
	}
	if !strings.HasSuffix(got, "a | a3\n") || len(got) != len("c | c1\na | a1\na | a2\nb | b1\na | a3\n") {
		t.Errorf("the output is %q", got)
	}
	if len(con.pending) != 0 {
		t.Errorf("%d outputs are pending", len(con.pending))
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
