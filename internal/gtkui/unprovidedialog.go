package gtkui

import (
	"fmt"
	"strings"

	"github.com/diamondburned/gotk4/pkg/gtk/v4"
	"github.com/nagylzs/runtree/internal/rt"
)

// unprovideInfo is the displayed part of the plan of an operation that unprovides resources (rt.UnprovidePlan). It is
// collected while the tree is locked.
type unprovideInfo struct {
	title     string // of the dialog
	question  string
	button    string // the label of the button that performs the operation
	resources []unprovideResource
	running   []string // reset to waiting: labels of the running nodes, they will not be reset
}

type unprovideResource struct {
	name    string
	details string // provided by, required by
}

// planUnprovide returns the information for the unprovide dialog, or nil when the dialog is not needed (the operation
// would not unprovide anything, or it is not allowed).
func planUnprovide(node *rt.Node, op rt.NodeOp, recursive bool) *unprovideInfo {
	node.RLockTree("planUnprovide")
	defer node.RUnlockTree()

	if _, allowed := node.AllowedManualOps(true, recursive)[op]; !allowed {
		return nil
	}
	plan := node.PlanOp(op, true, recursive)
	if len(plan.Unprovide) == 0 {
		return nil
	}
	info := &unprovideInfo{}
	const waitText = "Nodes that require an unprovided resource will wait until it is provided again."
	switch op {
	case rt.OpRun:
		info.title, info.button = "Run", "Run"
		if rt.StatusFinished(node.Status) {
			info.title, info.button = "Re-run", "Re-run"
		}
		info.question = fmt.Sprintf("%d resource(s) provided by this node or its parents will not be provided "+
			"while it runs. "+waitText, len(plan.Unprovide))
	default:
		info.title, info.button = "Reset to waiting", "Reset"
		info.question = fmt.Sprintf("The nodes that will be reset have provided %d resource(s). "+waitText,
			len(plan.Unprovide))
	}
	for _, r := range plan.Unprovide {
		details := "provided by " + nodeLabels(r.ProvidedBy)
		if len(r.RequiredBy) > 0 {
			details += ", required by " + nodeLabels(r.RequiredBy)
		}
		info.resources = append(info.resources, unprovideResource{name: r.Name, details: details})
	}
	for _, n := range plan.Running {
		info.running = append(info.running, nodeLabel(n))
	}
	return info
}

// nodeLabel returns a short label for the node
func nodeLabel(n *rt.Node) string {
	const maxLen = 40
	label := []rune(n.DisplayLabel())
	if len(label) > maxLen {
		return string(label[:maxLen-1]) + "…"
	}
	return string(label)
}

// nodeLabels returns the short labels of (at most 5) nodes
func nodeLabels(nodes []*rt.Node) string {
	const maxNodes = 5
	labels := make([]string, 0, maxNodes)
	for i, n := range nodes {
		if i == maxNodes {
			labels = append(labels, fmt.Sprintf("and %d more", len(nodes)-maxNodes))
			break
		}
		labels = append(labels, nodeLabel(n))
	}
	return strings.Join(labels, ", ")
}

// showUnprovideDialog asks which provided resources should be unprovided, and performs the operation (reset to
// waiting, run) on the node when confirmed. It must be called from the main gtk loop.
func showUnprovideDialog(node *rt.Node, op rt.NodeOp, recursive bool, info *unprovideInfo) {
	dialog := newDialogWindow(info.title)
	dialog.SetResizable(true)

	question := newWrappedLabel(info.question)

	rbAll := gtk.NewCheckButtonWithLabel("Unprovide all provided resources")
	rbSelect := gtk.NewCheckButtonWithLabel("Select resources to unprovide")
	rbSelect.SetGroup(rbAll)
	rbAll.SetActive(true)

	list := gtk.NewBox(gtk.OrientationVertical, 8)
	list.SetMarginStart(24)
	checks := make([]*gtk.CheckButton, len(info.resources))
	for i, r := range info.resources {
		check := gtk.NewCheckButtonWithLabel(r.name)
		check.SetActive(true)
		details := newWrappedLabel(r.details)
		details.SetMarginStart(28) // under the label of the check button
		details.AddCSSClass("dim-label")
		list.Append(check)
		list.Append(details)
		checks[i] = check
	}
	list.SetSensitive(false)
	rbSelect.ConnectToggled(func() {
		list.SetSensitive(rbSelect.Active())
	})
	scroll := gtk.NewScrolledWindow()
	scroll.SetPolicy(gtk.PolicyNever, gtk.PolicyAutomatic)
	scroll.SetPropagateNaturalHeight(true)
	scroll.SetMaxContentHeight(300)
	scroll.SetChild(list)

	btnCancel := gtk.NewButtonWithLabel("Cancel")
	btnPerform := gtk.NewButtonWithLabel(info.button)
	btnPerform.AddCSSClass("suggested-action")

	box := newDialogBox()
	box.Append(question)
	box.Append(rbAll)
	box.Append(rbSelect)
	box.Append(scroll)
	if len(info.running) > 0 {
		box.Append(newWrappedLabel("These nodes are running, they will not be reset: " +
			strings.Join(info.running, ", ")))
	}
	box.Append(newDialogButtons(btnCancel, btnPerform))
	dialog.SetChild(box)

	btnCancel.ConnectClicked(func() {
		dialog.Close()
	})
	btnPerform.ConnectClicked(func() {
		var args *rt.OpArgs // nil: unprovide all
		if rbSelect.Active() {
			names := make([]string, 0, len(checks))
			for i, check := range checks {
				if check.Active() {
					names = append(names, info.resources[i].name)
				}
			}
			args = &rt.OpArgs{Unprovide: names}
		}
		dialog.Close()
		// The tree may have changed since the dialog was opened, PerformOp checks everything again.
		performOp(node, op, recursive, args)
	})

	// Cancel is the default, so that an accidental Enter does not do anything.
	dialog.SetDefaultWidget(btnCancel)
	dialog.Present()
	btnCancel.GrabFocus()
}
