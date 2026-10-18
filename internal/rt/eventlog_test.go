package rt

import (
	"bytes"
	"fmt"
	"log/slog"
	"net"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/nagylzs/runtree/internal/runner"
)

// entryText formats an entry for comparing: "node event level message", the node is "-" for the tree
func entryText(e LogEntry) string {
	id := "-"
	if e.Node != nil {
		id = e.Node.Id
	}
	return fmt.Sprintf("%s %s %s %s", id, e.Event, e.Level, e.Message)
}

// eventTexts returns the entries of the tree after seq, with entryText
func eventTexts(tree *Tree, seq uint64) []string {
	var result []string
	for _, e := range tree.EventLog().Since(seq) {
		result = append(result, entryText(e))
	}
	return result
}

// lastSeq is the sequence number of the last entry of the tree (0 if there is none)
func lastSeq(tree *Tree) uint64 {
	entries := tree.EventLog().Since(0)
	if len(entries) == 0 {
		return 0
	}
	return entries[len(entries)-1].Seq
}

func checkEvents(t *testing.T, tree *Tree, seq uint64, want ...string) {
	t.Helper()
	if got := eventTexts(tree, seq); !slices.Equal(got, want) {
		t.Errorf("the events are\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

func attrValue(e LogEntry, key string) (string, bool) {
	for _, a := range e.Attrs {
		if a.Key == key {
			return a.Value.String(), true
		}
	}
	return "", false
}

func TestEventHandler(t *testing.T) {
	tree := loadTestTree(t, safetyTree)
	var buf bytes.Buffer
	tree.SetLogHandler(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn}))
	job := tree.NodeById["job"]

	job.log().Info("hello", attrEvent, EventStart, "a", 1, slog.Group("g", "b", "x"))
	job.log().Debug("not kept")
	tree.logger.WithGroup("grp").With("c", true).Warn("tree level", "d", 2*time.Second)

	entries := tree.EventLog().Since(0)
	if len(entries) != 2 {
		t.Fatalf("%d entries, want 2: %+v", len(entries), entries)
	}
	e := entries[0]
	if e.Node != job || e.Event != EventStart || e.Level != slog.LevelInfo || e.Message != "hello" || e.Seq != 1 {
		t.Errorf("the first entry is %s (seq %d)", entryText(e), e.Seq)
	}
	for key, want := range map[string]string{"a": "1", "g.b": "x"} {
		if got, _ := attrValue(e, key); got != want {
			t.Errorf("%s is %q, want %q", key, got, want)
		}
	}
	if _, ok := attrValue(e, attrNode); ok {
		t.Errorf("the node is an attribute too: %+v", e.Attrs)
	}
	e = entries[1]
	if e.Node != nil || e.Event != "" || e.Level != slog.LevelWarn {
		t.Errorf("the second entry is %s", entryText(e))
	}
	for key, want := range map[string]string{"grp.c": "true", "grp.d": "2s"} {
		if got, _ := attrValue(e, key); got != want {
			t.Errorf("%s is %q, want %q", key, got, want)
		}
	}
	// the next handler gets the records of its level, the node is logged with its id
	out := buf.String()
	if strings.Contains(out, "hello") || !strings.Contains(out, "tree level") || !strings.Contains(out, "grp.c=true") {
		t.Errorf("the next handler got\n%s", out)
	}
	buf.Reset()
	tree.SetLogHandler(slog.NewTextHandler(&buf, nil))
	job.log().Info("with node")
	if !strings.Contains(buf.String(), "node=job") {
		t.Errorf("the next handler got\n%s", buf.String())
	}
}

func TestEventLogSince(t *testing.T) {
	l := NewEventLog(10)
	for i := 1; i <= 25; i++ {
		l.add(LogEntry{Message: fmt.Sprint(i)})
	}
	all := l.Since(0)
	if len(all) == 0 || len(all) > 10 || all[len(all)-1].Seq != 25 || all[len(all)-1].Message != "25" {
		t.Fatalf("the entries are %+v", all)
	}
	for i := 1; i < len(all); i++ {
		if all[i].Seq != all[i-1].Seq+1 {
			t.Errorf("the entries are not continuous: %+v", all)
		}
	}
	if got := l.Since(23); len(got) != 2 || got[0].Seq != 24 {
		t.Errorf("since 23: %+v", got)
	}
	if got := l.Since(25); len(got) != 0 {
		t.Errorf("since 25: %+v", got)
	}
	if got := l.Since(1); len(got) != len(all) {
		t.Errorf("since 1 (it is gone already): %d entries, want %d", len(got), len(all))
	}
}

const eventTree = `
tree:
  id: root
  seq:
    - id: a
      args: ["true"]
      provides: [db]
      xlocks: [apt]
      rlocks: [net]
    - id: b
      args: [echo, "hello world"]
`

func TestOperationEvents(t *testing.T) {
	tree := loadTestTree(t, eventTree)
	a := tree.NodeById["a"]
	if _, err := a.PerformOp(OpFreeze, true, false, true, nil); err != nil {
		t.Fatal(err)
	}
	checkEvents(t, tree, 0,
		"a operation INFO operation: Freeze",
		"a status INFO Waiting → Frozen")

	seq := lastSeq(tree)
	if _, err := a.PerformOp(OpSuccess, true, false, true, nil); err == nil {
		t.Fatal("success is not allowed on a frozen node")
	}
	checkEvents(t, tree, seq) // not allowed, nothing happened

	// not manual (e.g. on_error of a sibling): only the status changes
	if _, err := a.PerformOp(OpSkip, true, false, false, nil); err != nil {
		t.Fatal(err)
	}
	checkEvents(t, tree, seq,
		"a status INFO Frozen → Skipped",
		"a provided INFO provided db") // like success

	seq = lastSeq(tree)
	if _, err := tree.Root.PerformOp(OpResetToWaiting, true, true, true, nil); err != nil {
		t.Fatal(err)
	}
	checkEvents(t, tree, seq,
		"root operation INFO operation: ResetToWaiting all",
		"a status INFO Skipped → Waiting",
		"root unprovided INFO unprovided db")
}

func TestProvideAndUnprovideEvents(t *testing.T) {
	tree := loadTestTree(t, eventTree)
	a := tree.NodeById["a"]
	a.WantManualStatus, a.ManualStatus = true, StatusSuccess
	a.updateStatusesUp(true)
	checkEvents(t, tree, 0,
		"a status INFO Waiting → Success",
		"a provided INFO provided db")
	seq := lastSeq(tree)
	if _, err := a.PerformOp(OpResetToWaiting, true, false, true, nil); err != nil {
		t.Fatal(err)
	}
	checkEvents(t, tree, seq,
		"a operation INFO operation: ResetToWaiting",
		"a status INFO Success → Waiting",
		"a unprovided INFO unprovided db")
}

func TestLockEvents(t *testing.T) {
	tree := loadTestTree(t, eventTree)
	a, b := tree.NodeById["a"], tree.NodeById["b"]
	a.acquireLocks(true)
	a.releaseLocks(true)
	b.acquireLocks(true) // no locks, no events
	checkEvents(t, tree, 0,
		"a locked INFO acquired xlocks apt; rlocks net",
		"a unlocked INFO released xlocks apt; rlocks net")
}

func TestOnErrorEvents(t *testing.T) {
	tree := loadTestTree(t, onErrorTree)
	step := tree.NodeById["step"]
	step.StartError = "failed in a test"
	step.updateStatusesUp(true)
	events := eventTexts(tree, 0)
	if len(events) < 3 || events[0] != "step status WARN Waiting → Failed" ||
		events[1] != "step on-error INFO on_error: freeze the siblings" {
		t.Fatalf("the events are\n%s", strings.Join(events, "\n"))
	}
	// the siblings are frozen, without operation events
	for _, want := range []string{"allowed status INFO Waiting → Frozen", "deep status INFO Waiting → Frozen"} {
		if !slices.Contains(events, want) {
			t.Errorf("%q is missing from the events:\n%s", want, strings.Join(events, "\n"))
		}
	}
	for _, e := range events {
		if strings.Contains(e, " operation ") {
			t.Errorf("an operation event: %s", e)
		}
	}
}

func TestStartFailedEvents(t *testing.T) {
	tree := loadTestTree(t, eventTree)
	tree.SetTerminalHelper(&fakeTerminalHelper{})
	// an address where nothing listens
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := l.Addr().String()
	_ = l.Close()

	b := tree.NodeById["b"]
	b.Calculated.Runner = addr
	tree.Lock("TestStartFailedEvents")
	err = b.Start(true)
	tree.UnLock()
	if err != nil {
		t.Fatal(err)
	}
	waitStarted(t, b)
	if b.StartError == "" {
		t.Fatal("the start did not fail")
	}
	entries := tree.EventLog().Since(0)
	var got []string
	for _, e := range entries {
		got = append(got, entryText(e))
	}
	want := []string{
		fmt.Sprintf(`b start INFO starting on %s: echo "hello world"`, addr),
		"b status INFO Waiting → Running", // while it connects to the runner
		"root status INFO Waiting → Running",
		"b start-failed ERROR failed to start: " + b.StartError,
		"b status WARN Running → Failed",
		"root status WARN Running → Failed",
	}
	if !slices.Equal(got, want) {
		t.Errorf("the events are\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	if runnerAttr, _ := attrValue(entries[0], "runner"); runnerAttr != addr {
		t.Errorf("the runner attribute is %q", runnerAttr)
	}
}

func TestExitMessages(t *testing.T) {
	for _, tc := range []struct {
		state runner.ProcState
		want  string
	}{
		{runner.ProcState{Pid: 10, Exited: true}, "INFO exited with code 0 after 2"},
		{runner.ProcState{Pid: 10, Exited: true, ExitCode: 3, Error: "exit status 3"}, "WARN exited with code 3 after 2"},
		{runner.ProcState{Pid: 10, ExitCode: -1, Error: "signal: killed"}, "WARN exited: signal: killed after 2"},
	} {
		tree := loadTestTree(t, eventTree)
		a := tree.NodeById["a"]
		a.Started = time.Now().Add(-2 * time.Second)
		a.logExit(tc.state)
		entries := tree.EventLog().Since(0)
		if len(entries) != 1 || entries[0].Event != EventExited {
			t.Fatalf("%+v: the events are %v", tc.state, eventTexts(tree, 0))
		}
		if got := entries[0].Level.String() + " " + entries[0].Message; !strings.HasPrefix(got, tc.want) {
			t.Errorf("%+v: the event is %q, want %q...", tc.state, got, tc.want)
		}
		if pid, _ := attrValue(entries[0], "pid"); pid != "10" {
			t.Errorf("the pid is %q", pid)
		}
	}
}

const exitTree = `
tree:
  id: root
  par:
    - id: ok
      args: ["true"]
    - id: fails
      args: [sh, -c, "sleep 0.2; exit 3"]
`

// TestExitEvents runs processes: the exit is logged once, before the status change (also when the scheduler sees the
// exit first)
func TestExitEvents(t *testing.T) {
	tree := loadTestTree(t, exitTree)
	tree.SetTerminalHelper(&ptyTerminalHelper{})
	addr := startTestRunner(t)
	tree.Lock("TestExitEvents")
	for _, id := range []string{"ok", "fails"} {
		n := tree.NodeById[id]
		n.Calculated.Runner = addr
		if err := n.Start(true); err != nil {
			t.Fatal(err)
		}
	}
	tree.UnLock()
	// the scheduler updates the statuses periodically
	for deadline := time.Now().Add(5 * time.Second); ; time.Sleep(10 * time.Millisecond) {
		tree.Lock("TestExitEvents")
		tree.Root.updateStatusesDown(true)
		done := StatusFinished(tree.Root.Status)
		tree.UnLock()
		if done {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the nodes did not finish, the events are\n%s", strings.Join(eventTexts(tree, 0), "\n"))
		}
	}
	tree.RLock("TestExitEvents")
	events := eventTexts(tree, 0)
	tree.RUnlock()
	for _, want := range [][]string{
		{"ok exited INFO exited with code 0 after ", "ok status INFO Running → Success"},
		{"fails exited WARN exited with code 3 after ", "fails status WARN Running → Failed"},
	} {
		i := slices.IndexFunc(events, func(e string) bool { return strings.HasPrefix(e, want[0]) })
		if i < 0 || i+1 >= len(events) || events[i+1] != want[1] {
			t.Errorf("%q is not followed by %q in the events\n%s", want[0], want[1], strings.Join(events, "\n"))
		}
		count := 0
		for _, e := range events {
			if strings.HasPrefix(e, strings.Fields(want[0])[0]+" exited ") {
				count++
			}
		}
		if count != 1 {
			t.Errorf("%d exit events of %s", count, strings.Fields(want[0])[0])
		}
	}
}

func TestSendSignalNotRunning(t *testing.T) {
	tree := loadTestTree(t, eventTree)
	sent, err := tree.NodeById["a"].SendSignal(true, "SIGINT")
	if sent || err != nil {
		t.Errorf("sent: %v, %v", sent, err)
	}
	checkEvents(t, tree, 0)
}

func TestCheckLock(t *testing.T) {
	tree := loadTestTree(t, eventTree)
	tree.Lock("a test")
	tree.CheckLock(time.Minute) // not for long yet
	tree.lckwho.Lock()
	tree.whoSince = time.Now().Add(-time.Hour)
	tree.lckwho.Unlock()
	tree.CheckLock(time.Minute)
	tree.CheckLock(time.Minute) // once for each time the lock is held
	tree.UnLock()
	tree.CheckLock(time.Minute)
	checkEvents(t, tree, 0, "-  WARN the tree is locked for a long time")
	if by, _ := attrValue(tree.EventLog().Since(0)[0], "by"); by != "a test" {
		t.Errorf("by is %q", by)
	}
}

func TestQuoteArgs(t *testing.T) {
	if got, want := quoteArgs([]string{"ssh", "a b", "", `x"y`, "-c"}), `ssh "a b" "" "x\"y" -c`; got != want {
		t.Errorf("got %s, want %s", got, want)
	}
}
