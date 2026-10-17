package logdb

import (
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
)

// The output of runtree --show-runs, --show-events and --show-output. The times are local.

const showTimeFormat = "2006-01-02 15:04:05"

// PrintRuns prints the runs of the database, the last one last
func PrintRuns(w io.Writer, r *Reader) error {
	runs, err := r.Runs()
	if err != nil {
		return err
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "RUN\tSTARTED\tFINISHED\tHOST\tUSER\tEVENTS\tOUTPUT\tTREE")
	for _, run := range runs {
		finished := "-" // still running, or runtree was killed
		if !run.Finished.IsZero() {
			finished = run.Finished.Local().Format(showTimeFormat)
		}
		events := fmt.Sprint(run.Events)
		if run.Dropped > 0 {
			events += fmt.Sprintf(" (%d dropped)", run.Dropped)
		}
		_, _ = fmt.Fprintf(tw, "%d\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n", run.ID, run.Started.Local().Format(showTimeFormat),
			finished, run.Host, run.User, events, formatBytes(run.Output), run.TreeFile)
	}
	return tw.Flush()
}

// PrintEvents prints the events of a run, one per line: time, level, [node id] node path: message
func PrintEvents(w io.Writer, r *Reader, runID int64) error {
	if err := r.CheckRun(runID); err != nil {
		return err
	}
	events, err := r.Events(runID)
	if err != nil {
		return err
	}
	for _, e := range events {
		node := ""
		if e.NodeID != "" {
			node = fmt.Sprintf("[%s] %s: ", e.NodeID, e.Path)
		}
		_, err := fmt.Fprintf(w, "%s %-5s %s%s\n", e.Time.Local().Format(showTimeFormat), e.Level, node, e.Message)
		if err != nil {
			return err
		}
	}
	return nil
}

// FindNode returns the node of a run with the given id, label or path (or the only one whose path contains it)
func FindNode(r *Reader, runID int64, name string) (NodeInfo, error) {
	if err := r.CheckRun(runID); err != nil {
		return NodeInfo{}, err
	}
	nodes, err := r.FindNodes(runID, name)
	if err != nil {
		return NodeInfo{}, err
	}
	switch len(nodes) {
	case 0:
		return NodeInfo{}, fmt.Errorf("run %d has no node %q", runID, name)
	case 1:
		return nodes[0], nil
	}
	var lines []string
	for _, n := range nodes {
		lines = append(lines, fmt.Sprintf("  %s: %s", n.ID, n.Path))
	}
	return NodeInfo{}, fmt.Errorf("%d nodes of run %d match %q, use the id:\n%s", len(nodes), runID, name,
		strings.Join(lines, "\n"))
}

// PrintOutput writes the terminal output of a start of a node (the last one when start is 0) to w. It returns the
// start, and the number of starts that have output.
func PrintOutput(w io.Writer, r *Reader, runID int64, node NodeInfo, start int) (int, int, error) {
	starts, err := r.Starts(runID, node.ID)
	if err != nil {
		return 0, 0, err
	}
	if len(starts) == 0 {
		return 0, 0, fmt.Errorf("%s (%s) has no output in run %d", node.ID, node.Path, runID)
	}
	if start == 0 {
		start = starts[len(starts)-1]
	}
	found := false
	for _, s := range starts {
		found = found || s == start
	}
	if !found {
		return 0, 0, fmt.Errorf("%s (%s) has no output for start %d in run %d (starts with output: %v)", node.ID,
			node.Path, start, runID, starts)
	}
	return start, len(starts), r.Output(runID, node.ID, start, w)
}

// formatBytes formats a size for humans: 16 B, 12.4 KB, 3.0 MB...
func formatBytes(n int64) string {
	if n < 1024 {
		return fmt.Sprintf("%d B", n)
	}
	size, unit := float64(n)/1024, "KB"
	for _, u := range []string{"MB", "GB", "TB"} {
		if size < 1024 {
			break
		}
		size, unit = size/1024, u
	}
	return fmt.Sprintf("%.1f %s", size, unit)
}
