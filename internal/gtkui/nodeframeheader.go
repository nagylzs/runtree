package gtkui

import (
	"context"
	"fmt"
	"sort"
	"strings"

	glib2 "github.com/diamondburned/gotk4/pkg/glib/v2"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
	"github.com/nagylzs/runtree/internal/rt"
	"github.com/nagylzs/runtree/internal/signal"
)

type NodeFrameHeader struct {
	*gtk.Box

	frame                     *NodeFrame
	signalPopoverRecursive    *gtk.Popover
	signalPopoverNonRecursive *gtk.Popover

	title  *gtk.Label
	path   *gtk.Label
	status *gtk.Label
	typ    *gtk.Label

	opButtonsRecursive    map[rt.NodeOp]*gtk.Button
	opButtonsNonRecursive map[rt.NodeOp]*gtk.Button

	prevStatus rt.Status
}

func NewHeaderFrame() *NodeFrameHeader {
	title := gtk.NewLabel("")
	title.AddCSSClass("tools-title")
	title.SetHExpand(true)
	title.SetHAlign(gtk.AlignStart)
	title.SetVAlign(gtk.AlignCenter)

	path := gtk.NewLabel("")
	path.AddCSSClass("tools-path")
	path.SetHExpand(true)
	path.SetHAlign(gtk.AlignStart)
	path.SetVAlign(gtk.AlignEnd)

	typ := gtk.NewLabel("")
	typ.SetHAlign(gtk.AlignStart)
	typ.SetVAlign(gtk.AlignStart)
	typ.SetHExpand(false)
	typ.AddCSSClass("type-label")

	status := gtk.NewLabel("")
	status.SetHAlign(gtk.AlignStart)
	status.SetVAlign(gtk.AlignStart)
	status.SetHExpand(false)
	status.AddCSSClass("status-label")

	ops := gtk.NewBox(gtk.OrientationHorizontal, 5)
	ops.SetHExpand(true)
	ops.SetHAlign(gtk.AlignStart)
	ops.SetVAlign(gtk.AlignCenter)
	ops.Append(typ)

	opsRec := gtk.NewBox(gtk.OrientationHorizontal, 5)
	opsRec.SetHExpand(true)
	opsRec.SetHAlign(gtk.AlignStart)
	opsRec.SetVAlign(gtk.AlignCenter)
	opsRec.Append(status)

	bx := gtk.NewBox(gtk.OrientationVertical, 5)
	bx.SetMarginStart(5)
	bx.SetMarginEnd(5)
	bx.Append(title)
	bx.Append(path)
	bx.Append(ops)
	bx.Append(opsRec)
	bx.SetHExpand(true)
	bx.SetVExpand(true)

	h := &NodeFrameHeader{
		Box:                   bx,
		title:                 title,
		path:                  path,
		typ:                   typ,
		status:                status,
		opButtonsRecursive:    map[rt.NodeOp]*gtk.Button{},
		opButtonsNonRecursive: map[rt.NodeOp]*gtk.Button{},
	}
	h.frame = NewNodeFrame(h.updateGUI)

	createOpButton := func(op rt.NodeOp, recursive bool) *gtk.Button {
		btn := gtk.NewButton()
		btn.SetLabel(rt.OpName(op))
		btn.SetSensitive(false)
		if op != rt.OpSignal {
			btn.ConnectClicked(func() {
				h.onOp(op, recursive)
			})
		}
		if recursive {
			opsRec.Append(btn)
		} else {
			ops.Append(btn)
		}
		return btn
	}
	for _, op := range rt.AllOps {
		h.opButtonsRecursive[op] = createOpButton(op, true)
	}
	for _, op := range rt.AllOps {
		h.opButtonsNonRecursive[op] = createOpButton(op, false)
	}

	populateSignalPopover := func(btnSignal *gtk.Button, recursive bool) *gtk.Popover {

		popOver := gtk.NewPopover()
		bxs := gtk.NewBox(gtk.OrientationVertical, 5)
		bxs.SetMarginStart(5)
		bxs.SetMarginEnd(5)

		signames := make([]string, 0, len(signal.Signals))
		for k := range signal.Signals {
			signames = append(signames, k)
		}
		sort.Strings(signames)
		for _, name := range signames {
			btn := gtk.NewButtonWithLabel(name)
			btn.ConnectClicked(func() {
				popOver.Popdown()
				h.sendSignal(name, recursive)
			})
			if name == "SIGINT" {
				btn.SetLabel("Ctrl+C️ SIGINT")
				btn.SetTooltipText("This is the default action to end a program gracefully.")
				btn.AddCSSClass("button-sigint")
			}
			if name == "SIGKILL" {
				btn.SetLabel("☠️ SIGKILL")
				btn.SetTooltipText("A nuclear solution to your problem. Cannot be caught or ignored.")
				btn.AddCSSClass("button-sigkill")
			}
			if name == "SIGHUP" {
				btn.SetLabel("⟳ SIGHUP")
				btn.SetTooltipText("Most services interpret this as 'reload your config'. Closes interactive programs.")
				btn.AddCSSClass("button-sighup")
			}
			if name == "SIGABRT" {
				btn.SetLabel("💣 SIGABORT")
				btn.SetTooltipText("Terminate with core dump.")
				btn.AddCSSClass("button-sigabort")
			}

			bxs.Append(btn)
		}

		//popOver.SetHasArrow(true)
		popOver.SetPosition(gtk.PosBottom)
		popOver.SetParent(btnSignal)
		popOver.SetChild(bxs)
		btnSignal.ConnectClicked(func() { popOver.Popup() })
		return popOver
	}

	h.signalPopoverRecursive = populateSignalPopover(h.opButtonsRecursive[rt.OpSignal], true)
	h.signalPopoverNonRecursive = populateSignalPopover(h.opButtonsNonRecursive[rt.OpSignal], false)

	return h

}

func (w *NodeFrameHeader) SetNode(node *rt.Node) {
	w.frame.SetNode(node)
}

func (w *NodeFrameHeader) updateGUI() {
	node := w.frame.Node()

	if node == nil {
		return
	}

	path := make([]string, 0, 10)
	node.RLockTree("NodeFrameTools.UpdateGUI")
	defer node.RUnlockTree()

	newStatus := node.Status
	prevStatus := w.prevStatus
	w.prevStatus = newStatus
	displayLabel := node.DisplayLabel()
	n := node
	for ; n != nil; n = n.Parent {
		if n.Calculated.Title == "" {
			path = append(path, n.Id)
		} else {
			path = append(path, n.Calculated.Title)
		}
	}
	st := rt.StatusName(newStatus)
	if node.SchedulerMessage != "" {
		st += ", " + node.SchedulerMessage
	}
	typ := rt.TypeName(node.Type)

	w.title.SetText(displayLabel)
	w.path.SetMarkup("<b>Path:</b> " + glib2.MarkupEscapeText(w.breadCrumbs(path)) + " Id=" + node.Id)
	w.typ.SetText(typ)

	w.status.RemoveCSSClass("status-" + strings.ToLower(rt.StatusName(prevStatus)))
	w.status.AddCSSClass("status-" + strings.ToLower(rt.StatusName(newStatus)))
	w.status.SetText(st)
	w.status.SetTooltipText(rt.StatusDescription(newStatus))

	ops := node.AllowedManualOps(true, true)
	for op, btn := range w.opButtonsRecursive {
		_, ok := ops[op]
		btn.SetVisible(node.HasChild())
		btn.SetSensitive(ok)
		title := rt.OpName(op)
		if rt.StatusFinished(newStatus) && op == rt.OpRun {
			title = "Re-run"
		}
		if node.HasChild() {
			title += " all"
		}
		btn.SetLabel(title)
	}
	ops = node.AllowedManualOps(true, false)
	for op, btn := range w.opButtonsNonRecursive {
		_, ok := ops[op]
		btn.SetSensitive(ok)
		title := rt.OpName(op)
		if rt.StatusFinished(newStatus) && op == rt.OpRun {
			title = "Re-run"
		}
		btn.SetLabel(title)
	}
}

func (w *NodeFrameHeader) breadCrumbs(path []string) string {
	pth := ""
	for i := len(path) - 1; i >= 0; i-- {
		pth += path[i]
		if i > 0 {
			pth += " › "
		}
	}
	return pth
}

func (w *NodeFrameHeader) onOp(op rt.NodeOp, recursive bool) {
	node := w.frame.Node()
	if node == nil {
		return
	}

	node.LockTree("NodeFrameTools.onOp")
	defer node.UnLockTree()

	// Is this operation allowed?
	ops := node.AllowedManualOps(true, recursive)
	_, allowed := ops[op]
	if !allowed {
		ShowToast("Operation " + rt.OpName(op) + " is not allowed on this node.")
		return
	}

	_, err := node.PerformOp(op, true, recursive, true)
	if err != nil {
		ShowToast(err.Error())
	}
}

func (w *NodeFrameHeader) sendSignal(name string, recursive bool) {
	node := w.frame.Node()
	if node == nil {
		return
	}

	node.LockTree("NodeFrameTools.onOp")
	defer node.UnLockTree()

	if !recursive && node.Client == nil {
		return
	}

	// Is this operation allowed?
	ops := node.AllowedManualOps(true, recursive)
	_, allowed := ops[rt.OpSignal]
	if !allowed {
		ShowToast("Operation " + rt.OpName(rt.OpSignal) + " is not allowed on this node.")
		return
	}

	w.sendSignalNode(node, name, recursive)
}

func (w *NodeFrameHeader) sendSignalNode(node *rt.Node, name string, recursive bool) {
	if node.Client != nil {
		ps := node.Client.LastState()
		if ps.Alive() {
			// TODO: add these to node log!
			ShowToast(fmt.Sprintf("Sending signal %s to %s", name, node.Id))
			err := node.Client.SendSignal(context.TODO(), name, true)
			if err != nil {
				ShowToast("Failed to send signal: " + err.Error())
			}
		}
	}
	if recursive {
		for _, sub := range node.Nodes {
			w.sendSignalNode(sub, name, true)
		}
	}
}
