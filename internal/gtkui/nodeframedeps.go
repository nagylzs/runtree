package gtkui

import (
	"fmt"
	"strings"

	"github.com/nagylzs/runtree/internal/rt"
	"github.com/nagylzs/set"
)

type NodeFrameDeps struct {
	*NodeFrameGrid
}

func NewNodeFrameDeps() *NodeFrameDeps {
	g := NewNodeFrameGrid("", nil)
	p := &NodeFrameDeps{g}
	p.buildGui()
	p.NodeFrameGrid.SetUpdateGui(p.updateGUI)
	return p
}

func (p *NodeFrameDeps) buildGui() {
	p.addStringProp("Provides", "Provides",
		"Resources provided by this node", 0, 12)
	p.nextRow()
	p.addStringProp("Requires", "Requires",
		"Resources required by this node", 0, 12)
	p.nextRow()
	p.addStringProp("MissingRequired", "MissingRequired",
		"Resources that are not yet provided and block this node", 0, 12)
	p.nextRow()
	p.addStringProp("RLocks", "RLocks",
		"Read-only (shared) locks required by this node", 0, 12)
	p.nextRow()
	p.addStringProp("XLocks", "XLocks",
		"Exclusive locks required by this node", 0, 12)
	p.nextRow()
	p.addStringProp("CannotRAcquire", "CannotRAcquire",
		"Locks that are r-acquired by other node(s) and block this node.", 0, 12)
	p.nextRow()
	p.addStringProp("CannotXAcquire", "CannotXAcquire",
		"Locks that are x-acquired by other node(s) and block this node.", 0, 12)
	p.nextRow()
	p.addStringProp("SchedulerMessage", "SchedulerMessage",
		"Additional informational message set by the scheduler on this node.", 0, 12)
	p.nextRow()

}

func (p *NodeFrameDeps) updateGUI() {
	node := p.frame.Node()

	if node == nil {
		p.tabLabel.SetVisible(false)
		return
	}
	p.tabLabel.SetVisible(true)
	node.RLockTree("NodeFrameDeps.UpdateGUI")
	defer node.RUnlockTree()

	usedProps := set.NewSet[string]()

	setStringProp := func(id string, value string) {
		pe, ok := p.propEntries[id]
		if !ok {
			panic("No prop entry for " + id)
		}
		cpy := p.copyButtons[id]
		pe.SetText(value)
		cpy.SetSensitive(value != "")
		if value != "" {
			usedProps.Add(id)
		}
	}

	hasStates := set.NewSet[string]()

	setStringProp("Provides", strings.Join(node.Calculated.Provides.List(), ", "))
	setStringProp("Requires", strings.Join(node.Calculated.Requires.List(), ", "))
	mr := strings.Join(node.MissingRequired.List(), ", ")
	setStringProp("MissingRequired", mr)
	if mr == "" {
		p.setPropState("MissingRequired", "", hasStates)
	} else {
		p.setPropState("MissingRequired", "waiting", hasStates)
	}
	setStringProp("RLocks", strings.Join(node.Calculated.RLocks.List(), ", "))
	setStringProp("XLocks", strings.Join(node.Calculated.XLocks.List(), ", "))

	cxa := strings.Join(node.CannotXAcquire.List(), ", ")
	setStringProp("CannotXAcquire", cxa)
	if cxa == "" {
		p.setPropState("CannotXAcquire", "", hasStates)
	} else {
		p.setPropState("CannotXAcquire", "waiting", hasStates)
	}

	cra := strings.Join(node.CannotRAcquire.List(), ", ")
	setStringProp("CannotRAcquire", cra)
	if cra == "" {
		p.setPropState("CannotRAcquire", "", hasStates)
	} else {
		p.setPropState("CannotRAcquire", "waiting", hasStates)
	}

	setStringProp("SchedulerMessage", node.SchedulerMessage)

	// We only display rows that are needed, but if a row is needed then all of its props are displayed.
	cnt := 0
	for row := 0; row < p.rowCount; row++ {
		vis := !usedProps.Intersection(p.rowProps[row]).Empty()
		for _, id := range p.rowProps[row].List() {
			p.propLabels[id].SetVisible(vis)
			p.propEntries[id].SetVisible(vis)
			p.copyButtons[id].SetVisible(vis)
			if vis {
				cnt++
			}
		}
	}
	if cnt == 0 {
		p.tabLabel.SetText("Dependencies")
	} else {
		p.tabLabel.SetMarkup(fmt.Sprintf("<b>Dependencies</b> (%d)", cnt))
		p.tabLabel.SetVisible(true)
	}
	p.updateTabState(hasStates)
	p.Grid.QueueResize()
}

func (p *NodeFrameDeps) SetNode(node *rt.Node) {
	p.frame.SetNode(node)
}
