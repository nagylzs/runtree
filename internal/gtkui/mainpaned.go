package gtkui

import (
	"fmt"
	"strconv"
	"strings"

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
	attention        map[NavMode]*attentionButton // next to cbRunOnly, see attentionModes
}

// attentionModes are the lists of the nodes that may need the user: next to the view switcher, a button shows how
// many nodes they have (when they have any), and opens the list
var attentionModes = []NavMode{NavModeFailed, NavModePaused}

// attentionButton shows the number of the nodes of an attention mode, in the color of their status
type attentionButton struct {
	*gtk.Button
	count *gtk.Label
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
	// the rows are as wide as the navigator (see rowWidth)
	navScroll.HAdjustment().NotifyProperty("page-size", nav.WidthChanged)

	navModeMenu.SetLabel(NavModeName(nav.Mode()))

	cbRunOnly := gtk.NewCheckButton()
	cbRunOnly.SetLabel("Run nodes only")
	cbRunOnly.SetTooltipText("When checked, only Run nodes will be shown in the list")
	cbRunOnly.SetActive(nav.OnlyShowRunNodes())
	cbRunOnly.SetVisible(nav.Mode() != NavModeTree)
	cbRunOnly.ConnectToggled(func() {
		nav.SetOnlyShowRunNodes(cbRunOnly.Active())
		Application.pane.updateNavModeButtons() // the counts
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
			Application.pane.setNavMode(navMode)
		})
		navModeButtons[navMode] = tb
		return tb
	}
	tbTree := addNavModeButton(navModeTooltip(NavModeTree), NavModeTree, nil)
	for _, nm := range AllListNavModes {
		addNavModeButton(navModeTooltip(nm), nm, tbTree)
	}
	navRow.Append(cbRunOnly)
	attention := make(map[NavMode]*attentionButton)
	for _, nm := range attentionModes {
		icon := iconFail
		if nm == NavModePaused {
			icon = iconPause
		}
		content, _, count := newOpButtonContent(icon, "")
		content.SetSpacing(3)
		b := &attentionButton{Button: gtk.NewButton(), count: count}
		b.SetChild(content)
		b.SetVAlign(gtk.AlignCenter)
		b.AddCSSClass("attention")
		b.AddCSSClass("status-" + strings.ToLower(NavModeName(nm)))
		b.SetVisible(false) // until there are such nodes
		b.ConnectClicked(func() {
			Application.pane.setNavMode(nm)
		})
		attention[nm] = b
		navRow.Append(b)
	}
	settingsMenu := newSettingsMenu()
	settingsMenu.SetHExpand(true)
	settingsMenu.SetHAlign(gtk.AlignEnd)
	navRow.Append(settingsMenu)
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
		attention:      attention,
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
	m.setNavMode(NavModeTree)
	m.navigator.ShowNode(node)
}

// setNavMode switches the navigator to a view mode (main gtk loop, the tree unlocked)
func (m *MainPaned) setNavMode(mode NavMode) {
	m.navModeButtons[mode].SetActive(true)
	m.cbRunOnly.SetVisible(mode != NavModeTree)
	m.navigator.SetMode(mode)
	m.updateNavModeButtons()
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

	onlyRun := m.navigator.OnlyShowRunNodes()
	counts := navModeCounts(RunTree.Nodes, onlyRun)

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

	for nm, b := range m.attention {
		cnt := counts[nm]
		if b.Visible() != (cnt > 0) {
			b.SetVisible(cnt > 0)
		}
		setText(b.count, strconv.Itoa(cnt))
		if tip := attentionTooltip(nm, cnt, onlyRun); b.TooltipText() != tip {
			b.SetTooltipText(tip)
		}
	}
}

// navModeCounts returns the number of the nodes in the tree and in each list (with the tree locked)
func navModeCounts(nodes []*rt.Node, onlyRun bool) map[NavMode]int {
	counts := make(map[NavMode]int)
	counts[NavModeTree] = len(nodes)
	for _, node := range nodes {
		for _, nm := range AllListNavModes {
			if listShows(nm, node, onlyRun) {
				counts[nm]++
			}
		}
	}
	return counts
}

// attentionTooltip is the tooltip of the button of an attention mode, e.g. "3 failed run nodes: show them"
func attentionTooltip(mode NavMode, count int, onlyRun bool) string {
	what, them := "node", "it"
	if onlyRun {
		what = "run node"
	}
	if count != 1 {
		what, them = what+"s", "them"
	}
	return fmt.Sprintf("%d %s %s: show %s", count, strings.ToLower(NavModeName(mode)), what, them)
}
