package rt

import "testing"

// The level of a node (the indentation in the GUI) is its depth in the tree, also below includes: an included object
// that replaces the node that includes it (a single include), also in a chain, and the objects of a list of includes.
func TestLevelBelowIncludes(t *testing.T) {
	tree := loadTestTree(t, `tree:
  title: upgrade
  par:
    - title: eugift-dev
      include: app
    - title: both
      include: [job, job]
    - seq:
        - include: job
app:
  include: app2
app2:
  par:
    - {title: build, args: ["true"]}
    - title: n507
      seq:
        - {title: stop, args: ["true"]}
        - include: job
        - include: [job, job]
job:
  title: job
  args: ["true"]
`)
	var check func(n *Node, depth uint)
	count := 0
	check = func(n *Node, depth uint) {
		count++
		if n.Calculated.Level != depth {
			t.Errorf("%s: level %d, want %d", nodePath(n), n.Calculated.Level, depth)
		}
		for _, c := range n.Nodes {
			check(c, depth+1)
		}
	}
	check(tree.Root, 0)
	if count != 14 {
		t.Errorf("%d nodes, want 14", count)
	}
}
