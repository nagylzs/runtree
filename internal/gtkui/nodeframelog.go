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
// tree itself too). It follows the end of the log, unless it is scrolled up.
type NodeFrameLog struct {
	*gtk.TextView
	frame    *NodeFrame
	tabLabel *gtk.Label
	end      *gtk.TextMark

	shown    *rt.Node // the node whose events are shown; main gtk loop only, like the fields below
	seq      uint64   // the last entry of the event log that was checked
	count    int      // the number of shown entries
	lastDate string   // of the last shown entry
}

func NewNodeFrameLog() *NodeFrameLog {
	d := &NodeFrameLog{
		TextView: gtk.NewTextView(),
		tabLabel: gtk.NewLabel("Log"),
	}
	d.TextView.SetEditable(false)
	d.TextView.SetCursorVisible(false)
	d.TextView.SetMonospace(true)
	d.TextView.SetWrapMode(gtk.WrapWordChar)
	buffer := d.TextView.Buffer()
	d.end = buffer.CreateMark("end", buffer.EndIter(), false)
	d.frame = NewNodeFrame(d.updateGUI)
	return d
}

func (d *NodeFrameLog) TabLabel() gtk.Widgetter {
	return d.tabLabel
}

func (d *NodeFrameLog) SetNode(node *rt.Node) {
	d.frame.SetNode(node)
}

func (d *NodeFrameLog) updateGUI() {
	node := d.frame.Node()
	buffer := d.TextView.Buffer()
	if node != d.shown {
		d.shown, d.seq, d.count, d.lastDate = node, 0, 0, ""
		buffer.SetText("")
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

	adj := d.TextView.VAdjustment()
	follow := adj == nil || adj.Value()+adj.PageSize() >= adj.Upper()-1
	buffer.InsertMarkup(buffer.EndIter(), sb.String())
	d.count += added
	d.updateTabLabel()
	if follow {
		d.TextView.ScrollToMark(d.end, 0, false, 0, 1)
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
