package rt

import (
	"strings"
	"testing"
)

const gatesTree = `
tree:
  id: root
  title: upgrade
  status: frozen          # a guard: nothing runs by accident
  seq:
    - {id: dev, title: dev upgrade, args: ["true"]}
    - id: prod
      title: prod
      status: frozen      # an approval gate inside it
      seq:
        - {id: prod1, title: prod upgrade, args: ["true"]}
    - id: after
      seq:
        - {id: after1, args: ["true"]}
        - {id: same, title: twice, args: ["true"]}
        - {id: same2, title: twice, args: ["true"]}
`

// --thaw thaws a gate (a node with its own status: frozen) and the sub-nodes that inherit it; a gate inside it stays
// frozen until it is thawed too
func TestThawGates(t *testing.T) {
	tree := loadTestTree(t, gatesTree)
	statuses := func() string {
		var s []string
		for _, id := range []string{"root", "dev", "prod", "prod1", "after", "after1"} {
			s = append(s, id+"="+StatusName(tree.NodeById[id].Status))
		}
		return strings.Join(s, " ")
	}
	tree.Lock("TestThawGates")
	defer tree.UnLock()
	if err := tree.ThawGates(true, []string{"upgrade"}); err != nil { // by title
		t.Fatal(err)
	}
	if got := statuses(); got != "root=Waiting dev=Waiting prod=Frozen prod1=Frozen after=Waiting after1=Waiting" {
		t.Errorf("after the root: %s", got)
	}
	if err := tree.ThawGates(true, []string{"upgrade › prod", "root"}); err != nil { // by path, and again by id
		t.Fatal(err)
	}
	if got := statuses(); got != "root=Waiting dev=Waiting prod=Waiting prod1=Waiting after=Waiting after1=Waiting" {
		t.Errorf("after prod: %s", got)
	}
	checkEventsInOrder(t, eventTexts(tree, 0), "root operation INFO thawed (--thaw)",
		"prod operation INFO thawed (--thaw)")
	// the scheduler starts the first one
	if n := tree.findRunnable(tree.Root); n == nil || n.Id != "dev" {
		t.Errorf("the first runnable node is %v", n)
	}
}

// --thaw of a node that is not a gate is an error, so that a typo does not leave a gate frozen
func TestThawGatesErrors(t *testing.T) {
	for name, want := range map[string]string{
		"nothing":      `no node is "nothing" (its id, title or path)`,
		"dev":          `upgrade › dev upgrade is frozen because upgrade is frozen (status: frozen): thaw that one`,
		"prod upgrade": `upgrade › prod › prod upgrade is frozen because upgrade › prod is frozen (status: frozen)`,
		"twice":        "2 nodes are \"twice\", use the id:\n  same: upgrade › after › twice\n  same2: ",
	} {
		tree := loadTestTree(t, gatesTree)
		tree.Lock("TestThawGatesErrors")
		err := tree.ThawGates(true, []string{name})
		tree.UnLock()
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("--thaw %s: the error is %v, want %q", name, err, want)
		}
	}
	tree := loadTestTree(t, "tree:\n  id: root\n  args: [\"true\"]\n")
	tree.Lock("TestThawGatesErrors")
	err := tree.ThawGates(true, []string{"root"})
	tree.UnLock()
	if err == nil || err.Error() != "root is not frozen in the tree file (status: frozen)" {
		t.Errorf("the error is %v", err)
	}
}

// Only the root node can have a lock
func TestLockProperty(t *testing.T) {
	tree := loadTestTree(t, "tree:\n  lock: upgrade.lock\n  seq:\n    - {args: [\"true\"]}\n")
	if tree.Root.Lock != "upgrade.lock" {
		t.Errorf("the lock is %q", tree.Root.Lock)
	}
	_, err := parseTestTree(t, "tree:\n  seq:\n    - {lock: x.lock, args: [\"true\"]}\n")
	if err == nil || !strings.Contains(err.Error(), "only the root node can have a lock") {
		t.Errorf("the error is %v", err)
	}
}
