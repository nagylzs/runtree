package gtkui

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/diamondburned/gotk4/pkg/gtk/v4"
	"github.com/diamondburned/gotk4/pkg/pango"
	"github.com/nagylzs/runtree/internal/rt"
	"github.com/nagylzs/runtree/internal/runner"
)

// The properties tab shows the node in one narrow column of names and values, in the order of what is looked for
// first: the result, the command and where it runs, the times, the settings that differ from the default, the
// dependencies and locks. "Show all" adds the details (the default settings, the raw values).

// property is a row of the properties tab
type property struct {
	id      string
	name    string
	tooltip string
	copy    bool // it has a copy button (long values)
	detail  bool // only shown with Show all
}

// properties are the rows of the properties tab, in their order
var properties = []property{
	{id: "result", name: "Result", tooltip: "The exit code of the process, or why it could not be started"},
	{id: "progress", name: "Progress", tooltip: "The run nodes below this node"},
	{id: "command", name: "Command", tooltip: "The command line of the process (quoted for a shell)", copy: true},
	{id: "runner", name: "Runner", tooltip: "Where the process runs", copy: true},
	{id: "cwd", name: "Folder", tooltip: "The working directory of the process (cwd)", copy: true},
	{id: "time", name: "Time", tooltip: "When it started and finished, and how long it ran"},
	{id: "pid", name: "PID", tooltip: "The process id on the runner"},
	{id: "settings", name: "Settings", tooltip: "The settings of the node that differ from the default"},
	{id: "provides", name: "Provides", tooltip: "The resources that this node provides when it succeeds"},
	{id: "requires", name: "Requires", tooltip: "The resources that this node needs before it starts"},
	{id: "missing", name: "Waiting for", tooltip: "The required resources that are not provided yet"},
	{id: "locks", name: "Locks", tooltip: "The locks that this node holds while it runs"},
	{id: "blocked", name: "Blocked by", tooltip: "The locks that other nodes hold, this node waits for them"},

	{id: "started", name: "Started", detail: true},
	{id: "finished", name: "Finished", detail: true},
	{id: "elapsed", name: "Elapsed", tooltip: "The time in running state", detail: true},
	{id: "proctime", name: "Process time", tooltip: "The sum of the running times of the processes below",
		detail: true},
	{id: "speedup", name: "Speedup", tooltip: "Process time / elapsed time", detail: true},
	{id: "processes", name: "Processes", tooltip: "The running processes below, and their limit (max_proc)",
		detail: true},
	{id: "on_error", name: "On error", tooltip: "on_error: the status when it fails, and the operation on its siblings",
		detail: true},
	{id: "children_on_error", name: "Children on error", tooltip: "children_on_error: the on_error of the sub-nodes",
		detail: true},
	{id: "success_exit_codes", name: "Success codes", tooltip: "success_exit_codes: the exit codes that mean success",
		detail: true},
	{id: "timeout", name: "Timeout", tooltip: "timeout: the longest time that it may run", detail: true},
	{id: "scheduler", name: "Scheduler", tooltip: "Why the scheduler does not start it, or that it is finished",
		detail: true},
}

// propertyValue is the value of a property for a node; state is "", "success", "failed" or "waiting"
type propertyValue struct {
	text, state string
}

// propertyRow shows a property
type propertyRow struct {
	name  *gtk.Label
	value *gtk.Label
	copy  *gtk.Button
}

type NodeFrameProperites struct {
	*gtk.Box
	frame    *NodeFrame
	tabLabel *gtk.Label
	rows     map[string]*propertyRow
	showAll  *gtk.CheckButton
}

func NewNodeFrameProperties() *NodeFrameProperites {
	grid := gtk.NewGrid()
	grid.SetRowSpacing(6)
	grid.SetColumnSpacing(12)
	grid.SetHExpand(true)

	p := &NodeFrameProperites{
		Box:      gtk.NewBox(gtk.OrientationVertical, 8),
		tabLabel: gtk.NewLabel("Properties"),
		rows:     make(map[string]*propertyRow, len(properties)),
	}
	p.SetMarginTop(6)
	p.SetMarginStart(6)
	p.SetMarginEnd(6)
	for i, prop := range properties {
		r := &propertyRow{name: gtk.NewLabel(prop.name), value: gtk.NewLabel("")}
		r.name.AddCSSClass("prop-name")
		r.name.SetWrap(true)
		r.name.SetMaxWidthChars(12) // the long names wrap, the values get the width
		r.name.SetXAlign(0)
		r.name.SetYAlign(0)
		r.name.SetVAlign(gtk.AlignStart)
		if prop.tooltip != "" {
			r.name.SetTooltipText(prop.tooltip)
		}
		r.value.SetXAlign(0)
		r.value.SetVAlign(gtk.AlignStart)
		r.value.SetHExpand(true)
		r.value.SetWrap(true)
		r.value.SetWrapMode(pango.WrapWordChar)
		r.value.SetSelectable(true) // and focusable: Ctrl+C copies the selection
		grid.Attach(r.name, 0, i, 1, 1)
		grid.Attach(r.value, 1, i, 1, 1)
		if prop.copy {
			r.copy = gtk.NewButton()
			r.copy.SetChild(newOpIconView(iconCopy))
			r.copy.SetTooltipText("Copy")
			r.copy.SetVAlign(gtk.AlignStart)
			r.copy.AddCSSClass("flat")
			value := r.value
			r.copy.ConnectClicked(func() {
				value.Clipboard().SetText(value.Text())
				ShowToast("Copied to clipboard")
			})
			grid.Attach(r.copy, 2, i, 1, 1)
		}
		p.rows[prop.id] = r
	}
	p.showAll = gtk.NewCheckButtonWithLabel("Show all")
	p.showAll.SetTooltipText("Show the details too: the default settings and the raw values")
	p.showAll.ConnectToggled(func() {
		p.updateGUI()
	})
	p.Append(grid)
	p.Append(p.showAll)
	p.setRowsVisible(false) // until a node is selected
	p.frame = NewNodeFrame(p.updateGUI)
	return p
}

// setRowsVisible shows or hides every row (and Show all)
func (p *NodeFrameProperites) setRowsVisible(visible bool) {
	for _, r := range p.rows {
		r.name.SetVisible(visible)
		r.value.SetVisible(visible)
		if r.copy != nil {
			r.copy.SetVisible(visible)
		}
	}
	p.showAll.SetVisible(visible)
}

func (p *NodeFrameProperites) TabLabel() gtk.Widgetter {
	return p.tabLabel
}

func (p *NodeFrameProperites) SetNode(node *rt.Node) {
	p.frame.SetNode(node)
}

func (p *NodeFrameProperites) updateGUI() {
	node := p.frame.Node()
	if node == nil {
		return
	}
	node.RLockTree("NodeFrameProperites.UpdateGUI")
	values := nodeProperties(node, time.Now())
	node.RUnlockTree()
	p.showAll.SetVisible(true)

	showAll := p.showAll.Active()
	tabState := ""
	for _, prop := range properties {
		r := p.rows[prop.id]
		v := values[prop.id]
		visible := v.text != "" && (showAll || !prop.detail)
		switch prop.id {
		case "settings":
			visible = visible && !showAll // the settings are in their own rows then
		case "runner":
			visible = visible && (showAll || v.text != runner.LocalAddress)
		}
		r.name.SetVisible(visible)
		r.value.SetVisible(visible)
		if r.copy != nil {
			r.copy.SetVisible(visible)
		}
		if !visible {
			continue
		}
		setText(r.value, v.text)
		setStateClass(r.value, v.state)
		// A colored value is a badge, not a bar. Only these: a wrapping label that does not fill its cell is made as
		// narrow as the height of the row allows (the row of a long name has two lines).
		halign := gtk.AlignFill
		if v.state != "" {
			halign = gtk.AlignStart
		}
		if r.value.HAlign() != halign {
			r.value.SetHAlign(halign)
		}
		switch {
		case v.state == "failed":
			tabState = "failed"
		case v.state == "waiting" && tabState != "failed":
			tabState = "waiting"
		case v.state == "success" && tabState == "":
			tabState = "success"
		}
	}
	setStateClass(p.tabLabel, tabState)
}

// setStateClass sets the status-<state> CSS class of a label (state "" removes it), when it changed
func setStateClass(l *gtk.Label, state string) {
	for _, s := range []string{"success", "failed", "waiting"} {
		has := l.HasCSSClass("status-" + s)
		if s == state && !has {
			l.AddCSSClass("status-" + s)
		} else if s != state && has {
			l.RemoveCSSClass("status-" + s)
		}
	}
}

// nodeProperties returns the values of the properties of a node (see properties); the empty ones are not shown. It
// must be called with the tree locked.
func nodeProperties(node *rt.Node, now time.Time) map[string]propertyValue {
	v := make(map[string]propertyValue, len(properties))
	set := func(id, text, state string) {
		v[id] = propertyValue{text, state}
	}
	isRun := node.Type == rt.TypeRun
	elapsed, procTime := node.CalcElapsed()

	if isRun {
		var ps runner.ProcState
		if node.Client != nil {
			ps = node.Client.LastState()
		}
		text, state := resultText(node.StartError, ps, node.Client != nil, node.ExitedSuccessfully(ps),
			node.TimedOut, node.Timeout)
		set("result", text, state)
		set("command", buildCommandLine(node.Calculated.Args), "")
		if ps.Pid != 0 {
			if ps.Alive() {
				set("pid", strconv.Itoa(ps.Pid), "")
			}
		}
	} else {
		set("progress", progressText(runNodeCounts(node), node.MaxProc), "")
		if procTime > 0 {
			set("proctime", fmtDuration(procTime), "")
			if elapsed > 0 {
				set("speedup", fmt.Sprintf("×%.1f", float64(procTime)/float64(elapsed)), "")
			}
		}
		maxProc := "no limit"
		if node.MaxProc >= 0 {
			maxProc = "at most " + strconv.Itoa(node.MaxProc)
		}
		set("processes", fmt.Sprintf("%d running, %s", node.NProc, maxProc), "")
		set("children_on_error", onErrorText(node.ChildrenOnError), "")
	}

	set("runner", node.Calculated.Runner, "")
	set("cwd", node.Calculated.CWD, "")
	set("time", timeText(node.Started, node.Finished, elapsed, rt.StatusActive(node.Status), now), "")
	set("settings", settingsText(node), "")

	set("provides", strings.Join(node.Calculated.Provides.List(), ", "), "")
	set("requires", strings.Join(node.Calculated.Requires.List(), ", "), "")
	set("missing", strings.Join(node.MissingRequired.List(), ", "), "waiting")
	set("locks", locksText(node.Calculated.RLocks.List(), node.Calculated.XLocks.List()), "")
	holders := func(locks []string) []string {
		for i, lock := range locks {
			if h := node.Tree.LockHolder(true, lock); h != nil {
				locks[i] += " (" + h.DisplayLabel() + ")"
			}
		}
		return locks
	}
	set("blocked", locksText(holders(node.CannotRAcquire.List()), holders(node.CannotXAcquire.List())), "waiting")

	set("started", fmtTime(node.Started), "")
	set("finished", fmtTime(node.Finished), "")
	if !node.Started.IsZero() {
		set("elapsed", fmtDuration(elapsed), "")
	}
	set("on_error", onErrorText(node.OnError), "")
	if isRun {
		set("success_exit_codes", intsText(node.SuccessExitCodes), "")
		timeout := "no limit"
		if node.Timeout > 0 {
			timeout = rt.DurationText(node.Timeout)
		}
		set("timeout", timeout, "")
	}
	set("scheduler", node.SchedulerMessage, "")

	for id, pv := range v {
		if pv.text == "" {
			delete(v, id)
		}
	}
	return v
}

// resultText tells how the process of a run node ended, and the state of the value ("success" or "failed"): its exit
// code, the signal that killed it, or why it could not be started (by the client or by the runner). It is "" while it
// runs, or before it was started.
func resultText(startError string, ps runner.ProcState, started, success, timedOut bool, timeout time.Duration) (
	string, string) {
	if startError != "" {
		return "could not start: " + startError, "failed"
	}
	if !started || ps.Alive() {
		return "", ""
	}
	if ps.Pid == 0 && ps.Error != "" {
		return "could not start: " + ps.Error, "failed"
	}
	text := ps.Error // e.g. "signal: interrupt"
	if ps.Exited && ps.ExitCode >= 0 && (ps.ExitCode != 0 || ps.Error == "") {
		text = "exit code " + strconv.Itoa(ps.ExitCode)
	}
	state := "success"
	if !success {
		state = "failed"
	}
	if timedOut {
		text += ", timed out after " + rt.DurationText(timeout)
		state = "failed"
	}
	return text, state
}

// runCounts are the numbers of run nodes below a node, by status
type runCounts struct {
	total, running, finished, failed int
}

// runNodeCounts counts the run nodes below a node
func runNodeCounts(node *rt.Node) runCounts {
	var c runCounts
	var count func(n *rt.Node)
	count = func(n *rt.Node) {
		for _, sub := range n.Nodes {
			if sub.Type == rt.TypeRun {
				c.total++
				switch {
				case sub.Status == rt.StatusRunning:
					c.running++
				case sub.Status == rt.StatusFailed:
					c.failed++
					c.finished++
				case rt.StatusFinished(sub.Status):
					c.finished++
				}
			}
			count(sub)
		}
	}
	count(node)
	return c
}

// progressText describes the run nodes below a seq/par node, e.g. "2 running (at most 3) · 4 of 7 finished · 1 failed"
func progressText(c runCounts, maxProc int) string {
	var parts []string
	if c.running > 0 {
		s := fmt.Sprintf("%d running", c.running)
		if maxProc >= 0 {
			s += fmt.Sprintf(" (at most %d)", maxProc)
		}
		parts = append(parts, s)
	}
	parts = append(parts, fmt.Sprintf("%d of %d finished", c.finished, c.total))
	if c.failed > 0 {
		parts = append(parts, fmt.Sprintf("%d failed", c.failed))
	}
	return strings.Join(parts, " · ")
}

// timeText tells when a node started and finished, and how long it ran: "10:53:17 → 10:54:49 (01:32)", or
// "since 10:53:17 (00:33)" while it runs
func timeText(started, finished time.Time, elapsed time.Duration, active bool, now time.Time) string {
	if started.IsZero() {
		return ""
	}
	if active || finished.IsZero() || finished.Before(started) {
		return fmt.Sprintf("since %s (%s)", fmtTimeAt(started, now), fmtDuration(elapsed))
	}
	return fmt.Sprintf("%s → %s (%s)", fmtTimeAt(started, now), fmtTimeAt(finished, now), fmtDuration(elapsed))
}

// settingsText lists the settings of a node that differ from the default, e.g. "timeout 3s · on_error paused"
func settingsText(node *rt.Node) string {
	var parts []string
	if node.Timeout > 0 {
		parts = append(parts, "timeout "+rt.DurationText(node.Timeout))
	}
	if node.OnError != rt.DefaultOnError {
		parts = append(parts, "on_error "+onErrorText(node.OnError))
	}
	if node.Type != rt.TypeRun {
		if node.ChildrenOnError != node.OnError {
			parts = append(parts, "children_on_error "+onErrorText(node.ChildrenOnError))
		}
		if node.MaxProc >= 0 {
			parts = append(parts, "max_proc "+strconv.Itoa(node.MaxProc))
		}
	}
	if !slices.Equal(node.SuccessExitCodes, []int{0}) {
		parts = append(parts, "success_exit_codes "+intsText(node.SuccessExitCodes))
	}
	return strings.Join(parts, " · ")
}

// onErrorText describes an on_error value, e.g. "failed, freeze the siblings"
func onErrorText(oe rt.OnError) string {
	s := strings.ToLower(rt.StatusName(oe.Status))
	if oe.Siblings != rt.OpNone {
		s += ", " + strings.ToLower(rt.OpName(oe.Siblings)) + " the siblings"
	}
	return s
}

// locksText lists read and exclusive locks: "read: a, b · exclusive: c"
func locksText(read, exclusive []string) string {
	var parts []string
	if len(read) > 0 {
		parts = append(parts, "read: "+strings.Join(read, ", "))
	}
	if len(exclusive) > 0 {
		parts = append(parts, "exclusive: "+strings.Join(exclusive, ", "))
	}
	return strings.Join(parts, " · ")
}

// intsText lists numbers: "0, 1, 2"
func intsText(ns []int) string {
	s := make([]string, len(ns))
	for i, n := range ns {
		s[i] = strconv.Itoa(n)
	}
	return strings.Join(s, ", ")
}

// fmtTime formats a time: only the time of day today, with the date on other days
func fmtTime(t time.Time) string {
	return fmtTimeAt(t, time.Now())
}

// fmtTimeAt is fmtTime at the time now
func fmtTimeAt(t, now time.Time) string {
	if t.IsZero() {
		return ""
	}
	if t.Year() == now.Year() && t.YearDay() == now.YearDay() {
		return t.Format("15:04:05") // Only time
	}
	return t.Format("2006-01-02 15:04:05") // Full date and time
}

func fmtDuration(d time.Duration) string {
	if d < time.Second {
		d = d.Round(time.Millisecond)
		return d.String()
	}

	d = d.Round(time.Second)
	h := d / time.Hour
	d -= h * time.Hour
	m := d / time.Minute
	d -= m * time.Minute
	s := d / time.Second

	if h > 0 {
		return fmt.Sprintf("%02d:%02d:%02d", h, m, s)
	} else {
		return fmt.Sprintf("%02d:%02d", m, s)
	}
}

// shellEscape escapes a single argument for safe use in a shell command.
func shellEscape(arg string) string {
	// If the argument is empty or contains special characters, wrap it in single quotes
	if arg == "" || strings.ContainsAny(arg, " \t\n\"'\\$`!&|<>*?[]{}()") {
		// Escape single quotes by closing the quote, inserting an escaped quote, and reopening
		arg = strings.ReplaceAll(arg, "'", "'\"'\"'")
		return "'" + arg + "'"
	}
	return arg
}

// buildCommandLine takes a slice of arguments and returns a shell-safe command line string.
func buildCommandLine(args []string) string {
	if len(args) == 0 {
		return ""
	}

	escaped := make([]string, len(args))
	for i, arg := range args {
		escaped[i] = shellEscape(arg)
	}
	return strings.Join(escaped, " ")
}
