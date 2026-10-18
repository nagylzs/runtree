package rt

import (
	"cmp"
	"context"
	"fmt"
	"log/slog"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

// The events of the tree are log records with an "event" attribute, and a "node" attribute (except for the events
// of the tree itself), Node.log adds it. The message is complete for humans (the Log tab of the GUI shows only the
// message), the other attributes are for filtering. The records at Info level and above that are logged with the
// logger of the tree are kept in its EventLog, the other records too (e.g. the errors of a runner connection).
const (
	EventSchedulerStarted = "scheduler-started"
	EventStart            = "start"        // the command is started on the runner
	EventStartFailed      = "start-failed" // it could not be started
	EventExited           = "exited"       // the process exited
	EventStatus           = "status"       // the status of the node changed
	EventOperation        = "operation"    // a manual operation
	EventSignal           = "signal"       // a signal is sent to the process
	EventOnError          = "on-error"     // the operation of on_error is performed on the siblings
	EventProvided         = "provided"
	EventUnprovided       = "unprovided" // by a reset to waiting
	EventLocked           = "locked"     // the xlocks and rlocks of the node are acquired
	EventUnlocked         = "unlocked"
	EventRunnerError      = "runner-error"
	EventStopped          = "stopped" // an unattended run stopped: nothing runs, and nothing can be started
)

const (
	attrNode  = "node"
	attrEvent = "event"
)

// LogEntry is a log record of the tree, kept by EventLog
type LogEntry struct {
	Seq     uint64 // increasing, from 1
	Time    time.Time
	Level   slog.Level
	Node    *Node  // nil for the events of the tree
	Event   string // empty for other records
	Message string
	Attrs   []slog.Attr // the other attributes, resolved, the keys of groups are prefixed ("group.key")
}

// EventLog keeps the last log records of a tree in memory. It has its own lock, and it never waits for the tree lock.
type EventLog struct {
	mu      sync.Mutex
	entries []LogEntry
	seq     uint64
	max     int
}

const DefaultEventLogSize = 100000

func NewEventLog(max int) *EventLog {
	return &EventLog{max: max}
}

func (l *EventLog) add(e LogEntry) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.seq++
	e.Seq = l.seq
	if len(l.entries) >= l.max {
		// the oldest tenth goes at once, not one by one
		l.entries = slices.Delete(l.entries, 0, max(l.max/10, 1))
	}
	l.entries = append(l.entries, e)
}

// Since returns the entries after seq (all of them for 0), the oldest ones may be gone already.
func (l *EventLog) Since(seq uint64) []LogEntry {
	l.mu.Lock()
	defer l.mu.Unlock()
	i, _ := slices.BinarySearchFunc(l.entries, seq+1, func(e LogEntry, s uint64) int { return cmp.Compare(e.Seq, s) })
	return slices.Clone(l.entries[i:])
}

// eventHandler keeps the records in the EventLog, and passes them to the next handler
type eventHandler struct {
	log    *EventLog
	next   slog.Handler
	with   LogEntry // the attributes of WithAttrs
	prefix string   // of the attribute keys, from WithGroup ("a.b.")
}

func (h *eventHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return level >= slog.LevelInfo || h.next.Enabled(ctx, level)
}

func (h *eventHandler) Handle(ctx context.Context, r slog.Record) error {
	if r.Level >= slog.LevelInfo {
		e := h.with
		e.Attrs = slices.Clone(e.Attrs)
		e.Time, e.Level, e.Message = r.Time, r.Level, r.Message
		r.Attrs(func(a slog.Attr) bool {
			e.add(h.prefix, a)
			return true
		})
		h.log.add(e)
	}
	if h.next.Enabled(ctx, r.Level) {
		return h.next.Handle(ctx, r)
	}
	return nil
}

func (h *eventHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	h2 := *h
	h2.with.Attrs = slices.Clone(h.with.Attrs)
	for _, a := range attrs {
		h2.with.add(h.prefix, a)
	}
	h2.next = h.next.WithAttrs(attrs)
	return &h2
}

func (h *eventHandler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	h2 := *h
	h2.prefix += name + "."
	h2.next = h.next.WithGroup(name)
	return &h2
}

// add adds an attribute to the entry: the node and the event have their own fields
func (e *LogEntry) add(prefix string, a slog.Attr) {
	if prefix == "" {
		if n, ok := a.Value.Any().(*Node); ok && a.Key == attrNode {
			e.Node = n
			return
		}
		if a.Key == attrEvent && a.Value.Kind() == slog.KindString {
			e.Event = a.Value.String()
			return
		}
	}
	v := a.Value.Resolve()
	if v.Kind() == slog.KindGroup {
		if a.Key != "" {
			prefix += a.Key + "."
		}
		for _, ga := range v.Group() {
			e.add(prefix, ga)
		}
		return
	}
	if a.Key == "" {
		return
	}
	e.Attrs = append(e.Attrs, slog.Attr{Key: prefix + a.Key, Value: v})
}

// LogValue logs the node with its id
func (n *Node) LogValue() slog.Value {
	if n == nil {
		return slog.StringValue("")
	}
	return slog.StringValue(n.Id)
}

// log returns the logger of the node: the logger of the tree, with the node attribute
func (n *Node) log() *slog.Logger {
	return n.Tree.logger.With(attrNode, n)
}

// statusLevel is the level of the event of a status change: Warn when the node failed, or it needs a decision
func statusLevel(st Status) slog.Level {
	if StatusHasError(st) || st == StatusPaused {
		return slog.LevelWarn
	}
	return slog.LevelInfo
}

// quoteArgs joins command line arguments with spaces, quoting the ones that would be ambiguous
func quoteArgs(args []string) string {
	quoted := make([]string, len(args))
	for i, a := range args {
		if a == "" || strings.ContainsAny(a, " \t\n\"'\\") {
			a = strconv.Quote(a)
		}
		quoted[i] = a
	}
	return strings.Join(quoted, " ")
}

// formatDuration rounds a duration for messages
func formatDuration(d time.Duration) string {
	switch {
	case d < time.Second:
		return d.Round(time.Millisecond).String()
	case d < time.Minute:
		return d.Round(100 * time.Millisecond).String()
	default:
		return d.Round(time.Second).String()
	}
}

// lockList is the text of the xlocks and rlocks of the node for messages, empty when the node has no locks
func (n *Node) lockList() string {
	var parts []string
	if xl := n.Calculated.XLocks.List(); len(xl) > 0 {
		slices.Sort(xl)
		parts = append(parts, fmt.Sprintf("xlocks %s", strings.Join(xl, ", ")))
	}
	if rl := n.Calculated.RLocks.List(); len(rl) > 0 {
		slices.Sort(rl)
		parts = append(parts, fmt.Sprintf("rlocks %s", strings.Join(rl, ", ")))
	}
	return strings.Join(parts, "; ")
}
