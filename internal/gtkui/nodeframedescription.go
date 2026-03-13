package gtkui

import (
	"fmt"
	"strings"

	"github.com/diamondburned/gotk4/pkg/gtk/v4"
	"github.com/nagylzs/runtree/internal/rt"
)

type NodeFrameDescription struct {
	*gtk.TextView
	frame    *NodeFrame
	tabLabel *gtk.Label
}

func NewNodeFrameDescription() *NodeFrameDescription {
	d := &NodeFrameDescription{
		TextView: gtk.NewTextView(),
		tabLabel: gtk.NewLabel("Description"),
	}
	d.TextView.SetEditable(false)
	d.frame = NewNodeFrame(d.updateGUI)
	return d

}

func (d *NodeFrameDescription) TabLabel() gtk.Widgetter {
	return d.tabLabel
}

func (d *NodeFrameDescription) SetNode(node *rt.Node) {
	d.frame.SetNode(node)
}

func (d *NodeFrameDescription) updateGUI() {
	node := d.frame.Node()

	if node == nil {
		d.tabLabel.SetVisible(false)
		return
	}

	node.RLockTree("NodeFrameDescription.UpdateGUI")
	description := node.Calculated.Description
	node.RUnlockTree()

	value := strings.TrimSpace(description)

	buffer := d.TextView.Buffer()
	buffer.SetText(value)

	if value == "" {
		d.tabLabel.SetText("Description")
	} else {
		d.tabLabel.SetMarkup(fmt.Sprintf("<b>Description</b> (%d)", len(value)))
	}

}
