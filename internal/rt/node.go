package rt

import (
	"slices"
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

	undefinedRefs []undefinedRef                // found while loading the node
	generatedId   bool                          // the id was not given in the source
	sources       []map[interface{}]interface{} // the objects of the source that the node was loaded from

	Type               Type   // immutable!
	Id                 string // immutable!
	InheritVars        bool
	SystemEnvs         bool
	InheritEnvs        bool
	InheritRequires    bool
	MaxProc            int
	NProc              int
	OnError            OnError
	ChildrenOnError    OnError       // inherited by the sub-nodes that do not specify on_error
	SuccessExitCodes   []int         // exit codes that mean success, inherited from the parent, the default is [0]
	Timeout            time.Duration // the longest time that a run node may run (0: no limit), inherited
	Expanded           bool
	ExpandOnActive     bool
	CollapseOnFinished bool

	Parsed     Parsed
	Calculated Calculated

	ClientArgs *runner.CmdArgs
	Client     *runner.Client
	starting   bool   // connecting to the runner, see Start
	StartError string // Only used when the client cannot be started, it indicates an error without a client state
	// TimedOut: the node ran longer than its timeout, its process is stopped (see checkTimeout), until it is started
	// again
	TimedOut bool
	Status   Status // This **must** be updated from Node.updateStatus!
	// held is the status of a seq/par node that is set on the node itself, instead of being calculated from its
	// sub-nodes: StatusFrozen (status: frozen) until it is thawed, StatusSkipped (Skip) until it is reset;
	// StatusWaiting when there is none.
	held Status
	// frozenInFile: the node is frozen by "status: frozen" in the tree file (its own, or inherited)
	frozenInFile bool
	// frozenGate: the node has its own "status: frozen" in the tree file (an approval gate, see ThawGates), the
	// sub-nodes that inherit it are frozen with it
	frozenGate bool
	Lock       string // the lock file of the tree (the lock property of the root node), see app.LockTree

	WantManualStatus bool   // Node.PerformOp sets this to true when it wants to manually set status
	ManualStatus     Status // Node.PerformOp sets this to the desired manual status

	Blocked          bool
	MissingRequired  *set.Set[string]
	CannotXAcquire   *set.Set[string]
	CannotRAcquire   *set.Set[string]
	SchedulerMessage string

	Started  time.Time
	Finished time.Time
	Starts   int // how many times the node was started (a re-run is the second start)

	elapsed             time.Duration // the time of the last run of this node (see updateStatus)
	totalProcElapsed    time.Duration // total elapsed in running processes, this node and its subnodes
	totalProcCommitedTo time.Time     // Last time we commited time to totalProcElapsed

}

// Parsed contains all values that are parsed from YAML file, and subject to variable substitution
type Parsed struct {
	Vars        map[string]interface{}
	LoopVars    map[string]interface{} // the combination of the parent's for_vars that this node was generated for
	DefVars     map[string]interface{}
	IfEq        map[string]interface{}
	IfNEq       map[string]interface{}
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
	IfEq        map[string]interface{}
	IfNEq       map[string]interface{}
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
	VarSources  map[string]VarSource // where the values of Vars are set
}

// VarSource tells where the value of a variable is set: in the vars or def_vars of Node, or in the for_vars of Node
// (for its sub-nodes)
type VarSource struct {
	Node *Node
	Prop string // vars, def_vars or for_vars
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
		Id:                 tree.newId(),
		generatedId:        true,
		InheritVars:        true,
		SystemEnvs:         true,
		InheritEnvs:        true,
		InheritRequires:    true,
		Expanded:           false,
		ExpandOnActive:     true,
		CollapseOnFinished: true,
		MaxProc:            -1,
		OnError:            DefaultOnError,
		ChildrenOnError:    DefaultOnError,

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
	n.InheritVars = n2.InheritVars
	n.SystemEnvs = n2.SystemEnvs
	n.InheritEnvs = n2.InheritEnvs
	n.InheritRequires = n2.InheritRequires
	n.MaxProc = n2.MaxProc
	n.SuccessExitCodes = n2.SuccessExitCodes
	n.Timeout = n2.Timeout
	n.NProc = n2.NProc
	n.OnError = n2.OnError
	n.ChildrenOnError = n2.ChildrenOnError
	n.Expanded = n2.Expanded
	n.ExpandOnActive = n2.ExpandOnActive
	n.CollapseOnFinished = n2.CollapseOnFinished
	n.Parsed = n2.Parsed
	lvl, idx := n.Calculated.Level, n.Calculated.Idx
	n.Calculated = n2.Calculated
	n.Calculated.Level, n.Calculated.Idx = lvl, idx
	n.Status = n2.Status
	n.held = n2.held
	n.frozenInFile = n2.frozenInFile
	n.frozenGate = n2.frozenGate
}

// keepOwn sets the properties of a node that its single included object replaced (see loadFrom), but that the object
// does not inherit: its own title and description, and its provides and requires (with the object's ones)
func (n *Node) keepOwn(p Parsed, c Calculated) {
	if p.Title != "" {
		n.Parsed.Title, n.Calculated.Title = p.Title, c.Title
	}
	if p.Description != "" {
		n.Parsed.Description, n.Calculated.Description = p.Description, c.Description
	}
	union := func(own, object *set.Set[string]) *set.Set[string] {
		switch {
		case own == nil:
			return object
		case object == nil:
			return own.Clone()
		}
		return own.Union(object)
	}
	if len(p.Provides) > 0 {
		n.Parsed.Provides = append(slices.Clone(p.Provides), n.Parsed.Provides...)
		n.Calculated.Provides = union(c.Provides, n.Calculated.Provides)
	}
	if len(p.Requires) > 0 {
		n.Parsed.Requires = append(slices.Clone(p.Requires), n.Parsed.Requires...)
		n.Calculated.Requires = union(c.Requires, n.Calculated.Requires)
	}
}

func (n *Node) DebugPath() string {
	res := ""
	if n.Calculated.Title != "" {
		res = n.Calculated.Title
	} else if n.Parsed.Title != "" {
		res = n.Parsed.Title
	} else {
		res = n.Id
	}
	if n.Parent != nil {
		return n.Parent.DebugPath() + "/" + res
	}
	return res
}
