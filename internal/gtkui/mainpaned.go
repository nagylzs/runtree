package gtkui

import (
	"fmt"

	"github.com/diamondburned/gotk4/pkg/core/glib"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
	vte "github.com/nagylzs/gotk4-vte"
	"github.com/nagylzs/runtree/internal/rt"
)

type MainPaned struct {
	gtk.Paned

	navigator   *NodeNavigator
	leftSide    *gtk.Box
	rightSide   *gtk.Paned
	toolBox     *NodeFrameTools
	termOverlay *gtk.Overlay
	termProbe   *vte.Terminal // invisible terminal over the whole terminal area, it tells the size of the area

	lastToggleButton *gtk.ToggleButton
	navModeButtons   map[NavMode]*gtk.ToggleButton
	navModeMenu      *gtk.MenuButton // shows the mode, its popover has navModeButtons
	cbRunOnly        *gtk.CheckButton
}

func NewMainPaned() *MainPaned {
	leftSide := gtk.NewBox(gtk.OrientationVertical, 5)

	// The navigation mode is chosen in the popover of a menu button, that shows the current one
	navModes := gtk.NewGrid()
	navModes.SetRowSpacing(5)
	navModes.SetColumnSpacing(5)
	navModes.SetColumnHomogeneous(true)
	const COLS = 2
	npRow, npCol := 0, 0
	navModeMenu := gtk.NewMenuButton()
	navModeMenu.SetAlwaysShowArrow(true)
	navModeMenu.SetHAlign(gtk.AlignStart)
	navModeMenu.SetTooltipText("Show the nodes in a tree, or the nodes in a status in a list")
	navModePopover := gtk.NewPopover()
	navModeMenu.SetPopover(navModePopover)
	navRow := gtk.NewBox(gtk.OrientationHorizontal, 5)
	navRow.Append(navModeMenu)
	leftSide.Append(navRow)

	navScroll := gtk.NewScrolledWindow()
	navScroll.SetHExpand(true)
	navScroll.SetVExpand(true)
	leftSide.Append(navScroll)

	var nav = NewNodeNavigator()
	nav.viewport = gtk.NewViewport(nil, nil)
	nav.viewport.SetChild(nav)
	navScroll.SetChild(nav.viewport)

	navModeMenu.SetLabel(NavModeName(nav.Mode()))

	cbRunOnly := gtk.NewCheckButton()
	cbRunOnly.SetLabel("Run nodes only")
	cbRunOnly.SetTooltipText("When checked, only Run nodes will be shown in the list")
	cbRunOnly.SetActive(nav.OnlyShowRunNodes())
	cbRunOnly.SetVisible(nav.Mode() != NavModeTree)
	cbRunOnly.ConnectToggled(func() {
		nav.SetOnlyShowRunNodes(cbRunOnly.Active())
	})

	navModeButtons := make(map[NavMode]*gtk.ToggleButton)

	addNavModeButton := func(toolTip string, navMode NavMode, group *gtk.ToggleButton) *gtk.ToggleButton {
		tb := gtk.NewToggleButton()
		tb.SetLabel(NavModeName(navMode))
		tb.SetTooltipText(toolTip)
		tb.SetGroup(group)
		if navMode == nav.Mode() {
			tb.SetActive(true)
		}
		navModes.Attach(tb, npCol, npRow, 1, 1)
		npCol++
		if npCol == COLS {
			npRow++
			npCol = 0
		}
		tb.ConnectClicked(func() {
			navModePopover.Popdown()
			nav.SetMode(navMode)
			cbRunOnly.SetVisible(navMode != NavModeTree)
			Application.pane.updateNavModeButtons()
		})
		navModeButtons[navMode] = tb
		return tb
	}
	tbTree := addNavModeButton("Show nodes in a tree", NavModeTree, nil)
	for _, nm := range AllNavModesWithStatus {
		st := NavModeToStatus(nm)
		addNavModeButton(fmt.Sprintf("Show %s nodes in a list", rt.StatusName(st)), nm, tbTree)
	}
	navRow.Append(cbRunOnly)
	navPopoverBox := gtk.NewBox(gtk.OrientationVertical, 8)
	navPopoverBox.Append(navModes)
	navModePopover.SetChild(scrollable(navPopoverBox))

	// the right side is a vertical pane, toolbox on the top
	rightSide := gtk.NewPaned(gtk.OrientationVertical)
	tools := NewToolsWidget()
	// Without scrolling, the minimum width of the tools would push the right side (and the terminal) out of the
	// window when the window is narrow.
	toolsScroll := gtk.NewScrolledWindow()
	toolsScroll.SetPolicy(gtk.PolicyAutomatic, gtk.PolicyAutomatic)
	toolsScroll.SetChild(tools)
	tools.header.SetAvailableWidth(toolsScroll.Width)
	termOverlay := gtk.NewOverlay()
	// The probe is an invisible terminal on top of the terminal area. It is always allocated to the whole area, so
	// its column and row count is the size of the area, even when no terminal is displayed. This is used to resize
	// the terminals that are not displayed, so they are already correct when they are displayed.
	termProbe, err := vte.TerminalNew()
	if err != nil {
		panic(fmt.Sprintf("could not create terminal: %s", err))
	}
	termProbe.SetOpacity(0)
	termProbe.SetCanTarget(false)
	termProbe.SetFocusable(false)
	termOverlay.AddOverlay(termProbe)
	rightSide.SetStartChild(toolsScroll)
	rightSide.SetEndChild(termOverlay)
	setInitialPosition(rightSide, 600)
	// Thin separators have an invisible grab area that reaches into the terminal, so selecting text from the
	// first column or row would move the separator. Wide separators can only be grabbed where they are visible.
	rightSide.SetWideHandle(true)

	m := &MainPaned{
		Paned:          *gtk.NewPaned(gtk.OrientationHorizontal),
		navigator:      nav,
		leftSide:       leftSide,
		rightSide:      rightSide,
		toolBox:        tools,
		termOverlay:    termOverlay,
		termProbe:      termProbe,
		navModeButtons: navModeButtons,
		navModeMenu:    navModeMenu,
		cbRunOnly:      cbRunOnly,
	}
	m.SetStartChild(leftSide)
	m.SetEndChild(rightSide)
	setInitialPosition(&m.Paned, 700)
	m.SetWideHandle(true)

	return m
}

// setInitialPosition sets the position of the divider of a paned, but when the paned is first allocated, at most to
// the half of its size: the window may start smaller than its default size (e.g. tiled by the window manager), and
// later the two sides keep their proportions.
func setInitialPosition(p *gtk.Paned, pos int) {
	p.SetPosition(pos)
	var handle glib.SignalHandle
	handle = p.NotifyProperty("max-position", func() {
		maxPos, _ := p.ObjectProperty("max-position").(int)
		if maxPos <= 0 {
			return
		}
		p.HandlerDisconnect(handle)
		if pos > maxPos/2 {
			p.SetPosition(maxPos / 2)
		}
	})
}

// ShowInTree switches to the tree view mode, and selects the node there (see NodeNavigator.ShowNode). It must be
// called from the main gtk loop, with the tree unlocked.
func (m *MainPaned) ShowInTree(node *rt.Node) {
	m.navModeButtons[NavModeTree].SetActive(true)
	m.cbRunOnly.SetVisible(false)
	m.navigator.SetMode(NavModeTree)
	m.updateNavModeButtons()
	m.navigator.ShowNode(node)
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
		if cnt, ok := counts[nm]; ok {
			lbl = fmt.Sprintf("%s (%d)", lbl, cnt)
		}
		if tb.Label() != lbl { // a new label is laid out and drawn again
			tb.SetLabel(lbl)
		}
		if nm == m.navigator.Mode() && m.navModeMenu.Label() != lbl {
			m.navModeMenu.SetLabel(lbl)
		}
	}
}
