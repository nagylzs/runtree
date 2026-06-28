package gtkui

import (
	"sync/atomic"
	"time"

	"github.com/diamondburned/gotk4/pkg/core/glib"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
	"github.com/nagylzs/runtree/internal/events"
	"github.com/nagylzs/runtree/internal/rt"
)

type NavMode = uint

const (
	NavModeTree NavMode = iota
	NavModeWaiting
	NavModeFrozen
	NavModeRunning
	NavModeSuccess
	NavModeFailed
	NavModeSkipped
	NavModePaused
)

var AllNavModesWithStatus = []NavMode{
	NavModeWaiting,
	NavModeFrozen,
	NavModeRunning,
	NavModeSuccess,
	NavModeFailed,
	NavModeSkipped,
	NavModePaused,
}

func NavModeToStatus(mode NavMode) rt.Status {
	switch mode {
	case NavModeTree:
		panic("there is no status for NavModeTree")
	case NavModeWaiting:
		return rt.StatusWaiting
	case NavModeFrozen:
		return rt.StatusFrozen
	case NavModeRunning:
		return rt.StatusRunning
	case NavModeSuccess:
		return rt.StatusSuccess
	case NavModeFailed:
		return rt.StatusFailed
	case NavModeSkipped:
		return rt.StatusSkipped
	case NavModePaused:
		return rt.StatusPaused
	default:
		panic("NavModeToStatus: invalid NavMode")
	}
}

func StatusToNavMode(status rt.Status) NavMode {
	switch status {
	case rt.StatusWaiting:
		return NavModeWaiting
	case rt.StatusFrozen:
		return NavModeFrozen
	case rt.StatusRunning:
		return NavModeRunning
	case rt.StatusSuccess:
		return NavModeSuccess
	case rt.StatusFailed:
		return NavModeFailed
	case rt.StatusSkipped:
		return NavModeSkipped
	case rt.StatusPaused:
		return NavModePaused
	default:
		panic("StatusToNavMode: invalid status")
	}
}

func NavModeName(navMode NavMode) string {
	if navMode == NavModeTree {
		return "Tree"
	}
	return rt.StatusName(NavModeToStatus(navMode))
}

// NodeNavigator displays RunTree
type NodeNavigator struct {
	*gtk.Fixed
	nodeViews    []*TreeNodeView
	nodeViewById map[string]*TreeNodeView
	added        map[string]bool
	layouter     *events.Debouncer[int]
	layouting    *atomic.Bool

	// selected is used internally to determine if the selection needs to be changed.
	// do not use it to retrieve the current selection.
	selected         *TreeNodeView
	mode             NavMode
	onlyShowRunNodes bool
}

func NewNodeNavigator() *NodeNavigator {
	t := &NodeNavigator{
		Fixed:            gtk.NewFixed(),
		nodeViews:        make([]*TreeNodeView, len(RunTree.Nodes)),
		nodeViewById:     make(map[string]*TreeNodeView),
		added:            make(map[string]bool),
		selected:         nil,
		layouting:        &atomic.Bool{},
		mode:             NavModeTree,
		onlyShowRunNodes: true,
	}
	t.layouter = events.NewDebouncer[int](t.debouncedLayout, true)
	t.layouting.Store(false)

	RunTree.RLock("NewNodeNavigator")
	defer RunTree.RUnlock()

	for i := range RunTree.Nodes {
		node := RunTree.Nodes[i]
		nw := NewTreeNodeView(t, node)
		t.nodeViews[i] = nw
		t.nodeViewById[node.Id] = nw
		t.added[node.Id] = false
	}
	for i := range RunTree.Nodes {
		t.nodeViews[i].Update(false, "NewNodeNavigator")
	}
	t.ConnectStateFlagsChanged(t.stateFlagsChanged)
	return t
}

// EnqueueLayoutItems requests layouting the items "soon"
func (t *NodeNavigator) EnqueueLayoutItems() {
	t.layouter.Call(0)
}

// EnqueueFullUpdate requests full update "soon"
func (t *NodeNavigator) EnqueueFullUpdate() {
	t.layouter.Call(1)
}

func (t *NodeNavigator) Mode() NavMode {
	return t.mode
}

func (t *NodeNavigator) SetMode(mode NavMode) {
	t.mode = mode
	t.EnqueueFullUpdate()
}

func (t *NodeNavigator) OnlyShowRunNodes() bool {
	return t.onlyShowRunNodes
}

func (t *NodeNavigator) SetOnlyShowRunNodes(onlyRunNodes bool) {
	t.onlyShowRunNodes = onlyRunNodes
	t.EnqueueFullUpdate()
}

func (t *NodeNavigator) stateFlagsChanged(flags gtk.StateFlags) {
	// stateFlagsChanged can be called by gtk while layoutItems is called
	// we do not call layoutItems here directly, but enqueue it instead
	// this reduces the number of calls
	t.EnqueueLayoutItems()
}

func (t *NodeNavigator) debouncedLayout(value int) {
	if value == 0 {
		glib.IdleAdd(t.layoutItems)
		time.Sleep(25 * time.Millisecond)
	} else {
		glib.IdleAdd(t.fullUpdate)
		time.Sleep(100 * time.Millisecond)
	}
}

// UpdateNode should be called when the tree is unlocked. It can be called from any thread.
func (t *NodeNavigator) UpdateNode(n *rt.Node, who string) {
	nodeView, ok := t.nodeViewById[n.Id]
	if !ok {
		return
	}
	glib.IdleAdd(func() {
		nodeView.Update(true, who)
	})
}

// LayoutItems must be called from the main gtk loop. The tree must not be locked.
func (t *NodeNavigator) layoutItems() {
	ok := t.layouting.CompareAndSwap(false, true)
	if !ok {
		return
	}
	defer t.layouting.Store(false)

	RunTree.RLock("LayoutItems")
	defer RunTree.RUnlock()

	switch t.mode {
	case NavModeTree:
		t.layoutTree()
	default:
		t.layoutList(t.mode)
	}
}

func (t *NodeNavigator) layoutList(mode NavMode) {
	st := NavModeToStatus(mode)

	// TODO: max width?
	tw := 1024

	var step float64 = 30
	var y float64 = 0
	for _, nodeView := range t.nodeViews {
		node := nodeView.Node
		nodeVisible := (node.Status == st) && (node.Type == rt.TypeRun || !t.onlyShowRunNodes)
		added := t.added[node.Id]
		if !nodeVisible && added {
			//t.Fixed.Remove(nodeView)
			//t.added[node.Id] = false
			nodeView.SetVisible(false)
		}
		if !nodeVisible {
			continue
		}
		_, natural, _, _ := nodeView.Measure(gtk.OrientationVertical, tw)
		nodeView.SetSizeRequest(tw, natural)
		if added {
			t.Fixed.Move(nodeView, 0, y)
		} else {
			t.Fixed.Put(nodeView, 0, y)
			t.added[node.Id] = true
		}
		nodeView.SetVisible(true)
		step = float64(natural)
		y += step
	}
}

func (t *NodeNavigator) layoutTree() {
	// TODO: max width?
	tw := 1024

	var ident float64 = 20
	var step float64 = 30
	var y float64 = 0
	for _, nodeView := range t.nodeViews {
		node := nodeView.Node
		nodeVisible, added := node.Visible(), t.added[node.Id]
		if !nodeVisible && added {
			//t.Fixed.Remove(nodeView)
			//t.added[node.Id] = false
			nodeView.SetVisible(false)
		}
		if !nodeVisible {
			continue
		}
		x := float64(node.Calculated.Level) * ident
		_, natural, _, _ := nodeView.Measure(gtk.OrientationVertical, tw)
		nodeView.SetSizeRequest(tw, natural)
		if added {
			t.Fixed.Move(nodeView, x, y)
		} else {
			t.Fixed.Put(nodeView, x, y)
			t.added[node.Id] = true
		}
		nodeView.SetVisible(true)
		step = float64(natural)
		y += step
	}
}

// fullUpdate must be called from gtk main loop, in unlocked state.
func (t *NodeNavigator) fullUpdate() {
	// Update all nodes
	ok := t.layouting.CompareAndSwap(false, true)
	if !ok {
		return
	}
	RunTree.RLock("fullUpdate")
	for _, n := range t.nodeViews {
		n.Update(false, "fullUpdate")
	}
	t.layouting.Store(false)
	RunTree.RUnlock()
	// then layout
	t.layoutItems()
}

// afterNodeClicked is called from a gtk signal
func (t *NodeNavigator) afterNodeClicked(nw *TreeNodeView, press int, x float64, y float64) {
	RunTree.Lock("afterNodeClicked")
	oldSel, changed := t.selected, false
	if oldSel != nw {
		changed = true
		t.selected = nw
		if oldSel != nil {
			oldSel.Selected = false
		}
		if t.selected != nil {
			t.selected.Selected = true
		}
	}
	RunTree.UnLock()

	if changed {
		if oldSel != nil {
			oldSel.Update(true, "afterNodeClicked")
		}
		if nw != nil {
			nw.Update(true, "afterNodeClicked")
		}
	}
	if nw != nil {
		Application.OnTreeNodeSelected(nw.Node)
	} else {
		Application.OnTreeNodeSelected(nil)
	}
}
