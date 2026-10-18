package rt

import (
	"slices"
	"strings"
	"testing"
)

// gateTree has a frozen par node (an approval gate): its sub-nodes inherit the frozen status
const gateTree = `
tree:
  id: root
  seq:
    - id: gate
      status: frozen
      par:
        - {id: a, args: ["true"]}
        - {id: b, args: ["true"]}
    - {id: after, args: ["true"]}
`

func op(s schedTree, id string, op NodeOp, recursive bool) {
	s.t.Helper()
	s.tree.Lock("op")
	_, err := s.tree.NodeById[id].PerformOp(op, true, recursive, true, nil)
	s.tree.UnLock()
	if err != nil {
		s.t.Fatalf("%s on %s: %v", opLabel(op, recursive), id, err)
	}
}

func (s schedTree) allowed(id string, recursive bool) []string {
	s.tree.Lock("allowed")
	defer s.tree.UnLock()
	var names []string
	for o := range s.tree.NodeById[id].AllowedManualOps(true, recursive) {
		names = append(names, OpName(o))
	}
	slices.Sort(names)
	return names
}

func (s schedTree) statuses(ids ...string) string {
	var st []string
	for _, id := range ids {
		st = append(st, s.status(id))
	}
	return strings.Join(st, " ")
}

func (s schedTree) expectStatuses(want string, ids ...string) {
	s.t.Helper()
	if got := s.statuses(ids...); got != want {
		s.t.Fatalf("the statuses of %s are %q, want %q", strings.Join(ids, ", "), got, want)
	}
}

// Skip on a frozen seq/par node skips it: the tree goes on after it
func TestSkipFrozenContainer(t *testing.T) {
	s := newSchedTree(t, gateTree)
	s.expect("")
	s.expectStatuses("Frozen Frozen Frozen Waiting", "gate", "a", "b", "root")
	if got := strings.Join(s.allowed("gate", false), " "); got != "ResetToWaiting Skip Thaw" {
		t.Errorf("the operations of the frozen gate are %s", got)
	}
	op(s, "gate", OpSkip, false)
	s.expectStatuses("Skipped Frozen Frozen", "gate", "a", "b")
	s.expect("after")
	s.run("after")
	s.finish(StatusSuccess, "after")
	s.expectStatuses("Success", "root")

	// reset: the gate is calculated from its sub-nodes again, they are still frozen
	op(s, "gate", OpResetToWaiting, false)
	s.expectStatuses("Frozen", "gate")
	if got := strings.Join(s.allowed("gate", false), " "); got != "Skip" {
		t.Errorf("the operations of a gate with frozen sub-nodes are %s, want Skip (and Thaw all)", got)
	}
}

// Skip all on a frozen seq/par node skips the node too, so its parent can finish
func TestSkipAllFrozenContainer(t *testing.T) {
	s := newSchedTree(t, gateTree)
	op(s, "gate", OpSkip, true)
	s.expectStatuses("Skipped Skipped Skipped", "gate", "a", "b")
	s.expect("after")
	s.run("after")
	s.finish(StatusSuccess, "after")
	s.expectStatuses("Success", "root")
}

// Skip on a waiting seq/par node skips its whole subtree
func TestSkipWaitingContainer(t *testing.T) {
	s := newSchedTree(t, strings.Replace(gateTree, "status: frozen", "title: gate", 1))
	s.expectStatuses("Waiting", "gate")
	op(s, "gate", OpSkip, false)
	s.expectStatuses("Skipped Waiting Waiting", "gate", "a", "b")
	s.expect("after")
	op(s, "gate", OpResetToWaiting, false)
	s.expect("a")
}

// Skip all on a running seq/par node skips the sub-nodes that are not started, the node goes on
func TestSkipAllRunningContainer(t *testing.T) {
	s := newSchedTree(t, strings.Replace(gateTree, "status: frozen", "title: gate", 1))
	s.expect("a")
	s.run("a")
	op(s, "gate", OpSkip, true)
	s.expectStatuses("Running Running Skipped", "gate", "a", "b")
	s.finish(StatusSuccess, "a")
	s.expectStatuses("Success", "gate")
	s.expect("after")
}

// Thaw on a frozen seq/par node: its sub-nodes that are not frozen are started
func TestThawFrozenContainer(t *testing.T) {
	s := newSchedTree(t, strings.Replace(gateTree, `{id: b, args: ["true"]}`,
		`{id: b, status: waiting, args: ["true"]}`, 1))
	s.expect("")
	seq := lastSeq(s.tree)
	op(s, "gate", OpThaw, false)
	s.expectStatuses("Waiting Frozen Waiting", "gate", "a", "b")
	if events := eventTexts(s.tree, seq); !slices.Contains(events, "gate status INFO Frozen → Waiting") {
		t.Errorf("the status change of the gate is not logged:\n%s", strings.Join(events, "\n"))
	}
	s.expect("b")
}

// A seq/par node whose sub-nodes are frozen is frozen too, until they are thawed
func TestContainerOfFrozenNodes(t *testing.T) {
	s := newSchedTree(t, `
tree:
  id: root
  par:
    - {id: first, on_error: [failed, freeze], args: ["false"]}
    - id: group
      seq:
        - {id: g1, args: ["true"]}
        - {id: g2, args: ["true"]}
    - id: partly
      seq:
        - {id: p1, args: ["true"]}
        - {id: p2, args: ["true"]}
`)
	op(s, "p1", OpSkip, false)
	s.expect("first")
	s.run("first")
	seq := lastSeq(s.tree)
	s.finish(StatusFailed, "first")
	s.expectStatuses("Failed Frozen Frozen Frozen", "first", "group", "g1", "g2")
	s.expectStatuses("Frozen Skipped Frozen", "partly", "p1", "p2")
	if events := eventTexts(s.tree, seq); !slices.Contains(events, "group status INFO Waiting → Frozen") {
		t.Errorf("the status change of the group is not logged:\n%s", strings.Join(events, "\n"))
	}
	s.expect("")
	if got := strings.Join(s.allowed("group", true), " "); !strings.Contains(got, "Thaw") {
		t.Errorf("the recursive operations of the group are %s, Thaw is missing", got)
	}
	op(s, "group", OpThaw, true)
	s.expectStatuses("Waiting Waiting Waiting", "group", "g1", "g2")
	s.expect("g1")
	op(s, "p2", OpThaw, false)
	s.expectStatuses("Waiting", "partly")
}
