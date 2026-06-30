package rt

import (
	"strings"
	"time"

	"github.com/nagylzs/runtree/internal/runner"
	"github.com/nagylzs/set"
)

// Node represents nodes as they are given in the YAML file(s)
type Node struct {
	Tree   *Tree
	Parent *Node
	Nodes  []*Node

	Type               Type   // immutable!
	Id                 string // immutable!
	BuiltinVars        bool
	InheritVars        bool
	SystemEnvs         bool
	InheritEnvs        bool
	InheritRequires    bool
	MaxProc            int
	NProc              int
	OnError            OnError
	Expanded           bool
	ExpandOnActive     bool
	CollapseOnFinished bool

	Parsed     Parsed
	Calculated Calculated

	ClientArgs *runner.CmdArgs
	Client     *runner.Client
	StartError string // Only used when the client cannot be started, it indicates an error without a client state
	Status     Status // This **must** be updated from Node.updateStatus!

	WantManualStatus bool   // Node.PerformOp sets this to true when it wants to manually set status
	ManualStatus     Status // Node.PerformOp sets this to the desired manual status

	Blocked          bool
	MissingRequired  *set.Set[string]
	CannotXAcquire   *set.Set[string]
	CannotRAcquire   *set.Set[string]
	SchedulerMessage string

	Started  time.Time
	Finished time.Time

	elapsed             time.Duration // time elapsed in running state, on this exact node
	totalProcElapsed    time.Duration // total elapsed in running processes, this node and its subnodes
	totalProcCommitedTo time.Time     // Last time we commited time to totalProcElapsed

}

// Parsed contains all values that are parsed from YAML file, and subject to variable substitution
type Parsed struct {
	Vars        map[string]interface{}
	DefVars     map[string]interface{}
	IfEq        map[string]string
	IfNEq       map[string]string
	Title       string
	Description string
	Envs        map[string]string
	Args        []string
	ArgsPrefix  []string
	CWD         string
	Runner      string
	XLocks      []string
	RLocks      []string
	Provides    []string
	Requires    []string
}

// Calculated contains all values that already went through variable substitution
type Calculated struct {
	Idx         uint
	Level       uint
	Vars        map[string]interface{}
	IfEq        map[string]string
	IfNEq       map[string]string
	Title       string
	Description string
	Envs        map[string]string
	ArgsPrefix  []string
	Args        []string
	CWD         string
	Runner      string
	RLocks      *set.Set[string]
	XLocks      *set.Set[string]
	Provides    *set.Set[string]
	Requires    *set.Set[string]
}

type OnError struct {
	Status   Status
	Siblings NodeOp
}

var DefaultOnError = OnError{
	Status:   StatusFailed,
	Siblings: OpNone,
}

type NodeNotifier = func(n *Node)

func New(defType Type, parent *Node, tree *Tree) *Node {
	n := &Node{
		Tree:               tree,
		Type:               defType,
		Parent:             parent,
		Id:                 nextId(),
		BuiltinVars:        true,
		InheritVars:        true,
		SystemEnvs:         true,
		InheritEnvs:        true,
		InheritRequires:    true,
		Expanded:           false,
		ExpandOnActive:     true,
		CollapseOnFinished: true,
		MaxProc:            -1,
		OnError:            DefaultOnError,

		MissingRequired: set.NewSet[string](),
		CannotRAcquire:  set.NewSet[string](),
		CannotXAcquire:  set.NewSet[string](),
	}
	if tree.Root == nil {
		tree.Root = n
	}
	err := tree.addNodeById(n)
	if err != nil {
		panic(err) // this should never happen, the id is a random uuid
	}
	return n
}

// ParentOf returns true if the called not is a parent of other
func (n *Node) ParentOf(other *Node) bool {
	if other == nil {
		return false
	}
	other = other.Parent
	for other != nil {
		if other == n {
			return true
		}
		other = other.Parent
	}
	return false
}

// ChildOf returns true if the called not is a child of other
func (n *Node) ChildOf(other *Node) bool {
	if other == nil {
		return false
	}
	return other.ParentOf(n)
}

func (n *Node) LockTree(who string) {
	n.Tree.Lock(who)
}

func (n *Node) UnLockTree() {
	n.Tree.UnLock()
}

func (n *Node) RLockTree(who string) {
	n.Tree.RLock(who)
}

func (n *Node) RUnlockTree() {
	n.Tree.RUnlock()
}

func (n *Node) DisplayLabel() string {
	if n.Calculated.Title != "" {
		return n.Calculated.Title
	}
	if len(n.Calculated.Args) > 0 {
		return strings.Join(n.Calculated.Args, " ")
	}
	return n.Id
}

// Visible if it is a root node, or if its parent is visible and expanded.
func (n *Node) Visible() bool {
	if n.Parent == nil {
		return true
	}
	if n.Parent.Expanded && n.Parent.Visible() {
		return true
	}
	return false
}

// HasChild is true if the node has at least one child.
func (n *Node) HasChild() bool {
	return len(n.Nodes) > 0
}

func (n *Node) addTotalElapsed(elapsed time.Duration) {
	n.totalProcElapsed += elapsed
	if n.Parent != nil {
		n.Parent.addTotalElapsed(elapsed)
	}
}

// CalcElapsed calculates the current elapsed and total elapsed values.
func (n *Node) CalcElapsed() (elapsed time.Duration, totalElapsed time.Duration) {
	if n.Started.IsZero() || !StatusActive(n.Status) {
		return n.elapsed, n.totalProcElapsed
	}
	return n.elapsed + time.Since(n.Started), n.totalProcElapsed
}

// loadFrom is used when the node is loaded into the parent, using include with a single name

func (n *Node) loadFrom(n2 *Node) {
	if n.Tree != n2.Tree {
		panic("Node.loadFrom: trees must be the same")
	}
	n.Type = n2.Type
	n.BuiltinVars = n2.BuiltinVars
	n.InheritVars = n2.InheritVars
	n.SystemEnvs = n2.SystemEnvs
	n.InheritEnvs = n2.InheritEnvs
	n.InheritRequires = n2.InheritRequires
	n.MaxProc = n2.MaxProc
	n.NProc = n2.NProc
	n.OnError = n2.OnError
	n.Expanded = n2.Expanded
	n.ExpandOnActive = n2.ExpandOnActive
	n.CollapseOnFinished = n2.CollapseOnFinished
	n.Parsed = n2.Parsed
	lvl, idx := n.Calculated.Level, n.Calculated.Idx
	n.Calculated = n2.Calculated
	n.Calculated.Level, n.Calculated.Idx = lvl, idx
	n.Status = n2.Status
}
