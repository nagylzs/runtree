package gtkui

import (
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
	"github.com/nagylzs/runtree/internal/rt"
)

type NodeFrameTools struct {
	*gtk.Box

	header      *NodeFrameHeader
	properties  *NodeFrameProperites
	description *NodeFrameDescription
	log         *NodeFrameLog
	vars        *NodeFrameVars
	envs        *NodeFrameEnvs
}

func NewToolsWidget() *NodeFrameTools {
	header := NewHeaderFrame()
	header.SetHExpand(true)
	header.SetVExpand(false)

	nb := gtk.NewNotebook()
	nb.SetScrollable(true) // the tabs do not make the right side wider
	nb.SetHExpand(true)
	nb.SetVExpand(true)

	addScrolledTab := func(f NodeFrameWithLabel) *gtk.ScrolledWindow {
		sc := gtk.NewScrolledWindow()
		sc.SetHExpand(true)
		sc.SetVExpand(true)
		sc.SetChild(f)
		nb.AppendPage(sc, f.TabLabel())
		return sc
	}

	// the properties (with the dependencies) wrap their values, they do not scroll horizontally
	properties := NewNodeFrameProperties()
	addScrolledTab(properties).SetPolicy(gtk.PolicyNever, gtk.PolicyAutomatic)

	log := NewNodeFrameLog() // it has its own scrolled window
	nb.AppendPage(log, log.TabLabel())

	description := NewNodeFrameDescription()
	addScrolledTab(description)

	vars := NewNodeFrameVars()
	addScrolledTab(vars)

	envs := NewNodeFrameEnvs()
	addScrolledTab(envs)

	bx := gtk.NewBox(gtk.OrientationVertical, 5)
	bx.SetMarginStart(5)
	bx.SetMarginEnd(5)
	bx.Append(header)
	bx.Append(nb)
	bx.SetHExpand(true)
	bx.SetVExpand(true)

	return &NodeFrameTools{
		Box:         bx,
		header:      header,
		properties:  properties,
		log:         log,
		description: description,
		vars:        vars,
		envs:        envs,
	}
}

func (w *NodeFrameTools) SetNode(node *rt.Node) {
	w.header.SetNode(node)
	w.properties.SetNode(node)
	w.description.SetNode(node)
	w.log.SetNode(node)
	w.vars.SetNode(node)
	w.envs.SetNode(node)
}
