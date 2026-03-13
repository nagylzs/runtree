package gtkui

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/diamondburned/gotk4/pkg/gtk/v4"
	"github.com/nagylzs/runtree/internal/rt"
)

type NodeFrameEnvs struct {
	*gtk.TextView
	frame    *NodeFrame
	tabLabel *gtk.Label
}

func NewNodeFrameEnvs() *NodeFrameEnvs {
	d := &NodeFrameEnvs{
		TextView: gtk.NewTextView(),
		tabLabel: gtk.NewLabel("Vars"),
	}
	d.TextView.SetEditable(false)
	d.TextView.SetMonospace(true)
	d.frame = NewNodeFrame(d.updateGUI)
	return d

}

func (d *NodeFrameEnvs) TabLabel() gtk.Widgetter {
	return d.tabLabel
}

func (d *NodeFrameEnvs) SetNode(node *rt.Node) {
	d.frame.SetNode(node)
}

func (d *NodeFrameEnvs) updateGUI() {
	node := d.frame.Node()

	if node == nil {
		d.tabLabel.SetVisible(false)
		return
	}

	node.RLockTree("NodeFrameEnvs.UpdateGUI")
	envs := maps.Clone(node.Calculated.Envs)
	node.RUnlockTree()

	items := make([]string, 0, len(envs))
	keys := slices.Collect(maps.Keys(envs))
	slices.Sort(keys)
	for _, k := range keys {
		items = append(items, fmt.Sprintf(`%s="%s"`, k, envs[k]))
	}
	buffer := d.TextView.Buffer()
	buffer.SetText(strings.Join(items, "\n"))

	if len(items) == 0 {
		d.tabLabel.SetText("Envs")
	} else {
		d.tabLabel.SetMarkup(fmt.Sprintf("<b>Envs</b> (%d)", len(items)))
	}

}
