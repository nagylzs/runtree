package rt

import (
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"
)

// schedTree drives the scheduler of a test tree without processes: a run node is made running by marking it as
// starting (see Node.Start), and it is finished by a manual status or a start error, so that the statuses change like
// in a real run (locks, provided resources and NProc follow the transitions).
type schedTree struct {
	t    *testing.T
	tree *Tree
}

func newSchedTree(t *testing.T, source string) schedTree {
	t.Helper()
	return schedTree{t, loadTestTree(t, source)}
}

// update recalculates the statuses and the blocked nodes, like a schedule cycle (it must be called locked)
func (s schedTree) update() {
	s.tree.Root.updateStatusesDown(true)
	s.tree.Root.updateBlockedAll(true)
}

// next returns the id of the node that the scheduler would start now, or ""
func (s schedTree) next() string {
	s.tree.Lock("schedTree.next")
	defer s.tree.UnLock()
	s.update()
	if n := s.tree.findRunnable(s.tree.Root); n != nil {
		return n.Id
	}
	return ""
}

// run makes the run nodes running
func (s schedTree) run(ids ...string) {
	s.tree.Lock("schedTree.run")
	defer s.tree.UnLock()
	for _, id := range ids {
		s.tree.NodeById[id].starting = true
	}
	s.update()
}

// finish finishes the running run nodes with success or failure
func (s schedTree) finish(st Status, ids ...string) {
	s.tree.Lock("schedTree.finish")
	defer s.tree.UnLock()
	for _, id := range ids {
		n := s.tree.NodeById[id]
		n.starting = false
		switch st {
		case StatusSuccess:
			n.WantManualStatus, n.ManualStatus = true, StatusSuccess
		case StatusFailed:
			n.StartError = "failed in a test"
		default:
			s.t.Fatalf("finish: %s", StatusName(st))
		}
	}
	s.update()
}

// expect checks the id of the node that the scheduler would start next (or "")
func (s schedTree) expect(want string) {
	s.t.Helper()
	if got := s.next(); got != want {
		s.t.Fatalf("the next node is %q, want %q", got, want)
	}
}

func (s schedTree) status(id string) string {
	s.tree.RLock("schedTree.status")
	defer s.tree.RUnlock()
	return StatusName(s.tree.NodeById[id].Status)
}

func (s schedTree) nproc(id string) int {
	s.tree.RLock("schedTree.nproc")
	defer s.tree.RUnlock()
	return s.tree.NodeById[id].NProc
}

// A seq node starts its children one after the other
func TestSchedulerSeq(t *testing.T) {
	s := newSchedTree(t, `
tree:
  id: root
  seq:
    - {id: a, args: ["true"]}
    - {id: b, args: ["true"]}
    - {id: c, args: ["true"]}
`)
	s.expect("a")
	s.run("a")
	s.expect("") // b waits for a
	s.finish(StatusSuccess, "a")
	s.expect("b")
	s.run("b")
	s.finish(StatusSuccess, "b")
	s.expect("c")
	s.run("c")
	s.finish(StatusSuccess, "c")
	s.expect("")
	if st := s.status("root"); st != "Success" {
		t.Errorf("root is %s", st)
	}
}

// A failed step stops the seq node (its status is failed, so nothing is started in it)
func TestSchedulerSeqStopsAtFailure(t *testing.T) {
	s := newSchedTree(t, `
tree:
  id: root
  seq:
    - {id: a, args: ["true"]}
    - {id: b, args: ["true"]}
`)
	s.run("a")
	s.finish(StatusFailed, "a")
	s.expect("")
	if st := s.status("root") + " " + s.status("b"); st != "Failed Waiting" {
		t.Errorf("the statuses are %s", st)
	}
}

// A par node starts all its children, in order, one per schedule cycle
func TestSchedulerPar(t *testing.T) {
	s := newSchedTree(t, `
tree:
  id: root
  par:
    - {id: a, args: ["true"]}
    - {id: b, args: ["true"]}
    - {id: c, args: ["true"]}
`)
	for _, id := range []string{"a", "b", "c"} {
		s.expect(id)
		s.run(id)
	}
	s.expect("")
	if n := s.nproc("root"); n != 3 {
		t.Errorf("%d processes", n)
	}
	s.finish(StatusSuccess, "b", "a", "c")
	if n := s.nproc("root"); n != 0 {
		t.Errorf("%d processes after they finished", n)
	}
	if st := s.status("root"); st != "Success" {
		t.Errorf("root is %s", st)
	}
}

// The scheduler looks into the running children for runnable nodes: a seq waits for its running par child, and a par
// starts the next node of each of its seq children
func TestSchedulerNested(t *testing.T) {
	s := newSchedTree(t, `
tree:
  id: root
  seq:
    - id: both
      par:
        - id: left
          seq:
            - {id: l1, args: ["true"]}
            - {id: l2, args: ["true"]}
        - id: right
          seq:
            - {id: r1, args: ["true"]}
            - {id: r2, args: ["true"]}
    - {id: last, args: ["true"]}
`)
	s.expect("l1")
	s.run("l1")
	s.expect("r1") // the par node goes on with its next child
	s.run("r1")
	s.expect("")
	s.finish(StatusSuccess, "r1")
	s.expect("r2")
	s.run("r2")
	s.finish(StatusSuccess, "r2")
	s.expect("") // last waits for the whole par node
	s.finish(StatusSuccess, "l1")
	s.expect("l2")
	s.run("l2")
	s.finish(StatusSuccess, "l2")
	s.expect("last")
}

// max_proc limits the running processes of a subtree
func TestSchedulerMaxProc(t *testing.T) {
	s := newSchedTree(t, `
tree:
  id: root
  seq:
    - id: limited
      max_proc: 2
      par:
        - {id: a, args: ["true"]}
        - {id: b, args: ["true"]}
        - id: inner
          par:
            - {id: c, args: ["true"]}
            - {id: d, args: ["true"]}
    - id: single
      max_proc: 1
      par:
        - {id: e, args: ["true"]}
        - {id: f, args: ["true"]}
`)
	s.expect("a")
	s.run("a")
	s.expect("b")
	s.run("b")
	s.expect("") // 2 processes run in limited
	s.tree.RLock("TestSchedulerMaxProc")
	msg := s.tree.NodeById["limited"].SchedulerMessage
	s.tree.RUnlock()
	if msg != "max proc reached" {
		t.Errorf("the scheduler message is %q", msg)
	}
	s.finish(StatusSuccess, "a")
	s.expect("c") // the limit counts the processes of the sub-nodes too
	s.run("c")
	s.expect("")
	s.finish(StatusSuccess, "b", "c")
	s.expect("d")
	s.run("d")
	s.finish(StatusSuccess, "d")
	s.expect("e")
	s.run("e")
	s.expect("")
	s.finish(StatusSuccess, "e")
	s.expect("f")
}

// Frozen nodes are not started: a seq node waits at them, a par node goes on with the others. Skipped nodes are
// finished, the subtree of a skipped node is not started.
func TestSchedulerFrozenAndSkipped(t *testing.T) {
	s := newSchedTree(t, `
tree:
  id: root
  par:
    - id: steps
      seq:
        - {id: cold, status: frozen, args: ["true"]}
        - {id: after_cold, args: ["true"]}
    - id: skipped
      seq:
        - {id: s1, args: ["true"]}
    - {id: free, args: ["true"]}
`)
	s.tree.Lock("TestSchedulerFrozenAndSkipped")
	_, err := s.tree.NodeById["skipped"].PerformOp(OpSkip, true, true, true, nil)
	s.tree.UnLock()
	if err != nil {
		t.Fatal(err)
	}
	s.expect("free")
	s.run("free")
	s.expect("") // after_cold waits for cold, s1 is skipped
	s.tree.Lock("TestSchedulerFrozenAndSkipped")
	_, err = s.tree.NodeById["cold"].PerformOp(OpThaw, true, false, true, nil)
	s.tree.UnLock()
	if err != nil {
		t.Fatal(err)
	}
	s.expect("cold")
}

// A node that requires a resource waits until a node provides it (it finishes successfully)
func TestSchedulerRequires(t *testing.T) {
	s := newSchedTree(t, `
tree:
  id: root
  par:
    - {id: deploy, requires: [binary], args: ["true"]}
    - {id: build, provides: [binary], args: ["true"]}
    - {id: failing, provides: [docs], args: ["true"]}
    - {id: publish, requires: [docs], args: ["true"]}
`)
	s.expect("build") // deploy is blocked
	s.tree.RLock("TestSchedulerRequires")
	deploy := s.tree.NodeById["deploy"]
	blocked, missing := deploy.Blocked, strings.Join(deploy.MissingRequired.List(), ",")
	s.tree.RUnlock()
	if !blocked || missing != "binary" {
		t.Errorf("deploy: blocked %v, missing %q", blocked, missing)
	}
	s.run("build")
	s.expect("failing")
	s.run("failing")
	s.expect("")
	s.finish(StatusFailed, "failing") // a failed node does not provide
	s.expect("")
	s.finish(StatusSuccess, "build")
	s.expect("deploy")
}

// Two nodes with the same exclusive lock do not run at the same time; a re-entrant lock is held by the topmost node
// that lists it, its subtree shares it, and it excludes the exclusive lock of other nodes
func TestSchedulerLocks(t *testing.T) {
	s := newSchedTree(t, `
tree:
  id: root
  par:
    - {id: x1, xlocks: [db], args: ["true"]}
    - {id: x2, xlocks: [db], args: ["true"]}
    - id: readers
      rlocks: [cache]
      par:
        - {id: r1, args: ["true"]}
        - {id: r2, args: ["true"]}
    - {id: writer, xlocks: [cache], args: ["true"]}
`)
	s.expect("x1")
	s.run("x1")
	s.expect("r1") // x2 is blocked by x1
	s.run("r1")    // readers becomes running, it holds cache
	s.expect("r2") // the same subtree
	s.run("r2")
	s.expect("") // writer is blocked by readers
	s.finish(StatusSuccess, "x1")
	s.expect("x2")
	s.run("x2")
	s.finish(StatusSuccess, "r1", "r2") // readers is finished, it releases cache
	s.expect("writer")
	s.tree.RLock("TestSchedulerLocks")
	holder := s.tree.xAcquiredBy["db"]
	s.tree.RUnlock()
	if holder == nil || holder.Id != "x2" {
		t.Errorf("db is held by %v", holder)
	}
}

// With real processes on the local runner: one node is started in a schedule cycle, max_proc is kept, and all of
// them finish
func TestSchedulerRunsProcesses(t *testing.T) {
	tree := loadTestTree(t, `
tree:
  id: root
  max_proc: 2
  par:
    - {id: a, args: [sleep, "0.3"]}
    - {id: b, args: [sleep, "0.3"]}
    - {id: c, args: [sleep, "0.3"]}
    - {id: d, args: [sleep, "0.3"]}
`)
	tree.SetTerminalHelper(&ptyTerminalHelper{})
	starts := func() int {
		tree.RLock("TestSchedulerRunsProcesses")
		defer tree.RUnlock()
		n := 0
		for _, id := range []string{"a", "b", "c", "d"} {
			n += tree.NodeById[id].Starts
		}
		return n
	}
	tree.schedule()
	if n := starts(); n != 1 {
		t.Errorf("%d nodes are started in the first cycle", n)
	}
	maxRunning := 0
	for deadline := time.Now().Add(10 * time.Second); ; time.Sleep(20 * time.Millisecond) {
		tree.schedule()
		tree.RLock("TestSchedulerRunsProcesses")
		running, done := 0, StatusFinished(tree.Root.Status)
		for _, id := range []string{"a", "b", "c", "d"} {
			if StatusActive(tree.NodeById[id].Status) {
				running++
			}
		}
		status, nproc := StatusName(tree.Root.Status), tree.Root.NProc
		tree.RUnlock()
		maxRunning = max(maxRunning, running)
		if running != nproc {
			t.Fatalf("%d nodes are running, NProc is %d", running, nproc)
		}
		if done {
			if status != "Success" || nproc != 0 {
				t.Errorf("the root is %s with %d processes", status, nproc)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the tree did not finish")
		}
	}
	if maxRunning != 2 {
		t.Errorf("at most %d nodes were running at the same time, want 2", maxRunning)
	}
}

// The sibling operation of on_error (freeze, skip) is performed on the siblings that are not started yet; the running
// ones go on (e.g. in a rolling upgrade, the hosts that are being upgraded finish their steps)
func TestOnErrorSiblingsNotStarted(t *testing.T) {
	s := newSchedTree(t, `
tree:
  id: root
  children_on_error: [failed, freeze]
  par:
    - id: h1
      seq:
        - {id: h1a, args: ["true"]}
        - {id: h1b, args: ["true"]}
    - id: h2
      seq:
        - {id: h2a, args: ["true"]}
        - {id: h2b, args: ["true"]}
    - id: h3
      seq:
        - {id: h3a, args: ["true"]}
        - {id: h3b, args: ["true"]}
`)
	s.expect("h1a")
	s.run("h1a")
	s.expect("h2a")
	s.run("h2a")
	s.finish(StatusFailed, "h1a") // h1 fails, h3 is frozen, h2 goes on
	if st := s.status("h1") + " " + s.status("h3a") + " " + s.status("h3b"); st != "Failed Frozen Frozen" {
		t.Errorf("the statuses are %s", st)
	}
	s.finish(StatusSuccess, "h2a")
	s.expect("h2b")
	s.run("h2b")
	s.finish(StatusSuccess, "h2b")
	if st := s.status("h2"); st != "Success" {
		t.Errorf("h2 is %s", st)
	}
}

// A node that includes one object becomes the object, but it keeps its own title, description, provides and
// requires (the object does not inherit them)
func TestIncludeKeepsOwnProperties(t *testing.T) {
	s := newSchedTree(t, `
tree:
  id: root
  par:
    - id: build
      title: "Build"
      provides: [binary]
      requires: [sources]
      include: compile
    - id: checkout
      provides: [sources]
      args: ["true"]
    - id: deploy
      requires: [binary]
      args: ["true"]
    - id: plain
      vars: {what: docs}
      include: compile

compile:
  title: "compile {what}"
  description: "it compiles"
  def_vars: {what: code}
  provides: [objects]
  args: ["true"]
`)
	s.tree.RLock("TestIncludeKeepsOwnProperties")
	build, plain := s.tree.NodeById["build"], s.tree.NodeById["plain"]
	provides := build.Calculated.Provides.List()
	slices.Sort(provides)
	got := fmt.Sprintf("%s, %s, %v, %v, %s %d | %s, %s %d", build.Calculated.Title, build.Calculated.Description,
		provides, build.Calculated.Requires.List(), TypeName(build.Type), len(build.Nodes), plain.Calculated.Title,
		TypeName(plain.Type), len(plain.Nodes))
	s.tree.RUnlock()
	if got != "Build, it compiles, [binary objects], [sources], Run 0 | compile docs, Run 0" {
		t.Errorf("got %s", got)
	}
	s.expect("checkout") // build waits for sources
	s.run("checkout")
	s.expect("plain") // deploy waits for binary
	s.run("plain")
	s.finish(StatusSuccess, "checkout")
	s.expect("build")
	s.run("build")
	s.finish(StatusSuccess, "build")
	s.expect("deploy")
}
