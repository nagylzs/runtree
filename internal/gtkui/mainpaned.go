package gtkui

import (
	"fmt"

	"github.com/diamondburned/gotk4/pkg/gtk/v4"
	"github.com/nagylzs/runtree/internal/rt"
)

type MainPaned struct {
	gtk.Paned

	navigator   *NodeNavigator
	leftSide    *gtk.Box
	rightSide   *gtk.Paned
	toolBox     *NodeFrameTools
	termOverlay *gtk.Overlay

	lastToggleButton *gtk.ToggleButton
	navModeButtons   map[NavMode]*gtk.ToggleButton
}

func NewMainPaned() *MainPaned {
	leftSide := gtk.NewBox(gtk.OrientationVertical, 5)

	navProps := gtk.NewGrid()
	leftSide.Append(navProps)
	const COLS = 3
	npRow, npCol := 0, 0

	navScroll := gtk.NewScrolledWindow()
	navScroll.SetHExpand(true)
	navScroll.SetVExpand(true)
	leftSide.Append(navScroll)

	var nav = NewNodeNavigator()
	navScroll.SetChild(nav)

	cbRunOnly := gtk.NewCheckButton()
	cbRunOnly.SetLabel("Run nodes only")
	cbRunOnly.SetTooltipText("When checked, only Run nodes will be shown in the list")
	cbRunOnly.SetActive(nav.OnlyShowRunNodes())
	cbRunOnly.SetVisible(nav.Mode() != NavModeTree)
	cbRunOnly.ConnectToggled(func() {
		nav.SetOnlyShowRunNodes(cbRunOnly.Active())
	})

	navProps.Attach(gtk.NewLabel("Navigation mode/filter"), 0, 0, COLS, 1)
	npRow++

	navModeButtons := make(map[NavMode]*gtk.ToggleButton)

	addNavModeButton := func(toolTip string, navMode NavMode, group *gtk.ToggleButton) *gtk.ToggleButton {
		tb := gtk.NewToggleButton()
		tb.SetLabel(NavModeName(navMode))
		tb.SetTooltipText(toolTip)
		tb.SetGroup(group)
		if navMode == nav.Mode() {
			tb.SetActive(true)
		}
		navProps.Attach(tb, npCol, npRow, 1, 1)
		npCol++
		if npCol > COLS {
			npRow++
			npCol = 0
		}
		tb.ConnectClicked(func() {
			nav.SetMode(navMode)
			cbRunOnly.SetVisible(navMode != NavModeTree)
		})
		navModeButtons[navMode] = tb
		return tb
	}
	tbTree := addNavModeButton("Show nodes in a tree", NavModeTree, nil)
	for _, nm := range AllNavModesWithStatus {
		st := NavModeToStatus(nm)
		addNavModeButton(fmt.Sprintf("Show %s nodes in a list", rt.StatusName(st)), nm, tbTree)
	}
	npCol = 0
	npCol++
	navProps.Attach(cbRunOnly, npCol, npRow, COLS, 1)

	// the right side is a vertical pane, toolbox on the top
	rightSide := gtk.NewPaned(gtk.OrientationVertical)
	tools := NewToolsWidget()
	termOverlay := gtk.NewOverlay()
	rightSide.SetStartChild(tools)
	rightSide.SetEndChild(termOverlay)
	rightSide.SetPosition(600)

	m := &MainPaned{
		Paned:          *gtk.NewPaned(gtk.OrientationHorizontal),
		navigator:      nav,
		leftSide:       leftSide,
		rightSide:      rightSide,
		toolBox:        tools,
		termOverlay:    termOverlay,
		navModeButtons: navModeButtons,
	}
	m.SetStartChild(leftSide)
	m.SetEndChild(rightSide)
	m.SetPosition(700)

	return m
}

func (m *MainPaned) SetTerminalWidget(child gtk.Widgetter) {
	m.termOverlay.SetChild(child)
}

// UpdateGUI must be called from the main gtk loop
func (m *MainPaned) UpdateGUI() {
	m.updateNavModeButtons()
	m.navigator.EnqueueFullUpdate()
}

func (m *MainPaned) updateNavModeButtons() {
	RunTree.RLock("NewNodeNavigator")
	defer RunTree.RUnlock()

	counts := make(map[NavMode]int)
	counts[NavModeTree] = 0
	for i := range RunTree.Nodes {
		node := RunTree.Nodes[i]
		counts[NavModeTree] += 1
		if m.navigator.OnlyShowRunNodes() && node.Type != rt.TypeRun {
			continue
		}
		navMode := StatusToNavMode(node.Status)
		cnt, ok := counts[navMode]
		if !ok {
			counts[navMode] = 1
		} else {
			counts[navMode] = cnt + 1
		}
	}

	for nm, tb := range m.navModeButtons {
		lbl := NavModeName(nm)
		cnt, ok := counts[nm]
		if !ok {
			tb.SetLabel(lbl)
			continue
		}
		tb.SetLabel(fmt.Sprintf("%s (%d)", lbl, cnt))
	}
}
