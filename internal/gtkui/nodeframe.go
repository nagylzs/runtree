package gtkui

import (
	"sync"

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

// NewNodeFrame creates a NodeFrame where the given updateGui function is called every second in the main gtk loop.
// It must be called from the main gtk loop.
func NewNodeFrame(updateGUI func()) *NodeFrame {
	f := &NodeFrame{node: nil, lck: &sync.Mutex{}, updateGui: updateGUI}
	if updateGUI != nil {
		f.start()
	}
	return f
}

// SetUpdateGUI must be called from the main gtk loop.
func (f *NodeFrame) SetUpdateGUI(updateGUI func()) {
	f.lck.Lock()
	defer f.lck.Unlock()
	needStart := f.updateGui == nil
	f.updateGui = updateGUI
	if needStart {
		f.start()
	}
}

// start calls updateGui every second with a timer of the main gtk loop (not from a goroutine, see runOnMain)
func (f *NodeFrame) start() {
	glib.TimeoutAdd(1000, func() bool {
		f.update()
		return true
	})
}

func (f *NodeFrame) update() {
	f.lck.Lock()
	updateGui := f.updateGui
	f.lck.Unlock()
	if updateGui != nil {
		updateGui()
	}
}

func (f *NodeFrame) SetNode(node *rt.Node) {
	f.lck.Lock()
	f.node = node
	f.lck.Unlock()
	runOnMain(f.update)
}

func (f *NodeFrame) Node() *rt.Node {
	f.lck.Lock()
	defer f.lck.Unlock()
	return f.node
}
