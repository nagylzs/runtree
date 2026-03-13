package gtkui

import (
	"fmt"

	glib2 "github.com/diamondburned/gotk4/pkg/glib/v2"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
	"github.com/nagylzs/runtree/internal/rt"
	"github.com/nagylzs/set"
)

// NodeFrameGrid can display node-related values in a grid
type NodeFrameGrid struct {
	*gtk.Grid
	frame       *NodeFrame
	tabLabel    *gtk.Label
	rowCount    int                      // number of rows in the grid
	rowProps    map[int]*set.Set[string] // which row contains which props
	propLabels  map[string]*gtk.Label    // labels showing property names
	propEntries map[string]*gtk.Entry    // entries showing property values
	copyButtons map[string]*gtk.Button   // copy buttons
	updateGui   func()
}

func NewNodeFrameGrid(tabLabel string, updateGUI func()) *NodeFrameGrid {
	grid := gtk.NewGrid()
	grid.SetHExpand(true)
	grid.SetVExpand(false)
	grid.SetRowSpacing(5)
	grid.SetColumnSpacing(5)

	propNames := make(map[string]*gtk.Label)
	propEntries := make(map[string]*gtk.Entry)
	copyButtons := make(map[string]*gtk.Button)
	rowProps := make(map[int]*set.Set[string])

	w := &NodeFrameGrid{
		Grid:        grid,
		tabLabel:    gtk.NewLabel(tabLabel),
		rowProps:    rowProps,
		rowCount:    0,
		propLabels:  propNames,
		propEntries: propEntries,
		copyButtons: copyButtons,
		updateGui:   updateGUI,
	}
	w.frame = NewNodeFrame(updateGUI)

	return w
}

func (w *NodeFrameGrid) addStringProp(id string, name string, tooltip string, col int, width int) {
	lblName := gtk.NewLabel("")
	lblName.SetMarkup(fmt.Sprintf("<b>%s</b>", glib2.MarkupEscapeText(name)))
	lblName.SetHExpand(false)
	lblName.SetHAlign(gtk.AlignStart)

	eValue := gtk.NewEntry()
	eValue.SetSensitive(false)
	eValue.SetHExpand(true)
	if tooltip != "" {
		eValue.SetTooltipText(tooltip)
	}

	btnCpy := gtk.NewButton()
	btnCpy.SetChild(gtk.NewButtonFromIconName("copy"))
	btnCpy.SetObjectProperty("id", id)
	btnCpy.ConnectClicked(func() {
		eValue.Clipboard().SetText(eValue.Text())
		ShowToast("Copied to clipboard")
	})
	btnCpy.SetHExpand(false)

	w.Grid.Attach(lblName, col, w.rowCount, 1, 1)
	w.Grid.Attach(btnCpy, col+1, w.rowCount, 1, 1)
	w.Grid.Attach(eValue, col+2, w.rowCount, width, 1)
	w.propLabels[id] = lblName
	w.propEntries[id] = eValue
	w.copyButtons[id] = btnCpy

	props, ok := w.rowProps[w.rowCount]
	if !ok {
		props = set.NewSet[string]()
		w.rowProps[w.rowCount] = props
	}
	props.Add(id)

}

func (w *NodeFrameGrid) setPropState(id string, state string, hasStates *set.Set[string]) {
	pe, ok := w.propEntries[id]
	if !ok {
		panic("No prop entry for " + id)
	}
	pe.RemoveCSSClass("status-success")
	pe.RemoveCSSClass("status-waiting")
	pe.RemoveCSSClass("status-failed")
	if state != "" {
		pe.AddCSSClass("status-" + state)
		hasStates.Add(state)
	}
}

func (w *NodeFrameGrid) updateTabState(hasStates *set.Set[string]) {
	w.tabLabel.RemoveCSSClass("status-success")
	w.tabLabel.RemoveCSSClass("status-failed")
	w.tabLabel.RemoveCSSClass("status-waiting")
	if hasStates.Contains("failed") {
		w.tabLabel.AddCSSClass("status-failed")
	} else if hasStates.Contains("waiting") {
		w.tabLabel.AddCSSClass("status-waiting")
	} else if hasStates.Contains("success") {
		w.tabLabel.AddCSSClass("status-success")
	}
}

func (w *NodeFrameGrid) nextRow() {
	w.rowCount++
}

func (w *NodeFrameGrid) TabLabel() gtk.Widgetter {
	return w.tabLabel
}

func (w *NodeFrameGrid) SetNode(node *rt.Node) {
	w.frame.SetNode(node)
}

func (w *NodeFrameGrid) SetUpdateGui(updateGui func()) {
	w.frame.SetUpdateGUI(updateGui)
}
