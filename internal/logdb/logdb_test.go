package logdb

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func openTestDB(t *testing.T, path string) *DB {
	t.Helper()
	d, err := Open(path, RunInfo{TreeFile: "/trees/upgrade.yaml", Host: "box", User: "admin", Pid: 42, Version: "v1"})
	if err != nil {
		t.Fatal(err)
	}
	if err := d.AddNodes([]NodeInfo{
		{ID: "n1", Type: "Seq", Label: "upgrade", Path: "upgrade"},
		{ID: "n2", ParentID: "n1", Type: "Run", Label: "host7", Path: "upgrade › host7"},
		{ID: "n3", ParentID: "n1", Type: "Run", Label: "host8", Path: "upgrade › host8"},
	}); err != nil {
		t.Fatal(err)
	}
	return d
}

func openTestReader(t *testing.T, path string) *Reader {
	t.Helper()
	r, err := OpenReader(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = r.Close() })
	return r
}

func TestEventsAndOutput(t *testing.T) {
	path := filepath.Join(t.TempDir(), "log.db")
	d := openTestDB(t, path)
	logger := slog.New(d.Handler())
	logger.Info("scheduler started", "event", "scheduler-started", "nodes", 3)
	node := logger.With("node", "n2")
	node.Info("starting", "event", "start", "args", []string{"apt", "upgrade"}, "start", 1)
	node.Debug("not saved")
	node.With("event", "exited").WithGroup("g").Warn("exited with code 3", "elapsed", 1500*time.Millisecond,
		"error", errors.New("exit status 3"))
	out := d.Output("n2", 1)
	for _, s := range []string{"Reading ", "package lists...", "\r\n"} {
		_, _ = out.Write([]byte(s))
	}
	_, _ = d.Output("n2", 2).Write([]byte("again"))
	if err := d.Close(); err != nil {
		t.Fatal(err)
	}
	// written after Close: not saved, it must not block or panic
	_, _ = out.Write([]byte("late"))
	logger.Info("late")
	if err := d.Close(); err != nil {
		t.Errorf("the second Close: %v", err)
	}

	r := openTestReader(t, path)
	runs, err := r.Runs()
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 1 || runs[0].ID != d.RunID() || runs[0].Host != "box" || runs[0].Finished.IsZero() ||
		runs[0].Events != 3 || runs[0].Output != int64(len("Reading package lists...\r\nagain")) {
		t.Errorf("the runs are %+v", runs)
	}
	events, err := r.Events(d.RunID())
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, e := range events {
		got = append(got, fmt.Sprintf("%s|%s|%s|%s|%s|%s", e.Level, e.NodeID, e.Path, e.Event, e.Message, e.Attrs))
	}
	want := []string{
		`INFO|||scheduler-started|scheduler started|{"nodes":3}`,
		`INFO|n2|upgrade › host7|start|starting|{"args":["apt","upgrade"],"start":1}`,
		`WARN|n2|upgrade › host7|exited|exited with code 3|{"g.elapsed":1.5,"g.error":"exit status 3"}`,
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("the events are\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	var buf bytes.Buffer
	if err := r.Output(d.RunID(), "n2", 1, &buf); err != nil {
		t.Fatal(err)
	}
	if buf.String() != "Reading package lists...\r\n" {
		t.Errorf("the output is %q", buf.String())
	}
	if starts, err := r.Starts(d.RunID(), "n2"); err != nil || fmt.Sprint(starts) != "[1 2]" {
		t.Errorf("the starts are %v, %v", starts, err)
	}
}

func TestSeveralRuns(t *testing.T) {
	path := filepath.Join(t.TempDir(), "log.db")
	for i := 0; i < 2; i++ {
		d := openTestDB(t, path)
		slog.New(d.Handler()).Info(fmt.Sprintf("run %d", i+1))
		if err := d.Close(); err != nil {
			t.Fatal(err)
		}
	}
	r := openTestReader(t, path)
	last, err := r.LastRun()
	if err != nil || last != 2 {
		t.Fatalf("the last run is %d, %v", last, err)
	}
	events, err := r.Events(1)
	if err != nil || len(events) != 1 || events[0].Message != "run 1" {
		t.Errorf("the events of run 1 are %+v, %v", events, err)
	}
	if err := r.CheckRun(3); err == nil {
		t.Error("there is no run 3")
	}
}

func TestFindNodes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "log.db")
	d := openTestDB(t, path)
	if err := d.Close(); err != nil {
		t.Fatal(err)
	}
	r := openTestReader(t, path)
	for name, want := range map[string]string{
		"n3":              "n3",
		"host7":           "n2",
		"upgrade › host8": "n3",
		"host":            "n2 n3", // in the path
		"upgrade":         "n1",    // the label, not the paths that contain it
		"%":               "",
	} {
		nodes, err := r.FindNodes(d.RunID(), name)
		if err != nil {
			t.Fatal(err)
		}
		var ids []string
		for _, n := range nodes {
			ids = append(ids, n.ID)
		}
		if got := strings.Join(ids, " "); got != want {
			t.Errorf("%q: got %q, want %q", name, got, want)
		}
	}
}

// many writers at the same time, and output that is bigger than a merged chunk
func TestConcurrentWrites(t *testing.T) {
	path := filepath.Join(t.TempDir(), "log.db")
	d := openTestDB(t, path)
	logger := slog.New(d.Handler())
	line := bytes.Repeat([]byte("x"), 1000)
	var wg sync.WaitGroup
	for _, node := range []string{"n2", "n3"} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			out := d.Output(node, 1)
			for i := 0; i < 2000; i++ { // 2 MB
				_, _ = out.Write(line)
				if i%100 == 0 {
					logger.Info("progress", "node", node, "i", i)
				}
			}
		}()
	}
	wg.Wait()
	if err := d.Close(); err != nil {
		t.Fatal(err)
	}
	r := openTestReader(t, path)
	for _, node := range []string{"n2", "n3"} {
		var buf bytes.Buffer
		if err := r.Output(d.RunID(), node, 1, &buf); err != nil {
			t.Fatal(err)
		}
		if buf.Len() != 2000*len(line) {
			t.Errorf("%s: %d bytes of output", node, buf.Len())
		}
	}
	events, err := r.Events(d.RunID())
	if err != nil || len(events) != 40 {
		t.Errorf("%d events, %v", len(events), err)
	}
	for _, e := range events {
		var attrs map[string]any
		if err := json.Unmarshal([]byte(e.Attrs), &attrs); err != nil {
			t.Errorf("attrs %q: %v", e.Attrs, err)
		}
	}
}

func TestNotALogDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing.db")
	if _, err := OpenReader(path); err == nil {
		t.Error("the file does not exist")
	}
}

func TestShow(t *testing.T) {
	path := filepath.Join(t.TempDir(), "log.db")
	d := openTestDB(t, path)
	slog.New(d.Handler()).Info("starting", "node", "n2", "event", "start")
	_, _ = d.Output("n2", 1).Write([]byte("first\r\n"))
	_, _ = d.Output("n2", 2).Write([]byte("second\r\n"))
	if err := d.Close(); err != nil {
		t.Fatal(err)
	}
	r := openTestReader(t, path)

	var buf bytes.Buffer
	if err := PrintRuns(&buf, r); err != nil {
		t.Fatal(err)
	}
	if lines := strings.Split(strings.TrimSpace(buf.String()), "\n"); len(lines) != 2 ||
		!strings.HasPrefix(lines[0], "RUN") || !strings.Contains(lines[1], "box") ||
		!strings.HasSuffix(lines[1], "/trees/upgrade.yaml") {
		t.Errorf("the runs are\n%s", buf.String())
	}

	buf.Reset()
	if err := PrintEvents(&buf, r, 1); err != nil {
		t.Fatal(err)
	}
	if got := buf.String(); !strings.HasSuffix(got, " INFO  [n2] upgrade › host7: starting\n") {
		t.Errorf("the events are %q", got)
	}
	if err := PrintEvents(&buf, r, 7); err == nil {
		t.Error("there is no run 7")
	}

	node, err := FindNode(r, 1, "host7")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		start      int
		want       string
		wantStart  int
		wantStarts int
	}{
		{0, "second\r\n", 2, 2}, // the last start
		{1, "first\r\n", 1, 2},
	} {
		buf.Reset()
		start, starts, err := PrintOutput(&buf, r, 1, node, tc.start)
		if err != nil || buf.String() != tc.want || start != tc.wantStart || starts != tc.wantStarts {
			t.Errorf("start %d: %q, %d of %d, %v", tc.start, buf.String(), start, starts, err)
		}
	}
	if _, _, err := PrintOutput(&buf, r, 1, node, 3); err == nil {
		t.Error("there is no start 3")
	}
	if _, err := FindNode(r, 1, "host"); err == nil || !strings.Contains(err.Error(), "use the id") {
		t.Errorf("host matches 2 nodes: %v", err)
	}
	if n3, _ := FindNode(r, 1, "n3"); n3.ID == "n3" {
		if _, _, err := PrintOutput(&buf, r, 1, n3, 0); err == nil {
			t.Error("n3 has no output")
		}
	}
}

func TestFormatBytes(t *testing.T) {
	for n, want := range map[int64]string{0: "0 B", 1023: "1023 B", 1024: "1.0 KB", 12700: "12.4 KB",
		3 << 20: "3.0 MB", 5 << 30: "5.0 GB"} {
		if got := formatBytes(n); got != want {
			t.Errorf("%d: %s, want %s", n, got, want)
		}
	}
}
