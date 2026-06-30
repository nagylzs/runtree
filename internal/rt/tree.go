package rt

import (
	"fmt"
	"log/slog"
	"regexp"
	"sync"

	"github.com/nagylzs/runtree/internal/events"
	"github.com/nagylzs/set"
)

type TreeNotifier = func(t *Tree)

type Tree struct {
	Root     *Node
	NodeById map[string]*Node
	Nodes    []*Node // access nodes by post-order index

	logger    *slog.Logger
	awaker    *events.Debouncer[int]
	unblocker *events.Debouncer[int]

	nodeListenerLock sync.Mutex
	nodeListeners    []chan *Node // node level events, when node status changes

	lck    *sync.RWMutex
	lckwho *sync.Mutex
	who    string

	providedBy  map[string]*Node
	provided    *set.Set[string]
	rAcquiredBy map[string]*Node
	xAcquiredBy map[string]*Node
	rAcquired   *set.Set[string]
	xAcquired   *set.Set[string]

	terminalHelper TerminalHelper
}

func (t *Tree) addNodeById(n *Node) error {
	ok, err := regexp.Match("[a-zA-Z][a-zA-Z0-9_\\-]*", []byte(n.Id))
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("invalid id: %s must match [a-zA-Z][a-zA-Z0-9_\\-]*", n.Id)
	}
	_, ok = t.NodeById[n.Id]
	if ok {
		return fmt.Errorf("duplicate node id: %s", n.Id)
	}
	t.NodeById[n.Id] = n
	return nil
}

func LoadTree(allTrees map[string]map[string]interface{}, filename string, id string, maxDepth uint) (*Tree, error) {
	rawTrees, ok := allTrees[filename]
	if !ok {
		return nil, fmt.Errorf("file not found: %s", filename)
	}
	rn, ok := rawTrees[id]
	if !ok {
		return nil, fmt.Errorf("'%s' is missing", id)
	}
	root, ok := rn.(map[interface{}]interface{})
	if !ok {
		return nil, fmt.Errorf("'%s' should be an object", id)
	}

	tree := &Tree{
		Root:             nil,
		NodeById:         make(map[string]*Node),
		logger:           slog.Default(),
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
	}
	tree.awaker = events.NewDebouncer(tree.debouncedSchedule, true)
	tree.unblocker = events.NewDebouncer(tree.debouncedUnblock, false)

	var ord uint = 0
	_, err := LoadNode(TypeRun, root, nil, tree, allTrees, filename, maxDepth, &ord, 0, nil, nil)
	if err != nil {
		return nil, err
	}
	nodes := make([]*Node, ord)
	tree.Root.FillNodeList(nodes)
	tree.Nodes = nodes
	return tree, err
}

func (t *Tree) setWho(who string) {
	t.lckwho.Lock()
	t.who = who
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

func (t *Tree) PrintWho() {
	t.lckwho.Lock()
	who := t.who
	t.lckwho.Unlock()
	if who != "" {
		println("Tree locked by", who)
	}
}

func (t *Tree) SetTerminalHelper(helper TerminalHelper) {
	t.terminalHelper = helper
}
