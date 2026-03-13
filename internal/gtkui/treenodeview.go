package gtkui

import (
	"fmt"
	"strings"
	"time"

	"github.com/diamondburned/gotk4/pkg/gtk/v4"
	"github.com/nagylzs/runtree/internal/rt"
)

const IconExpanded = "pan-down-symbolic"
const IconCollapsed = "pan-end-symbolic"

// TreeNodeView is a widget that can display a Node.
// You can create your composite widget and use it as a TreeNodeView.
type TreeNodeView struct {
	*gtk.Box
	hbox         *gtk.Box
	treeView     *NodeNavigator
	expander     *gtk.ToggleButton
	icon         *gtk.Image
	titleLabel   *gtk.Label
	typeLabel    *gtk.Label
	statusLabel  *gtk.Label
	elapsedLabel *gtk.Label
	prevStatus   rt.Status
	Node         *rt.Node
	Selected     bool
}

func NewTreeNodeView(tv *NodeNavigator, node *rt.Node) *TreeNodeView {
	tl := gtk.NewLabel("")
	tl.SetHExpand(true)
	tl.SetHAlign(gtk.AlignStart)
	tl.SetVAlign(gtk.AlignCenter)

	tyl := gtk.NewLabel("")
	tyl.SetHExpand(true)
	tyl.SetHAlign(gtk.AlignStart)
	tyl.SetVAlign(gtk.AlignCenter)
	tyl.AddCSSClass("type-label")
	tyl.AddCSSClass("node-status")
	tyl.SetVisible(node.Type != rt.TypeRun)

	sl := gtk.NewLabel("")
	sl.SetHExpand(true)
	sl.SetHAlign(gtk.AlignStart)
	sl.SetVAlign(gtk.AlignCenter)
	sl.AddCSSClass("status-label")
	sl.AddCSSClass("node-status")
	//sl.AddCSSClass("list-row")

	el := gtk.NewLabel("")
	el.AddCSSClass("node-elapsed")
	el.SetHAlign(gtk.AlignStart)
	el.SetVAlign(gtk.AlignStart)

	bxSecondRow := gtk.NewBox(gtk.OrientationHorizontal, 5)
	bxSecondRow.Append(tyl)
	bxSecondRow.Append(sl)
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

	bxExpanderAndRows := gtk.NewBox(gtk.OrientationHorizontal, 0)
	bxExpanderAndRows.SetHExpand(true)
	bxExpanderAndRows.SetHAlign(gtk.AlignStart)
	bxExpanderAndRows.SetVAlign(gtk.AlignCenter)
	bxExpanderAndRows.Append(exp)
	bxExpanderAndRows.Append(bxRows)
	bxExpanderAndRows.AddCSSClass("node")

	nw := &TreeNodeView{
		Box:          bxExpanderAndRows,
		hbox:         bxRows,
		treeView:     tv,
		expander:     exp,
		icon:         icon,
		titleLabel:   tl,
		typeLabel:    tyl,
		statusLabel:  sl,
		elapsedLabel: el,
		Node:         node,
		Selected:     false,
	}
	if exp != nil {
		exp.ConnectToggled(nw.toggleExpanded)
	}

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
	sm := node.SchedulerMessage
	hasChild := node.HasChild()
	expanded := node.Expanded
	nProc := node.NProc
	maxProc := node.MaxProc
	prevStatus := nw.prevStatus
	selected := nw.Selected
	nw.prevStatus = node.Status

	if lockTree {
		node.RUnlockTree()
	}

	nw.titleLabel.SetText(displayLabel)
	sn := rt.StatusName(status)
	icons := ""
	if hasTerm {
		icons += rt.TermEmoji
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
	if icons != "" {
		sn = icons + " " + sn
	}
	nw.statusLabel.SetText(sn)
	nw.statusLabel.RemoveCSSClass("status-" + strings.ToLower(rt.StatusName(prevStatus)))
	nw.statusLabel.AddCSSClass("status-" + strings.ToLower(rt.StatusName(status)))
	nw.statusLabel.SetTooltipText(rt.StatusDescription(status))

	nw.typeLabel.SetText(rt.TypeName(typ))

	if selected {
		nw.Box.SetStateFlags(gtk.StateFlagChecked, true)
	} else {
		nw.Box.SetStateFlags(gtk.StateFlagNormal, true)
	}

	elapsed, totalElapsed := node.CalcElapsed()
	el := nw.elapsedLabel
	if elapsed == time.Duration(0) {
		el.SetVisible(false)
	} else if (elapsed - totalElapsed).Abs() < time.Second {
		el.SetText(fmtDuration(elapsed))
		el.SetVisible(true)
	} else {
		el.SetText(fmt.Sprintf("%s tot=%s  ×%.1f",
			fmtDuration(elapsed), fmtDuration(totalElapsed), float64(totalElapsed)/float64(elapsed)))
		el.SetVisible(true)
	}

	nw.expander.SetSensitive(hasChild)
	if hasChild {
		if expanded {
			nw.icon.SetFromIconName(IconExpanded)
		} else {
			nw.icon.SetFromIconName(IconCollapsed)
		}
	}

}

func (nw *TreeNodeView) afterClicked(nPress int, x, y float64) {
	nw.treeView.afterNodeClicked(nw, nPress, x, y)
}
