package rt

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/nagylzs/runtree/internal/events"
)

func newUnattendedTree(t *testing.T, source string) schedTree {
	t.Helper()
	s := newSchedTree(t, source)
	s.tree.SetUnattended()
	return s
}

// end does what the scheduler does when it finds nothing to start, and tells how the run ended: the status of the
// root, or "stopped: " and the holds. It is "" when the run has not ended, or when there is a node to start.
func (s schedTree) end() string {
	s.t.Helper()
	s.tree.Lock("schedTree.end")
	defer s.tree.UnLock()
	s.update()
	if s.tree.findRunnable(s.tree.Root) != nil {
		return ""
	}
	s.tree.checkEnd()
	select {
	case <-s.tree.Ended():
	default:
		return ""
	}
	end := s.tree.End()
	if !end.Stopped {
		return StatusName(end.Status)
	}
	var holds []string
	for _, h := range end.Holds {
		holds = append(holds, h.Node.Id+": "+h.Reason)
	}
	return "stopped: " + strings.Join(holds, "; ")
}

func (s schedTree) expectEnd(want string) {
	s.t.Helper()
	if got := s.end(); got != want {
		s.t.Fatalf("the end is %q, want %q", got, want)
	}
}

// Nobody decides in an unattended tree: a node that would pause fails instead (the run node, and the seq/par nodes),
// and the event tells why
func TestUnattendedPausedFails(t *testing.T) {
	s := newUnattendedTree(t, `
tree:
  id: root
  par:
    - id: steps
      on_error: paused
      children_on_error: paused
      seq:
        - {id: a, args: ["false"]}
        - {id: b, args: ["true"]}
    - {id: c, args: ["true"]}
`)
	s.expect("a")
	s.run("a")
	s.expectEnd("")
	seq := lastSeq(s.tree)
	s.finish(StatusFailed, "a")
	s.expectStatuses("Failed Waiting Failed", "a", "b", "steps")
	events := eventTexts(s.tree, seq)
	for _, want := range []string{
		"a status WARN Running → Failed (on_error is paused, but nobody can decide: the tree runs unattended)",
		"steps status WARN Running → Failed (on_error is paused, but nobody can decide: the tree runs unattended)",
	} {
		if !slices.Contains(events, want) {
			t.Errorf("%q is not in the events:\n%s", want, strings.Join(events, "\n"))
		}
	}
	s.expect("c")
	s.run("c")
	s.finish(StatusSuccess, "c")
	s.expectEnd("Failed")
}

// The run ends when the root is finished, not while a process runs
func TestUnattendedFinished(t *testing.T) {
	s := newUnattendedTree(t, `
tree:
  id: root
  seq:
    - {id: a, on_error: success, args: ["false"]}
    - {id: b, args: ["true"]}
`)
	s.run("a")
	s.expectEnd("")
	s.finish(StatusFailed, "a")
	s.run("b")
	s.expectEnd("")
	s.finish(StatusSuccess, "b")
	s.expectEnd("Success")
}

// A frozen node (status: frozen) is never thawed in an unattended tree: the run stops when nothing else runs
func TestUnattendedFrozenGate(t *testing.T) {
	s := newUnattendedTree(t, `
tree:
  id: root
  par:
    - id: gate
      status: frozen
      seq: [{id: g1, args: ["true"]}]
    - {id: frozen_run, status: frozen, args: ["true"]}
    - {id: other, args: ["true"]}
`)
	s.expect("other")
	s.run("other")
	s.expectEnd("")
	s.finish(StatusSuccess, "other")
	s.expectEnd("stopped: gate: frozen (status: frozen); frozen_run: frozen (status: frozen)")
	s.expectStatuses("Running", "root")
}

// A rolling upgrade with [paused, freeze]: the failed host fails (nobody decides), the hosts that are not started are
// frozen, and the run stops after the running hosts finish
func TestUnattendedRollingUpgrade(t *testing.T) {
	s := newUnattendedTree(t, `
tree:
  id: root
  max_proc: 2
  children_on_error: [paused, freeze]
  par:
    - id: h1
      children_on_error: failed
      seq: [{id: h1a, args: ["true"]}]
    - id: h2
      children_on_error: failed
      seq: [{id: h2a, args: ["true"]}]
    - id: h3
      children_on_error: failed
      seq: [{id: h3a, args: ["true"]}]
`)
	s.run("h1a")
	s.run("h2a")
	s.finish(StatusFailed, "h1a")
	s.expectStatuses("Failed Running Frozen", "h1", "h2", "h3")
	s.expectEnd("")
	s.finish(StatusSuccess, "h2a")
	s.expectEnd("stopped: h3: frozen (by on_error)")
}

// A required resource that nothing provides, a lock that a running group holds while its next step is frozen, and
// max_proc: 0 stop the run
func TestUnattendedBlocked(t *testing.T) {
	s := newUnattendedTree(t, `
tree:
  id: root
  par:
    - {id: needs_db, requires: [db], args: ["true"]}
    - id: group
      xlocks: [disk]
      seq:
        - {id: first, args: ["true"]}
        - {id: second, status: frozen, args: ["true"]}
    - {id: wants_disk, xlocks: [disk], args: ["true"]}
    - id: none
      max_proc: 0
      seq: [{id: n1, args: ["true"]}]
`)
	s.expect("first")
	s.run("first")
	s.finish(StatusSuccess, "first")
	s.expectEnd("stopped: needs_db: requires db (nothing has provided it); second: frozen (status: frozen); " +
		"wants_disk: cannot lock disk (group holds it, it is Running); none: max_proc is 0")
}

// The run does not stop while the statuses are not up to date: the check updates them once more, and when something
// has changed, the next cycle decides
func TestUnattendedFixedPoint(t *testing.T) {
	s := newUnattendedTree(t, `
tree:
  id: root
  seq:
    - {id: a, status: frozen, args: ["true"]}
    - {id: b, args: ["true"]}
`)
	s.tree.Lock("TestUnattendedFixedPoint")
	s.update()
	// a is successful, but its status is not updated yet: the scheduler sees a frozen node
	a := s.tree.NodeById["a"]
	a.WantManualStatus, a.ManualStatus = true, StatusSuccess
	if n := s.tree.findRunnable(s.tree.Root); n != nil {
		t.Fatalf("%s is runnable", n.Id)
	}
	s.tree.checkEnd()
	s.tree.UnLock()
	select {
	case <-s.tree.Ended():
		t.Fatalf("the run ended: %v", s.tree.End())
	default:
	}
	s.expectStatuses("Success", "a")
	s.expect("b")
}

// An unattended tree with real processes: they have no terminals, their output goes to the recorder
func TestUnattendedRun(t *testing.T) {
	tree := loadTestTree(t, `
tree:
  id: root
  seq:
    - {id: hello, args: [sh, -c, "echo hello"]}
    - {id: fails, on_error: paused, args: [sh, -c, "echo failing; exit 3"]}
    - {id: never, args: ["true"]}
`)
	tree.awaker = events.NewDebouncer(tree.debouncedSchedule, true)
	tree.unblocker = events.NewDebouncer(tree.debouncedUnblock, false)
	tree.SetTerminalHelper(&fakeTerminalHelper{})
	recorder := &testRecorder{}
	tree.SetOutputRecorder(recorder)
	tree.SetUnattended()
	go tree.Run()
	t.Cleanup(tree.StopScheduler)
	select {
	case <-tree.Ended():
	case <-time.After(10 * time.Second):
		t.Fatal("the run did not end")
	}
	if end := tree.End(); end.Stopped || end.Status != StatusFailed {
		t.Errorf("the end is %+v", end)
	}
	recorder.check(t, "hello 1", "hello\r\n")
	recorder.check(t, "fails 1", "failing\r\n")
	tree.RLock("TestUnattendedRun")
	defer tree.RUnlock()
	if c := tree.NodeById["hello"].Client; c == nil || c.PtyPath != "" {
		t.Errorf("the client of hello has a terminal")
	}
	if n := tree.NodeById["never"]; n.Starts != 0 {
		t.Errorf("%s is started", n.Id)
	}
}
