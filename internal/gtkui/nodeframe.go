package gtkui

import (
	"sync"
	"time"

	"github.com/diamondburned/gotk4/pkg/core/glib"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
	"github.com/nagylzs/runtree/internal/rt"
)

type NodeFrameWithLabel interface {
	gtk.Widgetter
	TabLabel() gtk.Widgetter
}

// NodeFrame can hold a reference to an rt.Node and it can periodically update itself in the main gtk loop.
type NodeFrame struct {
	updateGui func()
	node      *rt.Node
	lck       *sync.Mutex
}

// NewNodeFrame creates a NodeFrame where the given updateGui function is called periodically in the main gtk loop
func NewNodeFrame(updateGUI func()) *NodeFrame {
	f := &NodeFrame{node: nil, lck: &sync.Mutex{}, updateGui: updateGUI}
	if updateGUI != nil {
		go f.run()
	}
	return f
}

func (f *NodeFrame) SetUpdateGUI(updateGUI func()) {
	f.lck.Lock()
	defer f.lck.Unlock()
	needStart := f.updateGui == nil
	f.updateGui = updateGUI
	if needStart {
		go f.run()
	}
}

func (f *NodeFrame) run() {
	for {
		time.Sleep(1 * time.Second)
		glib.IdleAdd(f.updateGui)
	}
}

func (f *NodeFrame) SetNode(node *rt.Node) {
	f.lck.Lock()
	f.node = node
	f.lck.Unlock()
	glib.IdleAdd(f.updateGui)
}

func (f *NodeFrame) Node() *rt.Node {
	f.lck.Lock()
	defer f.lck.Unlock()
	return f.node
}
