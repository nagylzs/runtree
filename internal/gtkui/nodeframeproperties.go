package gtkui

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/nagylzs/runtree/internal/rt"
	"github.com/nagylzs/set"
)

type NodeFrameProperites struct {
	*NodeFrameGrid
}

func NewNodeFrameProperties() *NodeFrameProperites {
	g := NewNodeFrameGrid("", nil)
	p := &NodeFrameProperites{NodeFrameGrid: g}
	p.buildGui()
	p.NodeFrameGrid.SetUpdateGui(p.updateGUI)
	return p
}

func (p *NodeFrameProperites) buildGui() {
	p.addStringProp("PID", "PID",
		"Process id on the runner, only if the process was started", 0, 1)
	p.addStringProp("ExitCode", "ExitCode",
		"Last exit code of the process, only if the process has exited", 3, 1)
	p.addStringProp("Started", "Started",
		"When the process was started (the last time)", 6, 1)
	p.addStringProp("Finished", "Finished",
		"When the process has finished (the last time)", 9, 1)
	p.nextRow()
	p.addStringProp("StartError", "StartError", "Describes why the node could not be started.",
		0, 12)
	p.nextRow()

	p.addStringProp("Elapsed", "Elapsed",
		"Time elapsed in running state of this node, regardless of its type.", 0, 1)
	p.addStringProp("TotalProcElapsed", "TotalProcElapsed",
		"Total elapsed time in running processes in this subtree.", 3, 1)
	p.addStringProp("Speedup", "Speedup",
		"Speed up = TotalProcElapsed / Elapsed", 6, 1)
	p.nextRow()

	p.addStringProp("Runner", "Runner", "The (possibly remote) runner that runs the process", 0, 12)
	p.nextRow()
	p.addStringProp("CmdLine", "CmdLine", "Shell-escaped command line", 0, 12)
	p.nextRow()

	p.addStringProp("MaxProc", "MaxProc",
		"Maximum number of running processes allowed in this subtree, -1 means infinite.", 0, 1)
	p.addStringProp("NProc", "NProc",
		"Number of currently running processes in this subtree", 3, 1)
	p.addStringProp("OnError.Status", "OnError.Status",
		"What status to set when the node fails", 6, 1)
	p.addStringProp("OnError.Siblings", "OnError.Siblings",
		"What operation to perform on siblings when the node fails", 9, 1)
	p.nextRow()

}

func (p *NodeFrameProperites) updateGUI() {
	node := p.frame.Node()

	if node == nil {
		p.tabLabel.SetVExpand(false)
		return
	}
	p.tabLabel.SetVExpand(true)
	node.RLockTree("NodeFrameProperites.UpdateGUI")
	defer node.RUnlockTree()

	pid, exitcode := "", ""
	elapsed, totalElapsed := node.CalcElapsed()
	speedUp := ""
	if elapsed > 0 || (elapsed-totalElapsed).Abs() > time.Second {
		if node.Type == rt.TypeRun {
			speedUp = "1.0"
		} else {
			speedUp = fmt.Sprintf("%.2f", float64(totalElapsed)/float64(elapsed))
		}
	}

	c := node.Client
	if c != nil {
		ls := c.LastState()
		if ls.Pid != 0 {
			pid = strconv.Itoa(ls.Pid)
		}
		if ls.Exited {
			exitcode = strconv.Itoa(ls.ExitCode)
		}
	}

	usedProps := set.NewSet[string]()
	setStringProp := func(id string, value string) {
		pe, ok := p.propEntries[id]
		if !ok {
			panic("No prop entry for " + id)
		}
		cpy := p.copyButtons[id]
		pe.SetText(value)
		cpy.SetSensitive(value != "")
		if value != "" {
			usedProps.Add(id)
		}
	}

	hasStates := set.NewSet[string]()

	setStringProp("PID", pid)
	setStringProp("Started", fmtTime(node.Started))
	setStringProp("Finished", fmtTime(node.Finished))
	setStringProp("Elapsed", fmtDuration(elapsed))
	setStringProp("TotalProcElapsed", fmtDuration(totalElapsed))
	setStringProp("Speedup", speedUp)
	setStringProp("ExitCode", exitcode)
	if exitcode == "" {
		p.setPropState("ExitCode", "", hasStates)
	} else if exitcode == "0" {
		p.setPropState("ExitCode", "success", hasStates)
	} else {
		p.setPropState("ExitCode", "failed", hasStates)
	}

	setStringProp("StartError", node.StartError)
	if node.StartError == "" {
		p.setPropState("StartError", "", hasStates)
	} else {
		p.setPropState("StartError", "failed", hasStates)
	}

	setStringProp("Runner", node.Calculated.Runner)
	setStringProp("CmdLine", buildCommandLine(node.Calculated.Args))

	setStringProp("MaxProc", strconv.Itoa(node.MaxProc))
	setStringProp("NProc", strconv.Itoa(node.NProc))
	setStringProp("OnError.Status", rt.StatusName(node.OnError.Status))
	setStringProp("OnError.Siblings", rt.OpName(node.OnError.Siblings))

	// We only display rows that are needed, but if a row is needed then all of its props are displayed.
	cnt := 0
	for row := 0; row < p.rowCount; row++ {
		vis := !usedProps.Intersection(p.rowProps[row]).Empty()
		for _, id := range p.rowProps[row].List() {
			p.propLabels[id].SetVisible(vis)
			p.propEntries[id].SetVisible(vis)
			p.copyButtons[id].SetVisible(vis)
			if vis {
				cnt++
			}
		}
	}
	if cnt > 0 {
		p.tabLabel.SetMarkup(fmt.Sprintf("%s (%d)", "Properties", cnt))
	} else {
		p.tabLabel.SetText("")
	}
	p.updateTabState(hasStates)
	p.Grid.QueueResize()
}

func (p *NodeFrameProperites) SetNode(node *rt.Node) {
	p.frame.SetNode(node)
}

func fmtTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}

	now := time.Now()
	if t.Year() == now.Year() && t.Month() == now.Month() && t.Day() == now.Day() {
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
