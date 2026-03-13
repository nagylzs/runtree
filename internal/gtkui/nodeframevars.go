package gtkui

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/diamondburned/gotk4/pkg/gtk/v4"
	"github.com/nagylzs/runtree/internal/rt"
)

type NodeFrameVars struct {
	*gtk.TextView
	frame    *NodeFrame
	tabLabel *gtk.Label
}

func NewNodeFrameVars() *NodeFrameVars {
	d := &NodeFrameVars{
		TextView: gtk.NewTextView(),
		tabLabel: gtk.NewLabel("Vars"),
	}
	d.TextView.SetEditable(false)
	d.TextView.SetMonospace(true)
	d.frame = NewNodeFrame(d.updateGUI)
	return d

}

func (d *NodeFrameVars) TabLabel() gtk.Widgetter {
	return d.tabLabel
}

func (d *NodeFrameVars) SetNode(node *rt.Node) {
	d.frame.SetNode(node)
}

func (d *NodeFrameVars) updateGUI() {
	node := d.frame.Node()

	if node == nil {
		d.tabLabel.SetVisible(false)
		return
	}

	node.RLockTree("NodeFrameVars.UpdateGUI")
	vars := maps.Clone(node.Calculated.Vars)
	node.RUnlockTree()

	items := make([]string, 0, len(vars))
	keys := slices.Collect(maps.Keys(vars))
	slices.Sort(keys)
	for _, k := range keys {
		items = append(items, fmt.Sprintf(`%s="%s"`, k, vars[k]))
	}
	buffer := d.TextView.Buffer()
	buffer.SetText(strings.Join(items, "\n"))

	if len(items) == 0 {
		d.tabLabel.SetText("Vars")
	} else {
		d.tabLabel.SetMarkup(fmt.Sprintf("<b>Vars</b> (%d)", len(items)))
	}

}
