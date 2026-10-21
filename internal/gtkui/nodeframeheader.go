package gtkui

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	glib2 "github.com/diamondburned/gotk4/pkg/glib/v2"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
	"github.com/diamondburned/gotk4/pkg/pango"
	"github.com/nagylzs/runtree/internal/rt"
	"github.com/nagylzs/runtree/internal/signal"
)

// signalColumns is the number of columns of the signals in the popovers of the Signal buttons
const signalColumns = 3

// headerOps are the operations of the buttons of the header, in their order
var headerOps = []rt.NodeOp{rt.OpRun, rt.OpSignal, rt.OpFreeze, rt.OpThaw, rt.OpSkip, rt.OpSuccess, rt.OpFail,
	rt.OpResetToWaiting}

// opLabel is the text of the button of an operation; rerun is for Run on a finished node
func opLabel(op rt.NodeOp, rerun bool) string {
	switch {
	case op == rt.OpRun && rerun:
		return "Re-run"
	case op == rt.OpResetToWaiting:
		return "Reset"
	}
	return rt.OpName(op)
}

// opTooltip explains an operation on the tooltip of its button
func opTooltip(op rt.NodeOp, rerun, recursive bool) string {
	var s string
	switch op {
	case rt.OpRun:
		s = "Run: start it now"
		if rerun {
			s = "Re-run: start it again"
		}
	case rt.OpSignal:
		s = "Signal: send a signal to the process (e.g. SIGINT, like Ctrl+C)"
	case rt.OpFreeze:
		s = "Freeze: the scheduler does not start it until it is thawed"
	case rt.OpThaw:
		s = "Thaw: it is Waiting again"
	case rt.OpSkip:
		s = "Skip: finish it without running it"
	case rt.OpSuccess:
		s = "Success: the final status of a paused node is Success"
	case rt.OpFail:
		s = "Fail: the final status of a paused node is Failed"
	case rt.OpResetToWaiting:
		s = "Reset to waiting: the scheduler starts it again when it can"
	}
	if recursive {
		s += ", for all the nodes below"
	}
	return s
}

// ButtonMode tells how the operation buttons of the header are shown, see SetButtonMode
type ButtonMode int

const (
	ButtonsAuto  ButtonMode = iota // with icons and texts, only icons when they do not fit
	ButtonsText                    // with icons and texts
	ButtonsIcons                   // only icons (the tooltips tell the operations)
)

var buttonMode = ButtonsAuto

// SetButtonMode sets how the operation buttons are shown (runtree --buttons), before the GUI starts
func SetButtonMode(mode ButtonMode) {
	buttonMode = mode
}

// opButton is a button of an operation, with an icon and a label (hidden when only the icons are shown)
type opButton struct {
	op     rt.NodeOp
	widget *gtk.Widget // a gtk.Button, or a gtk.MenuButton for Signal
	icon   *opIconView
	label  *gtk.Label
}

// opButtonSpacing is the space between the icon and the label of an operation button
const opButtonSpacing = 6

// newOpButtonContent returns the child of an operation button: its icon and its label
func newOpButtonContent(icon opIcon, label string) (*gtk.Box, *opIconView, *gtk.Label) {
	bx := gtk.NewBox(gtk.OrientationHorizontal, opButtonSpacing)
	iv := newOpIconView(icon)
	iv.SetVAlign(gtk.AlignCenter)
	lbl := gtk.NewLabel(label)
	bx.Append(iv)
	bx.Append(lbl)
	return bx, iv, lbl
}

// update shows the button of the operation for the node
func (b *opButton) update(allowed, rerun, recursive bool) {
	b.widget.SetSensitive(allowed)
	b.icon.setIcon(opIconOf(b.op, rerun))
	setText(b.label, opLabel(b.op, rerun))
	if tip := opTooltip(b.op, rerun, recursive); b.widget.TooltipText() != tip {
		b.widget.SetTooltipText(tip)
	}
}

type NodeFrameHeader struct {
	*gtk.Box

	frame *NodeFrame

	title      *gtk.Label
	path       *gtk.Label // the nodes of the path are links, see showPathNode
	pathMarkup string     // set again only when it changes, so that a click on a link is not lost
	status     *gtk.Label
	typ        *gtk.Label

	opsRow     *gtk.Box
	opButtons  []*opButton // the operations of the node, in opsRow
	signalMenu *gtk.MenuButton
	allButton  *gtk.MenuButton
	allLabel   *gtk.Label
	allButtons []*opButton // the operations of the sub-nodes, in the popover of allButton
	allSignals *gtk.Grid   // the signals for the sub-nodes, in the popover of allButton
	iconMode   bool        // only the icons of opButtons are shown

	availableWidth func() int // the width that the header can use, see SetAvailableWidth
	layoutKey      string     // the node, its buttons and the available width when the mode was chosen

	prevStatus  rt.Status
	statusShown bool // the status class and tooltip are set (for prevStatus)
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
	path.SetXAlign(0)
	path.SetWrap(true)
	path.SetWrapMode(pango.WrapWordChar)
	path.ConnectActivateLink(showPathNode)

	typ := gtk.NewLabel("")
	typ.SetVAlign(gtk.AlignCenter)
	typ.AddCSSClass("type-label")

	status := gtk.NewLabel("")
	status.SetVAlign(gtk.AlignCenter)
	status.SetXAlign(0)
	status.SetWrap(true)
	status.SetWrapMode(pango.WrapWordChar)
	status.AddCSSClass("status-label")

	// the type and the status are on their own row: the buttons do not move when the status changes
	statusRow := gtk.NewBox(gtk.OrientationHorizontal, 5)
	statusRow.Append(typ)
	statusRow.Append(status)

	// the buttons of the node are linked (no space between them), All is after them
	opsRow := gtk.NewBox(gtk.OrientationHorizontal, 8)
	opsRow.SetHAlign(gtk.AlignStart)
	opsGroup := gtk.NewBox(gtk.OrientationHorizontal, 0)
	opsGroup.AddCSSClass("linked")
	opsRow.Append(opsGroup)

	bx := gtk.NewBox(gtk.OrientationVertical, 5)
	bx.SetMarginStart(5)
	bx.SetMarginEnd(5)
	bx.Append(title)
	bx.Append(path)
	bx.Append(statusRow)
	bx.Append(opsRow)
	bx.SetHExpand(true)
	bx.SetVExpand(true)

	h := &NodeFrameHeader{
		Box:    bx,
		title:  title,
		path:   path,
		typ:    typ,
		status: status,
		opsRow: opsRow,
	}
	h.frame = NewNodeFrame(h.updateGUI)

	// the buttons of the node
	for _, op := range headerOps {
		content, icon, label := newOpButtonContent(opIconOf(op, false), opLabel(op, false))
		b := &opButton{op: op, icon: icon, label: label}
		if op == rt.OpSignal {
			mb := gtk.NewMenuButton()
			mb.SetChild(content)
			mb.SetAlwaysShowArrow(true)
			mb.SetPopover(h.newSignalPopover(false))
			h.signalMenu = mb
			b.widget = gtk.BaseWidget(mb)
			opsGroup.Append(mb)
		} else {
			btn := gtk.NewButton()
			btn.SetChild(content)
			btn.ConnectClicked(func() {
				h.onOp(op, false)
			})
			b.widget = gtk.BaseWidget(btn)
			opsGroup.Append(btn)
		}
		b.widget.AddCSSClass("op-button")
		b.widget.SetSensitive(false)
		h.opButtons = append(h.opButtons, b)
	}

	// All: the operations of the sub-nodes (seq/par)
	content, _, allLabel := newOpButtonContent(iconAll, "All")
	h.allLabel = allLabel
	h.allButton = gtk.NewMenuButton()
	h.allButton.SetChild(content)
	h.allButton.SetAlwaysShowArrow(true)
	h.allButton.SetTooltipText("The operations of all the nodes below this one")
	h.allButton.SetPopover(h.newAllPopover())
	h.allButton.AddCSSClass("op-button")
	h.allButton.SetVisible(false) // for seq/par nodes
	opsRow.Append(h.allButton)

	return h
}

// newAllPopover returns the popover of the All button: the operations of the sub-nodes, and their signals
func (w *NodeFrameHeader) newAllPopover() *gtk.Popover {
	popover := gtk.NewPopover()
	grid := gtk.NewGrid()
	grid.SetRowSpacing(5)
	grid.SetColumnSpacing(5)
	grid.SetColumnHomogeneous(true)
	i := 0
	for _, op := range headerOps {
		if op == rt.OpSignal {
			continue // below
		}
		content, icon, label := newOpButtonContent(opIconOf(op, false), opLabel(op, false))
		btn := gtk.NewButton()
		btn.SetChild(content)
		btn.ConnectClicked(func() {
			popover.Popdown()
			w.onOp(op, true)
		})
		b := &opButton{op: op, widget: gtk.BaseWidget(btn), icon: icon, label: label}
		b.widget.SetSensitive(false)
		w.allButtons = append(w.allButtons, b)
		grid.Attach(btn, i%signalColumns, i/signalColumns, 1, 1)
		i++
	}

	signals := gtk.NewLabel("Signal all:")
	signals.SetXAlign(0)
	signals.AddCSSClass("popover-heading")

	bx := gtk.NewBox(gtk.OrientationVertical, 8)
	bx.SetMarginStart(5)
	bx.SetMarginEnd(5)
	bx.Append(grid)
	bx.Append(gtk.NewSeparator(gtk.OrientationHorizontal))
	bx.Append(signals)
	w.allSignals = w.newSignalGrid(popover, true)
	bx.Append(w.allSignals)
	popover.SetChild(scrollable(bx))
	return popover
}

// newSignalPopover returns the popover of the Signal button
func (w *NodeFrameHeader) newSignalPopover(recursive bool) *gtk.Popover {
	popover := gtk.NewPopover()
	grid := w.newSignalGrid(popover, recursive)
	grid.SetMarginStart(5)
	grid.SetMarginEnd(5)
	popover.SetChild(scrollable(grid))
	return popover
}

// newSignalGrid returns the buttons of the signals, in a grid. A click sends the signal, and closes the popover.
func (w *NodeFrameHeader) newSignalGrid(popover *gtk.Popover, recursive bool) *gtk.Grid {
	grid := gtk.NewGrid()
	grid.SetRowSpacing(5)
	grid.SetColumnSpacing(5)
	grid.SetColumnHomogeneous(true)

	signames := make([]string, 0, len(signal.Signals))
	for k := range signal.Signals {
		signames = append(signames, k)
	}
	sort.Strings(signames)
	for i, name := range signames {
		btn := gtk.NewButtonWithLabel(name)
		btn.ConnectClicked(func() {
			popover.Popdown()
			w.sendSignal(name, recursive)
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
		grid.Attach(btn, i%signalColumns, i/signalColumns, 1, 1)
	}
	return grid
}

// scrollable puts a widget into a scrolled window that is as large as the widget, but can be shrunk: GTK closes a
// popover that does not fit on the screen, a scrolled one is shrunk instead
func scrollable(child gtk.Widgetter) *gtk.ScrolledWindow {
	scroll := gtk.NewScrolledWindow()
	scroll.SetPolicy(gtk.PolicyNever, gtk.PolicyAutomatic)
	scroll.SetPropagateNaturalWidth(true)
	scroll.SetPropagateNaturalHeight(true)
	scroll.SetChild(child)
	return scroll
}

// SetAvailableWidth sets the function that tells the width that the header can use (the width of the scrolled window
// around it), see showIcons
func (w *NodeFrameHeader) SetAvailableWidth(f func() int) {
	w.availableWidth = f
}

func (w *NodeFrameHeader) SetNode(node *rt.Node) {
	w.frame.SetNode(node)
}

func (w *NodeFrameHeader) updateGUI() {
	node := w.frame.Node()

	if node == nil {
		return
	}

	var path []*rt.Node
	node.RLockTree("NodeFrameTools.UpdateGUI")
	defer node.RUnlockTree()

	newStatus := node.Status
	prevStatus := w.prevStatus
	w.prevStatus = newStatus
	displayLabel := node.DisplayLabel()
	for n := node; n != nil; n = n.Parent {
		path = append(path, n)
	}
	st := rt.StatusName(newStatus)
	if node.TimedOut {
		st += ", timed out"
	}
	if node.SchedulerMessage != "" {
		st += ", " + node.SchedulerMessage
	}
	if node.HasChild() && !rt.StatusFinished(newStatus) {
		if failed := node.FailedRunNodes(true); failed > 0 {
			st += fmt.Sprintf(", %d failed", failed)
		}
	}
	typ := rt.TypeName(node.Type)

	setText(w.title, displayLabel)
	if markup := "<b>Path:</b> " + breadCrumbs(path) + " Id=" + node.Id; markup != w.pathMarkup {
		w.pathMarkup = markup
		w.path.SetMarkup(markup)
	}
	setText(w.typ, typ)

	// only the changes are set, see nodeViewState
	if newStatus != prevStatus || !w.statusShown {
		w.status.RemoveCSSClass("status-" + strings.ToLower(rt.StatusName(prevStatus)))
		w.status.AddCSSClass("status-" + strings.ToLower(rt.StatusName(newStatus)))
		w.status.SetTooltipText(rt.StatusDescription(newStatus))
		w.statusShown = true
	}
	setText(w.status, st)

	rerun := rt.StatusFinished(newStatus)
	ops := node.AllowedManualOps(true, false)
	for _, b := range w.opButtons {
		_, ok := ops[b.op]
		b.update(ok, rerun, false)
	}
	hasChild := node.HasChild()
	if w.allButton.Visible() != hasChild {
		w.allButton.SetVisible(hasChild)
	}
	if hasChild {
		ops = node.AllowedManualOps(true, true)
		for _, b := range w.allButtons {
			_, ok := ops[b.op]
			b.update(ok, rerun, true)
		}
		_, canSignal := ops[rt.OpSignal]
		w.allSignals.SetSensitive(canSignal)
		w.allButton.SetSensitive(len(ops) > 0)
	}
	w.chooseIconMode(fmt.Sprintf("%s %v %v", node.Id, hasChild, rerun))
}

// chooseIconMode shows only the icons of the buttons, when buttonMode says so, or when the texts do not fit
// (ButtonsAuto). The width of the texts is measured when the node, its buttons (key) or the available width changed:
// the texts are shown for the measurement, then hidden if needed (before the window is drawn again).
func (w *NodeFrameHeader) chooseIconMode(key string) {
	if buttonMode != ButtonsAuto {
		w.setIconMode(buttonMode == ButtonsIcons)
		return
	}
	available := w.Width()
	if w.availableWidth != nil {
		available = w.availableWidth()
	}
	if available <= 0 {
		return // not allocated yet
	}
	key += " " + strconv.Itoa(available)
	if key == w.layoutKey {
		return
	}
	w.layoutKey = key
	w.setIconMode(false)
	_, width, _, _ := w.opsRow.Measure(gtk.OrientationHorizontal, -1)
	w.setIconMode(width+w.MarginStart()+w.MarginEnd() > available)
}

// setIconMode shows only the icons of the operation buttons, or their texts too
func (w *NodeFrameHeader) setIconMode(icons bool) {
	if icons == w.iconMode {
		return
	}
	w.iconMode = icons
	for _, b := range w.opButtons {
		b.label.SetVisible(!icons)
	}
	w.allLabel.SetVisible(!icons)
	// the arrows of the menu buttons only with the texts
	w.signalMenu.SetAlwaysShowArrow(!icons)
	w.allButton.SetAlwaysShowArrow(!icons)
}

// setText sets the text of the label when it is different
func setText(l *gtk.Label, text string) {
	if l.Text() != text {
		l.SetText(text)
	}
}

// pathLinkPrefix is the start of the links of the path, before the id of the node
const pathLinkPrefix = "node:"

// breadCrumbs returns the markup of the path of a node (from the node to the root): the title (or id) of each node is
// a link to the node
func breadCrumbs(path []*rt.Node) string {
	var b strings.Builder
	for i := len(path) - 1; i >= 0; i-- {
		n := path[i]
		label := n.Calculated.Title
		if label == "" {
			label = n.Id
		}
		fmt.Fprintf(&b, `<a href="%s%s" title="Show it in the tree">%s</a>`, pathLinkPrefix, n.Id,
			glib2.MarkupEscapeText(label))
		if i > 0 {
			b.WriteString(" › ")
		}
	}
	return b.String()
}

// showPathNode is called when a node of the path is clicked: it shows the node in the tree
func showPathNode(uri string) bool {
	defer recoverPanic("show the node of the path")
	id, ok := strings.CutPrefix(uri, pathLinkPrefix)
	if !ok {
		return false
	}
	if node, ok := RunTree.NodeById[id]; ok {
		Application.pane.ShowInTree(node)
	}
	return true
}

func (w *NodeFrameHeader) onOp(op rt.NodeOp, recursive bool) {
	defer recoverPanic("operation " + rt.OpName(op))
	node := w.frame.Node()
	if node == nil {
		return
	}

	// When the operation (reset to waiting, run) would unprovide resources, then the user can select them first.
	if info := planUnprovide(node, op, recursive); info != nil {
		showUnprovideDialog(node, op, recursive, info)
		return
	}
	performOp(node, op, recursive, nil)
}

// performOp performs a manual operation on the node, and shows the error, if any.
// It must be called from the main gtk loop.
func performOp(node *rt.Node, op rt.NodeOp, recursive bool, args *rt.OpArgs) {
	defer recoverPanic("operation " + rt.OpName(op))
	node.LockTree("performOp")
	defer node.UnLockTree()

	// Is this operation allowed?
	ops := node.AllowedManualOps(true, recursive)
	_, allowed := ops[op]
	if !allowed {
		ShowToast("Operation " + rt.OpName(op) + " is not allowed on this node.")
		return
	}

	_, err := node.PerformOp(op, true, recursive, true, args)
	if err != nil {
		ShowToast(err.Error())
	}
}

func (w *NodeFrameHeader) sendSignal(name string, recursive bool) {
	defer recoverPanic("sending signal " + name)
	node := w.frame.Node()
	if node == nil {
		return
	}

	node.LockTree("NodeFrameTools.onOp")
	defer node.UnLockTree()

	if !recursive && node.Client == nil {
		if node.Starting(true) {
			ShowToast(node.Id + " is still connecting to its runner")
		}
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
	sent, err := node.SendSignal(true, name)
	if err != nil {
		ShowToast("Failed to send signal: " + err.Error())
	} else if sent {
		ShowToast(fmt.Sprintf("Sending signal %s to %s", name, node.Id))
	}
	if recursive {
		for _, sub := range node.Nodes {
			w.sendSignalNode(sub, name, true)
		}
	}
}
