package rt

import (
	"fmt"
	"log/slog"
	"regexp"
	"sync"
	"sync/atomic"
	"time"

	"github.com/nagylzs/runtree/internal/events"
	"github.com/nagylzs/runtree/internal/runner"
	"github.com/nagylzs/set"
)

type TreeNotifier = func(t *Tree)

type Tree struct {
	Root     *Node
	NodeById map[string]*Node
	Nodes    []*Node // access nodes by post-order index

	logger    *slog.Logger // logs the events too, see eventlog.go
	events    *EventLog
	awaker    *events.Debouncer[int]
	unblocker *events.Debouncer[int]
	stopped   atomic.Bool // the scheduler is stopped, see StopScheduler

	nodeListenerLock sync.Mutex
	nodeListeners    []chan *Node // node level events, when node status changes

	lck      *sync.RWMutex
	lckwho   *sync.Mutex
	who      string
	whoSince time.Time // when who got the lock
	warned   time.Time // whoSince of the last warning of CheckLock

	providedBy  map[string]*Node
	provided    *set.Set[string]
	rAcquiredBy map[string]*Node
	xAcquiredBy map[string]*Node
	rAcquired   *set.Set[string]
	xAcquired   *set.Set[string]

	terminalHelper TerminalHelper
	outputRecorder OutputRecorder

	defaultRunner string // the runner of the nodes that do not specify one, see Sources.DefaultRunner

	lastId int // the number of the last generated node id
}

var idRe = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_-]*$`)

// newId returns a generated node id that is not used yet
func (t *Tree) newId() string {
	for {
		t.lastId++
		id := fmt.Sprintf("n%d", t.lastId)
		if _, ok := t.NodeById[id]; !ok {
			return id
		}
	}
}

// addNodeById registers the node by its id. A node with a generated id gives up its id when another node has it
// explicitly (generated ids look like "n12", so they can be given explicitly too).
func (t *Tree) addNodeById(n *Node) error {
	if !n.generatedId && !idRe.MatchString(n.Id) {
		return fmt.Errorf("invalid id: %q must match [a-zA-Z][a-zA-Z0-9_-]*", n.Id)
	}
	if prev, ok := t.NodeById[n.Id]; ok {
		if !prev.generatedId || n.generatedId {
			return fmt.Errorf("duplicate node id: %s", n.Id)
		}
		prev.Id = t.newId()
		t.NodeById[prev.Id] = prev
	}
	t.NodeById[n.Id] = n
	return nil
}

// LoadTree loads the tree from the top level object called id. The returned errors are ParseErrors.
func LoadTree(sources *Sources, filename string, id string, maxDepth uint) (*Tree, error) {
	tree, err := loadTree(sources, filename, id, maxDepth)
	if err != nil {
		return nil, err
	}
	nodes := make([]*Node, len(tree.Nodes))
	tree.Root.FillNodeList(nodes)
	tree.Nodes = nodes
	return tree, nil
}

// LoadPartialTree is LoadTree for editors: it also returns the tree when there are errors, with the nodes that
// could be loaded (it cannot be run). The tree is nil when the root object cannot be loaded at all.
func LoadPartialTree(sources *Sources, filename string, id string, maxDepth uint) (*Tree, error) {
	tree, err := loadTree(sources, filename, id, maxDepth)
	if tree != nil {
		tree.Nodes = nil
		var walk func(n *Node)
		walk = func(n *Node) {
			tree.Nodes = append(tree.Nodes, n)
			for _, c := range n.Nodes {
				walk(c)
			}
		}
		if tree.Root != nil {
			walk(tree.Root)
		}
	}
	return tree, err
}

// loadTree loads the tree, it returns the tree with the errors too (its Nodes has the number of nodes only)
func loadTree(sources *Sources, filename string, id string, maxDepth uint) (*Tree, error) {
	rawTrees, ok := sources.Files[filename]
	if !ok {
		return nil, newParseError(Pos{File: filename}, "the file was not loaded")
	}
	rn, ok := rawTrees[id]
	if !ok {
		return nil, newParseError(Pos{File: filename}, "the top level object %q is missing", id)
	}
	root, ok := rn.(map[interface{}]interface{})
	if !ok {
		return nil, newParseError(sources.TopPos(filename, id), "%q should be an object", id)
	}

	tree := &Tree{
		Root:             nil,
		NodeById:         make(map[string]*Node),
		events:           NewEventLog(DefaultEventLogSize),
		nodeListenerLock: sync.Mutex{},
		nodeListeners:    make([]chan *Node, 1000),
		lck:              &sync.RWMutex{},
		lckwho:           &sync.Mutex{},
		providedBy:       make(map[string]*Node),
		provided:         set.NewSet[string](),
		rAcquiredBy:      make(map[string]*Node),
		xAcquiredBy:      make(map[string]*Node),
		rAcquired:        set.NewSet[string](),
		xAcquired:        set.NewSet[string](),
		defaultRunner:    sources.DefaultRunner,
	}
	if tree.defaultRunner == "" {
		tree.defaultRunner = runner.LocalAddress
	}
	tree.SetLogHandler(slog.Default().Handler())
	tree.awaker = events.NewDebouncer(tree.debouncedSchedule, true)
	tree.unblocker = events.NewDebouncer(tree.debouncedUnblock, false)

	var ord uint = 0
	_, err := LoadNode(TypeRun, root, nil, tree, sources, filename, maxDepth, &ord, 0, nil, nil)
	if err != nil {
		sources.report(err)
	}
	tree.Nodes = make([]*Node, ord)
	// all errors of the tree
	return tree, sources.Errors()
}

// SetLogHandler sets the handler of the log records of the tree (they are kept in its EventLog too). The default is
// the handler of the default logger when the tree is loaded.
func (t *Tree) SetLogHandler(h slog.Handler) {
	t.logger = slog.New(&eventHandler{log: t.events, next: h})
}

// EventLog returns the log records of the tree, see eventlog.go
func (t *Tree) EventLog() *EventLog {
	return t.events
}

func (t *Tree) setWho(who string) {
	t.lckwho.Lock()
	t.who = who
	t.whoSince = time.Now()
	t.lckwho.Unlock()
}

func (t *Tree) Lock(who string) {
	t.lck.Lock()
	t.setWho(who)
}

func (t *Tree) UnLock() {
	t.setWho("")
	t.lck.Unlock()
}

func (t *Tree) RLock(who string) {
	t.lck.RLock()
	t.setWho("R:" + who)
}

func (t *Tree) RUnlock() {
	t.setWho("")
	t.lck.RUnlock()
}

// CheckLock logs a warning when the tree is locked by the same holder for longer than d, it is probably a deadlock,
// or a long blocking call (e.g. connecting to a runner that does not answer). It warns once for each time the lock is
// held. (The holder is only known for sure when there is one, because the readers overwrite each other.) It is called
// periodically by the GUI.
func (t *Tree) CheckLock(d time.Duration) {
	t.lckwho.Lock()
	who, since := t.who, t.whoSince
	warn := who != "" && time.Since(since) > d && t.warned != since
	if warn {
		t.warned = since
	}
	t.lckwho.Unlock()
	if warn {
		t.logger.Warn("the tree is locked for a long time", "by", who, "for", time.Since(since).Round(time.Second))
	}
}

func (t *Tree) SetTerminalHelper(helper TerminalHelper) {
	t.terminalHelper = helper
}

// SetOutputRecorder sets the recorder of the terminal output, it must be called before the tree runs
func (t *Tree) SetOutputRecorder(r OutputRecorder) {
	t.outputRecorder = r
}

// UnfinishedRunCounts counts the run nodes that are not in Success or Skipped status, grouped by status.
// Sub-nodes of a skipped node are not counted, because the scheduler will never start them.
func (t *Tree) UnfinishedRunCounts(locked bool) map[Status]int {
	if !locked {
		panic("must be called in locked state")
	}
	counts := make(map[Status]int)
	var count func(n *Node)
	count = func(n *Node) {
		if n.Status == StatusSkipped {
			return
		}
		if n.Type == TypeRun && n.Status != StatusSuccess {
			counts[n.Status]++
		}
		for _, c := range n.Nodes {
			count(c)
		}
	}
	count(t.Root)
	return counts
}
