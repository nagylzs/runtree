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

// levelIndent is the indentation of a level of the tree in the navigator
const levelIndent = 20

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

	viewport *gtk.Viewport // of the scrolled window that shows the navigator
	scrollTo *TreeNodeView // ShowNode scrolls to it after the next layout (main gtk loop only)
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
		runOnMain(t.layoutItems)
		time.Sleep(25 * time.Millisecond)
	} else {
		runOnMain(t.fullUpdate)
		time.Sleep(100 * time.Millisecond)
	}
}

// UpdateNode should be called when the tree is unlocked. It can be called from any thread.
func (t *NodeNavigator) UpdateNode(n *rt.Node, who string) {
	nodeView, ok := t.nodeViewById[n.Id]
	if !ok {
		return
	}
	runOnMain(func() {
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
	began := time.Now()
	defer func() { logSlow("navigator layout", began) }()

	RunTree.RLock("LayoutItems")
	defer RunTree.RUnlock()

	switch t.mode {
	case NavModeTree:
		t.layoutTree()
	default:
		t.layoutList(t.mode)
	}
	if nw := t.scrollTo; nw != nil && t.mode == NavModeTree && t.viewport != nil {
		t.scrollTo = nil
		// after the moved node views are allocated
		glib.IdleAdd(func() {
			t.scrollToView(nw)
		})
	}
}

// scrollToView scrolls the navigator to the node view: vertically to its middle when it is not visible as a whole,
// and horizontally to its left edge when that is not visible. (Viewport.ScrollTo would scroll the views, that are
// wider than the navigator, to their right end, where they show nothing.)
func (t *NodeNavigator) scrollToView(nw *TreeNodeView) {
	v := t.viewport.VAdjustment()
	if nw.y < v.Value() || nw.y+nw.height > v.Value()+v.PageSize() {
		v.SetValue(nw.y + nw.height/2 - v.PageSize()/2) // it is clamped to the range
	}
	h := t.viewport.HAdjustment()
	if nw.x < h.Value() || nw.x > h.Value()+h.PageSize()-levelIndent {
		h.SetValue(nw.x - levelIndent)
	}
}

// ShowNode selects the node in the tree view mode, expands its parents, and scrolls to it. It must be called from
// the main gtk loop, with the tree unlocked, after the mode is set to NavModeTree.
func (t *NodeNavigator) ShowNode(node *rt.Node) {
	nw, ok := t.nodeViewById[node.Id]
	if !ok {
		return
	}
	RunTree.Lock("ShowNode")
	for p := node.Parent; p != nil; p = p.Parent {
		p.Expanded = true
	}
	RunTree.UnLock()
	t.scrollTo = nw
	t.selectView(nw)
	t.EnqueueFullUpdate()
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
		step = t.place(nodeView, 0, y, tw, added)
		y += step
	}
}

// place puts the node view to x, y with the width tw, and returns its height. It only changes the size and the
// position of the view when they changed (it would be laid out again).
func (t *NodeNavigator) place(nodeView *TreeNodeView, x, y float64, tw int, added bool) float64 {
	_, natural, _, _ := nodeView.Measure(gtk.OrientationVertical, tw)
	if nodeView.width != float64(tw) || nodeView.height != float64(natural) {
		nodeView.SetSizeRequest(tw, natural)
		nodeView.width, nodeView.height = float64(tw), float64(natural)
	}
	switch {
	case !added:
		t.Fixed.Put(nodeView, x, y)
		t.added[nodeView.Node.Id] = true
	case nodeView.x != x || nodeView.y != y:
		t.Fixed.Move(nodeView, x, y)
	}
	nodeView.x, nodeView.y = x, y
	nodeView.SetVisible(true)
	return float64(natural)
}

func (t *NodeNavigator) layoutTree() {
	// TODO: max width?
	tw := 1024

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
		x := float64(node.Calculated.Level) * levelIndent
		step = t.place(nodeView, x, y, tw, added)
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
	began := time.Now()
	RunTree.RLock("fullUpdate")
	for _, n := range t.nodeViews {
		n.Update(false, "fullUpdate")
	}
	t.layouting.Store(false)
	RunTree.RUnlock()
	logSlow("navigator update", began, "nodes", len(t.nodeViews))
	// then layout
	t.layoutItems()
}

// afterNodeClicked is called from a gtk signal
func (t *NodeNavigator) afterNodeClicked(nw *TreeNodeView, press int, x float64, y float64) {
	defer recoverPanic("node selection")
	t.selectView(nw)
}

// selectView selects the node view (nil: no selection), and shows the node on the right side. It must be called from
// the main gtk loop, with the tree unlocked.
func (t *NodeNavigator) selectView(nw *TreeNodeView) {
	oldSel, changed := t.selected, false
	func() {
		RunTree.Lock("afterNodeClicked")
		defer RunTree.UnLock()
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
	}()

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
