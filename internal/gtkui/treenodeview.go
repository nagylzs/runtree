package gtkui

import (
	"fmt"
	"strings"
	"time"

	"github.com/diamondburned/gotk4/pkg/core/glib"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
	"github.com/nagylzs/runtree/internal/rt"
)

const IconExpanded = "pan-down-symbolic"
const IconCollapsed = "pan-end-symbolic"
const IconShowInTree = "find-location-symbolic"

// TreeNodeView is a widget that can display a Node.
// You can create your composite widget and use it as a TreeNodeView.
type TreeNodeView struct {
	*gtk.Box              // the row in the navigator: it has the size of the row (see NodeNavigator.place)
	content      *gtk.Box // what the row shows, it fills the row; its own size is the size that its content needs
	hbox         *gtk.Box
	treeView     *NodeNavigator
	expander     *gtk.ToggleButton
	toggled      glib.SignalHandle // of the expander, blocked while Update sets it
	showInTree   *gtk.Button       // instead of the expander when the navigator shows a list
	icon         *gtk.Image
	titleLabel   *gtk.Label
	typeLabel    *gtk.Label
	status       *gtk.Box // the status of the node: the terminal mark and the status label
	termMark     *gtk.DrawingArea
	statusLabel  *gtk.Label
	elapsedLabel *gtk.Label
	shown        nodeViewState // what the widgets show, see Update
	Node         *rt.Node
	Selected     bool

	// the position and the size in the navigator (see NodeNavigator.place)
	x, y, width, height float64
}

// nodeViewState is what a TreeNodeView shows. Update changes only the widgets whose part of it changed: the navigator
// updates every node every second, and some changes restyle or redraw a widget even when the result is the same
// (removing and adding its CSS class, resetting its state flags); over a remote X server or VNC, every redraw goes over
// the network.
type nodeViewState struct {
	title, status, statusClass, tooltip, typeName string
	elapsed                                       string // empty: hidden
	hasTerm, selected, inList, hasChild, expanded bool
}

func NewTreeNodeView(tv *NodeNavigator, node *rt.Node) *TreeNodeView {
	tl := gtk.NewLabel("")
	tl.SetHExpand(true)
	tl.SetHAlign(gtk.AlignStart)
	tl.SetVAlign(gtk.AlignCenter)

	// the type, the status and the elapsed time follow each other; the spare width (when the title is wider) is after
	// the elapsed time
	tyl := gtk.NewLabel("")
	tyl.SetHAlign(gtk.AlignStart)
	tyl.SetVAlign(gtk.AlignCenter)
	tyl.AddCSSClass("type-label")
	tyl.AddCSSClass("node-status")
	tyl.SetVisible(node.Type != rt.TypeRun)

	sl := gtk.NewLabel("")
	sl.SetVAlign(gtk.AlignCenter)
	tm := newTermMark()
	tm.SetVisible(false)
	sbx := gtk.NewBox(gtk.OrientationHorizontal, 0)
	sbx.SetHAlign(gtk.AlignStart)
	sbx.SetVAlign(gtk.AlignCenter)
	sbx.AddCSSClass("status-label")
	sbx.AddCSSClass("node-status")
	sbx.Append(tm)
	sbx.Append(sl)

	el := gtk.NewLabel("")
	el.AddCSSClass("node-elapsed")
	el.SetHExpand(true)
	el.SetHAlign(gtk.AlignStart)
	el.SetVAlign(gtk.AlignStart)

	bxSecondRow := gtk.NewBox(gtk.OrientationHorizontal, 5)
	bxSecondRow.Append(tyl)
	bxSecondRow.Append(sbx)
	bxSecondRow.Append(el)

	bxRows := gtk.NewBox(gtk.OrientationVertical, 0)
	bxRows.SetHExpand(true)
	bxRows.SetHAlign(gtk.AlignStart)
	bxRows.SetVAlign(gtk.AlignCenter)
	bxRows.Append(tl)
	bxRows.Append(bxSecondRow)

	exp := gtk.NewToggleButton()
	exp.SetActive(node.Expanded)

	var icon *gtk.Image
	if node.HasChild() {
		icon = gtk.NewImageFromIconName(IconCollapsed)
		exp.SetChild(icon)
	} else {
		icon = gtk.NewImage()
		exp.SetChild(icon)
	}

	show := gtk.NewButtonFromIconName(IconShowInTree)
	show.SetTooltipText("Show it in the tree")
	show.SetVisible(false)

	bxExpanderAndRows := gtk.NewBox(gtk.OrientationHorizontal, 0)
	bxExpanderAndRows.SetHExpand(true)
	bxExpanderAndRows.SetVAlign(gtk.AlignCenter)
	bxExpanderAndRows.Append(exp)
	bxExpanderAndRows.Append(show)
	bxExpanderAndRows.Append(bxRows)
	bxExpanderAndRows.AddCSSClass("node")

	// The size request of the row is on this box, not on the content: GTK measures a widget at least as large as its
	// size request, so the content could not tell that it needs less when the row gets narrower.
	row := gtk.NewBox(gtk.OrientationHorizontal, 0)
	row.Append(bxExpanderAndRows)

	nw := &TreeNodeView{
		Box:          row,
		content:      bxExpanderAndRows,
		hbox:         bxRows,
		treeView:     tv,
		expander:     exp,
		showInTree:   show,
		icon:         icon,
		titleLabel:   tl,
		typeLabel:    tyl,
		status:       sbx,
		termMark:     tm,
		statusLabel:  sl,
		elapsedLabel: el,
		Node:         node,
		Selected:     false,
	}
	nw.toggled = exp.ConnectToggled(nw.toggleExpanded)
	show.ConnectClicked(func() {
		defer recoverPanic("show in the tree")
		Application.pane.ShowInTree(node)
	})

	gc := gtk.NewGestureClick()
	nw.AddController(gc)
	gc.ConnectPressed(nw.afterClicked)

	nw.Update(false, "NewTreeNodeView")
	return nw
}

// toggleExpanded is called from a gtk signal, while the tree is unlocked
func (nw *TreeNodeView) toggleExpanded() {
	expanded := nw.expander.Active()
	node := nw.Node
	if node.Expanded != expanded {
		node.LockTree("afterNodeClicked")
		node.Expanded = expanded
		node.UnLockTree()
	}
	if node.Expanded != expanded {
		nw.Update(true, "toggleExpanded")
		nw.treeView.EnqueueLayoutItems()
	}

}

// Update will update an existing TreeNodeView so that it displays the given Node.
func (nw *TreeNodeView) Update(lockTree bool, who string) {
	node := nw.Node

	pid := 0
	hasTerm := false
	if lockTree {
		node.RLockTree(who + " -> Update")
	}
	displayLabel := node.DisplayLabel()
	status := node.Status
	typ := node.Type
	if node.Client != nil {
		pid = node.Client.LastState().Pid
		hasTerm = pid != 0
	}
	connecting := typ == rt.TypeRun && node.Starting(true)
	sm := node.SchedulerMessage
	timedOut := node.TimedOut
	hasChild := node.HasChild()
	expanded := node.Expanded
	nProc := node.NProc
	maxProc := node.MaxProc
	selected := nw.Selected
	failed, stopped := 0, ""
	if hasChild && !rt.StatusFinished(status) {
		failed = node.FailedRunNodes(true)
		stopped = node.Stopped(true)
	}

	if lockTree {
		node.RUnlockTree()
	}

	sn := rt.StatusName(status)
	if timedOut {
		sn += ", timed out"
	}
	if connecting {
		sn += ", connecting"
	}
	if sm != "" {
		sn += ", " + sm
	}
	if pid > 0 {
		sn += fmt.Sprintf(" PID=%d", pid)
	} else if nProc > 0 {
		if maxProc > 0 {
			sn += fmt.Sprintf(" (%d/%d)", nProc, maxProc)
		} else {
			sn += fmt.Sprintf(" (%d)", nProc)
		}
	}
	if stopped != "" {
		sn += ", " + stopped // the paused nodes tell the failures
	} else if failed > 0 {
		sn += fmt.Sprintf(", %d failed", failed)
	}
	st := nodeViewState{
		title:       displayLabel,
		status:      sn,
		statusClass: "status-" + strings.ToLower(rt.StatusName(status)),
		tooltip:     rt.StatusDescription(status),
		typeName:    rt.TypeName(typ),
		hasTerm:     hasTerm,
		selected:    selected,
		inList:      nw.treeView.Mode() != NavModeTree,
		hasChild:    hasChild,
		expanded:    expanded,
	}
	elapsed, totalElapsed := node.CalcElapsed()
	if elapsed != time.Duration(0) {
		if (elapsed - totalElapsed).Abs() < time.Second {
			st.elapsed = fmtDuration(elapsed)
		} else {
			st.elapsed = fmt.Sprintf("%s tot=%s  ×%.1f",
				fmtDuration(elapsed), fmtDuration(totalElapsed), float64(totalElapsed)/float64(elapsed))
		}
	}
	nw.show(st)
}

// show changes the widgets whose part of the state changed (a zero shown state shows everything)
func (nw *TreeNodeView) show(st nodeViewState) {
	old, first := nw.shown, nw.shown == nodeViewState{}
	nw.shown = st
	if first || st.title != old.title {
		nw.titleLabel.SetText(st.title)
	}
	if first || st.status != old.status {
		nw.statusLabel.SetText(st.status)
	}
	if first || st.hasTerm != old.hasTerm {
		nw.termMark.SetVisible(st.hasTerm)
	}
	if first || st.statusClass != old.statusClass {
		if old.statusClass != "" {
			nw.status.RemoveCSSClass(old.statusClass)
		}
		nw.status.AddCSSClass(st.statusClass)
	}
	if first || st.tooltip != old.tooltip {
		nw.status.SetTooltipText(st.tooltip)
	}
	if first || st.typeName != old.typeName {
		nw.typeLabel.SetText(st.typeName)
	}
	if first || st.selected != old.selected {
		if st.selected {
			nw.content.SetStateFlags(gtk.StateFlagChecked, false)
		} else {
			nw.content.UnsetStateFlags(gtk.StateFlagChecked)
		}
	}
	if first || st.elapsed != old.elapsed {
		nw.elapsedLabel.SetText(st.elapsed)
		nw.elapsedLabel.SetVisible(st.elapsed != "")
	}
	if first || st.inList != old.inList {
		nw.expander.SetVisible(!st.inList)
		nw.showInTree.SetVisible(st.inList)
	}
	if first || st.hasChild != old.hasChild {
		nw.expander.SetSensitive(st.hasChild)
	}
	// Expanded is also changed by the scheduler (expand_on_active, collapse_on_finished) and by ShowNode
	if nw.expander.Active() != st.expanded {
		nw.expander.HandlerBlock(nw.toggled)
		nw.expander.SetActive(st.expanded)
		nw.expander.HandlerUnblock(nw.toggled)
	}
	if st.hasChild && (first || st.expanded != old.expanded) {
		if st.expanded {
			nw.icon.SetFromIconName(IconExpanded)
		} else {
			nw.icon.SetFromIconName(IconCollapsed)
		}
	}
}

func (nw *TreeNodeView) afterClicked(nPress int, x, y float64) {
	nw.treeView.afterNodeClicked(nw, nPress, x, y)
}
