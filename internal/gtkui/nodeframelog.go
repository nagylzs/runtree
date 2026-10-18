package gtkui

import (
	"fmt"
	"log/slog"
	"strings"

	glib2 "github.com/diamondburned/gotk4/pkg/glib/v2"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
	"github.com/nagylzs/runtree/internal/rt"
)

// NodeFrameLog shows the events of the selected node and of the nodes below it (the root shows the events of the
// tree itself too). It follows the end of the log, unless it is scrolled up: then a button in the top right corner
// scrolls to the end, and it follows the end again.
type NodeFrameLog struct {
	*gtk.Overlay
	view     *gtk.TextView
	scroll   *gtk.ScrolledWindow
	toEnd    *gtk.Button
	frame    *NodeFrame
	tabLabel *gtk.Label
	end      *gtk.TextMark

	shown     *rt.Node // the node whose events are shown; main gtk loop only, like the fields below
	seq       uint64   // the last entry of the event log that was checked
	count     int      // the number of shown entries
	lastDate  string   // of the last shown entry
	following bool     // the view is at the end of the log, new entries scroll it
}

func NewNodeFrameLog() *NodeFrameLog {
	d := &NodeFrameLog{
		Overlay:   gtk.NewOverlay(),
		view:      gtk.NewTextView(),
		scroll:    gtk.NewScrolledWindow(),
		tabLabel:  gtk.NewLabel("Log"),
		following: true,
	}
	d.view.SetEditable(false)
	d.view.SetCursorVisible(false)
	d.view.SetMonospace(true)
	d.view.SetWrapMode(gtk.WrapWordChar)
	buffer := d.view.Buffer()
	d.end = buffer.CreateMark("end", buffer.EndIter(), false)
	d.scroll.SetHExpand(true)
	d.scroll.SetVExpand(true)
	d.scroll.SetChild(d.view)
	d.Overlay.SetChild(d.scroll)

	content := gtk.NewBox(gtk.OrientationHorizontal, 5)
	content.Append(gtk.NewImageFromIconName("go-bottom-symbolic"))
	content.Append(gtk.NewLabel("End"))
	d.toEnd = gtk.NewButton()
	d.toEnd.SetChild(content)
	d.toEnd.SetTooltipText("Scroll to the end of the log, and follow it")
	d.toEnd.AddCSSClass("osd")
	d.toEnd.SetHAlign(gtk.AlignEnd)
	d.toEnd.SetVAlign(gtk.AlignStart)
	d.toEnd.SetMarginTop(8)
	d.toEnd.SetMarginEnd(20) // not on the scrollbar
	d.toEnd.SetVisible(false)
	d.toEnd.ConnectClicked(d.scrollToEnd)
	d.Overlay.AddOverlay(d.toEnd)

	// Only scrolling changes the value: a new entry changes the upper bound, then it is scrolled to the end when the
	// view follows the log.
	d.scroll.VAdjustment().ConnectValueChanged(func() {
		d.setFollowing(d.atEnd())
	})
	d.frame = NewNodeFrame(d.updateGUI)
	return d
}

// atEnd tells if the view is scrolled to the end
func (d *NodeFrameLog) atEnd() bool {
	adj := d.scroll.VAdjustment()
	return adj.Value()+adj.PageSize() >= adj.Upper()-1
}

func (d *NodeFrameLog) setFollowing(following bool) {
	d.following = following
	d.toEnd.SetVisible(!following)
}

func (d *NodeFrameLog) scrollToEnd() {
	adj := d.scroll.VAdjustment()
	adj.SetValue(adj.Upper() - adj.PageSize())
	d.setFollowing(true)
}

func (d *NodeFrameLog) TabLabel() gtk.Widgetter {
	return d.tabLabel
}

func (d *NodeFrameLog) SetNode(node *rt.Node) {
	d.frame.SetNode(node)
}

func (d *NodeFrameLog) updateGUI() {
	node := d.frame.Node()
	buffer := d.view.Buffer()
	if node != d.shown {
		d.shown, d.seq, d.count, d.lastDate = node, 0, 0, ""
		buffer.SetText("")
		d.setFollowing(true)
		d.updateTabLabel()
	}
	if node == nil {
		return
	}

	// the event log has its own lock, it is read before locking the tree
	entries := node.Tree.EventLog().Since(d.seq)
	if len(entries) == 0 {
		return
	}
	d.seq = entries[len(entries)-1].Seq

	var sb strings.Builder
	added := 0
	node.RLockTree("NodeFrameLog.updateGUI")
	for _, e := range entries {
		if !shownIn(e, node) {
			continue
		}
		d.writeEntry(&sb, e, node)
		added++
	}
	node.RUnlockTree()
	if added == 0 {
		return
	}

	buffer.InsertMarkup(buffer.EndIter(), sb.String())
	d.count += added
	d.updateTabLabel()
	if d.following {
		d.view.ScrollToMark(d.end, 0, false, 0, 1)
	}
}

// shownIn tells if the entry is shown in the log of the node: the events of the node and of the nodes below it,
// and for the root node the events of the tree too
func shownIn(e rt.LogEntry, node *rt.Node) bool {
	if e.Node == nil {
		return node.Parent == nil
	}
	return e.Node == node || node.ParentOf(e.Node)
}

// writeEntry writes the markup of a line, it must be called with the tree locked (for the labels of the nodes)
func (d *NodeFrameLog) writeEntry(sb *strings.Builder, e rt.LogEntry, node *rt.Node) {
	if date := e.Time.Format("2006-01-02"); date != d.lastDate {
		if d.lastDate != "" {
			sb.WriteString("\n")
		}
		fmt.Fprintf(sb, "<span alpha=\"60%%\">%s</span>", date)
		d.lastDate = date
	}
	sb.WriteString("\n")
	fmt.Fprintf(sb, "<span alpha=\"60%%\">%s</span> ", e.Time.Format("15:04:05"))
	level := fmt.Sprintf("%-5s", e.Level.String())
	switch {
	case e.Level >= slog.LevelError:
		fmt.Fprintf(sb, "<span foreground=\"#dc322f\" weight=\"bold\">%s</span> ", level)
	case e.Level >= slog.LevelWarn:
		fmt.Fprintf(sb, "<span foreground=\"#cb4b16\" weight=\"bold\">%s</span> ", level)
	default:
		fmt.Fprintf(sb, "<span alpha=\"60%%\">%s</span> ", level)
	}
	if e.Node != nil && e.Node != node {
		fmt.Fprintf(sb, "<b>%s</b>: ", glib2.MarkupEscapeText(e.Node.DisplayLabel()))
	}
	sb.WriteString(glib2.MarkupEscapeText(e.Message))
}

func (d *NodeFrameLog) updateTabLabel() {
	if d.count == 0 {
		d.tabLabel.SetText("Log")
	} else {
		d.tabLabel.SetMarkup(fmt.Sprintf("<b>Log</b> (%d)", d.count))
	}
}
