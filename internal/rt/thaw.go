package rt

import (
	"fmt"
	"strings"
)

// FindNodes returns the nodes whose id, title (DisplayLabel) or path is name, in the order of the tree
func (t *Tree) FindNodes(name string) []*Node {
	var found []*Node
	var walk func(n *Node)
	walk = func(n *Node) {
		if n.Id == name || n.DisplayLabel() == name || n.Path() == name {
			found = append(found, n)
		}
		for _, c := range n.Nodes {
			walk(c)
		}
	}
	walk(t.Root)
	return found
}

// ThawGates thaws the approval gates of the tree file with these names (id, title or path), before the run (--thaw):
// the nodes that have their own "status: frozen", and the sub-nodes that are frozen because they inherit it. A
// sub-node with its own "status: frozen" is another gate, it stays frozen. It must be called in locked state, before
// the scheduler starts.
func (t *Tree) ThawGates(locked bool, names []string) error {
	if !locked {
		panic("must be called in locked state")
	}
	for _, name := range names {
		nodes := t.FindNodes(name)
		switch len(nodes) {
		case 0:
			return fmt.Errorf("no node is %q (its id, title or path)", name)
		case 1:
		default:
			var lines []string
			for _, n := range nodes {
				lines = append(lines, fmt.Sprintf("  %s: %s", n.Id, n.Path()))
			}
			return fmt.Errorf("%d nodes are %q, use the id:\n%s", len(nodes), name, strings.Join(lines, "\n"))
		}
		if err := nodes[0].thawGate(); err != nil {
			return err
		}
	}
	t.Root.updateStatusesDown(true)
	return nil
}

// thawGate thaws a node with its own "status: frozen", and its sub-nodes that inherit it, see ThawGates
func (n *Node) thawGate() error {
	if !n.frozenGate {
		if n.frozenInFile {
			gate := n.Parent
			for gate != nil && !gate.frozenGate {
				gate = gate.Parent
			}
			if gate != nil {
				return fmt.Errorf("%s is frozen because %s is frozen (status: frozen): thaw that one", n.Path(),
					gate.Path())
			}
		}
		return fmt.Errorf("%s is not frozen in the tree file (status: frozen)", n.Path())
	}
	if !n.frozenInFile {
		return nil // thawed already (it was given twice)
	}
	var thaw func(m *Node)
	thaw = func(m *Node) {
		if _, err := m.performOp(OpThaw, true, false, false); err != nil {
			panic("Node.thawGate: " + err.Error()) // a node that is frozen in the file can be thawed
		}
		m.frozenInFile = false
		for _, c := range m.Nodes {
			if c.frozenInFile && !c.frozenGate {
				thaw(c)
			}
		}
	}
	thaw(n)
	n.log().Info("thawed (--thaw)", attrEvent, EventOperation, "op", OpName(OpThaw))
	return nil
}
