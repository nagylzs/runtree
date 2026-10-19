package rt

import "github.com/nagylzs/set"

// ValueKind is the kind of value of a node property in a tree file
type ValueKind int

const (
	KindString     ValueKind = iota // a string (Enum lists the allowed values, if any)
	KindStringList                  // a list of strings
	KindBool                        // a boolean (yes/no, on/off are accepted too)
	KindInt                         // an integer
	KindExitCodes                   // a non-empty list of integers between 0 and 255
	KindDuration                    // a duration with a unit (90s, 30m, 1h30m), or 0
	KindStringMap                   // an object with string values
	KindVarMap                      // an object with string or string list values (variables)
	KindForVars                     // an object with string list values, or names of string list variables
	KindOnError                     // a status and/or a sibling operation (a string, or a list of two strings)
	KindNodeList                    // a list of nodes (objects), or of run nodes given as lists of arguments
	KindInclude                     // a top level object name, or a list of them ("file|name" for other files)
)

// Property describes a node property of tree files. This table is the single source of the property names: the
// parser accepts the properties listed here, and the JSON Schema (for editors) is generated from it.
type Property struct {
	Name        string
	Kind        ValueKind
	Enum        []string // allowed values of a KindString property
	Inherited   bool     // the value is inherited from the parent node when it is not specified
	Default     string   // documented default value (empty if none)
	Description string
}

var OnErrorStatuses = []string{"failed", "paused", "success"}
var OnErrorSiblingOps = []string{"none", "skip", "freeze"}

var NodeProperties = []Property{
	{Name: "type", Kind: KindString, Enum: []string{"run", "seq", "par"}, Default: "run",
		Description: "The type of the node: run (runs a command), seq (runs its sub-nodes one after the other) or " +
			"par (runs its sub-nodes in parallel). It is not needed when the node has a seq or par property, and a " +
			"node with include is a seq node by default."},
	{Name: "id", Kind: KindString,
		Description: "Identifier of the node, it must match [a-zA-Z][a-zA-Z0-9_-]* and be unique in the tree. " +
			"A generated id is used when it is not given."},
	{Name: "title", Kind: KindString,
		Description: "A single line, human-readable title, displayed in the GUI. Variables can be used."},
	{Name: "description", Kind: KindString,
		Description: "Multi-line description of the node, displayed in the GUI when the node is selected."},
	{Name: "args", Kind: KindStringList,
		Description: "The command and its arguments of a run node (after args_prefix). They are not given to a " +
			"shell: the first one is looked up in the PATH of the runner. It is not inherited. Variables " +
			"can be used."},
	{Name: "args_prefix", Kind: KindStringList, Inherited: true,
		Description: "Arguments that are put before the args of the run nodes in this subtree, e.g. [\"ssh\", " +
			"\"root@{server}\"]. Its variables are the ones of the node that sets it."},
	{Name: "cwd", Kind: KindString, Inherited: true,
		Description: "The directory where the processes are started. When it is empty, the current directory of " +
			"the runner is used (runtree's own for the local runner, the home directory for an ssh runner). ~ is " +
			"not expanded, a cwd that starts with ~ is an error."},
	{Name: "runner", Kind: KindString, Inherited: true, Default: "local",
		Description: "The runner that runs the processes of the node: local (runtree runs them itself), the " +
			"address (host:port or tcp://host:port) of an rtrunner, or ssh://[user@]host[:port][/path/to/rtrunner] " +
			"(ssh starts rtrunner on the host for each process). When it is not set, it is inherited from the " +
			"parent; the default of the root is local, or the --runner option of runtree."},
	{Name: "vars", Kind: KindVarMap, Inherited: true,
		Description: "Variables (name: value) for this node and its subtree. Values are strings or lists of " +
			"strings, and can be used as {name} in other properties."},
	{Name: "def_vars", Kind: KindVarMap,
		Description: "Default variable values: only used when the variable is not inherited from the parent. " +
			"A variable cannot be in both vars and def_vars."},
	{Name: "inherit_vars", Kind: KindBool, Default: "true",
		Description: "Inherit the variables of the parent node. When it is false, the node only has its own " +
			"variables and the for_vars of its parent; def_vars cannot be used then."},
	{Name: "envs", Kind: KindStringMap, Inherited: true,
		Description: "Environment variables (name: value) for the processes of this subtree. The values can use " +
			"variables, and {NAME} of the inherited envs."},
	{Name: "system_envs", Kind: KindBool, Default: "true",
		Description: "Use the environment of the runner (runtree's own for the local runner) as a base environment " +
			"for the processes."},
	{Name: "inherit_envs", Kind: KindBool, Default: "true",
		Description: "Inherit the environment variables (envs) of the parent node."},
	{Name: "xlocks", Kind: KindStringList,
		Description: "Names of resources that are locked exclusively while the node is running or paused: no other " +
			"node can lock them meanwhile. A name cannot be in the rlocks of the node too (also inherited ones). " +
			"It is not inherited."},
	{Name: "rlocks", Kind: KindStringList, Inherited: true,
		Description: "Names of resources that are locked while the node is running or paused. The topmost node " +
			"that lists a name holds it, its subtree shares it, other nodes cannot lock it meanwhile. It " +
			"is inherited, the node's own rlocks replace the inherited ones."},
	{Name: "provides", Kind: KindStringList,
		Description: "Names of resources that are provided when the node finishes without an error (success, or " +
			"skipped). The nodes that require them wait until then. It is not inherited."},
	{Name: "requires", Kind: KindStringList,
		Description: "Names of resources that must be provided (by other nodes) before the node can be started. " +
			"It is not inherited."},
	{Name: "max_proc", Kind: KindInt, Inherited: true, Default: "-1",
		Description: "Maximum number of running processes in this subtree; a negative value means no limit, 0 " +
			"starts nothing. It is inherited: a sub-node limits its own subtree with the same number."},
	{Name: "success_exit_codes", Kind: KindExitCodes, Inherited: true, Default: "[0]",
		Description: "Process exit codes that mean success, e.g. [0, 255] for \"ssh host reboot\". The listed " +
			"codes replace the default, so list 0 too if it should also mean success."},
	{Name: "timeout", Kind: KindDuration, Inherited: true, Default: "0",
		Description: "The longest time that a run node may run, e.g. 90s, 30m or 1h30m (0: no limit). Then its " +
			"process is stopped (Ctrl+C is typed into its terminal, SIGTERM and SIGKILL follow if it does not " +
			"stop), and the node fails: it gets the status of its on_error. It is inherited: on a seq or par " +
			"node it is the limit of each run node below it, not of the group."},
	{Name: "status", Kind: KindString, Enum: []string{"waiting", "frozen"}, Inherited: true, Default: "waiting",
		Description: "The initial status of the node. A frozen node is not started until it is thawed manually. It " +
			"is inherited: the sub-nodes of a frozen seq or par node are frozen too."},
	{Name: "on_error", Kind: KindOnError, Inherited: true, Default: "failed",
		Description: "What to do when the process or a sub-node fails: the status of the node (failed, paused or " +
			"success) and/or an operation on the not yet started siblings (none, skip or freeze), e.g. " +
			"paused, [paused, freeze] or \"paused, freeze\". The part that is not given is the default " +
			"(failed, none). Inherited from the parent's children_on_error, or from its on_error."},
	{Name: "children_on_error", Kind: KindOnError,
		Description: "The on_error value that the sub-nodes inherit (when they do not specify on_error). When it " +
			"is not given, the sub-nodes inherit the on_error of the node."},
	{Name: "expanded", Kind: KindBool, Default: "false",
		Description: "Display the sub-nodes of the node in the GUI when the tree is loaded."},
	{Name: "expand_on_active", Kind: KindBool, Default: "true",
		Description: "Expand the node in the GUI when it becomes active."},
	{Name: "collapse_on_finished", Kind: KindBool, Default: "true",
		Description: "Collapse the node in the GUI when it finishes."},
	{Name: "nodes", Kind: KindNodeList,
		Description: "The sub-nodes of a seq or par node."},
	{Name: "seq", Kind: KindNodeList,
		Description: "Sub-nodes that run one after the other (this makes the node a seq node)."},
	{Name: "par", Kind: KindNodeList,
		Description: "Sub-nodes that run in parallel (this makes the node a par node)."},
	{Name: "include", Kind: KindInclude,
		Description: "Top level objects to load as sub-nodes, from this file (name) or from another file " +
			"(file|name, relative to this file). A node that includes one object (without for_vars and " +
			"locks) becomes that object, with its own title, description, provides and requires."},
	{Name: "for_vars", Kind: KindForVars,
		Description: "Repeat the sub-nodes for every combination of the listed variable values (lists of " +
			"strings, or names of list variables). The first variable changes the slowest."},
	{Name: "ifeq", Kind: KindVarMap,
		Description: "Include the node only if all of the given variables have the given values (an undefined " +
			"variable is empty)."},
	{Name: "ifneq", Kind: KindVarMap,
		Description: "Include the node only if all of the given variables differ from the given values (an " +
			"undefined variable is empty)."},
}

// ValidNodeConfigs is the set of the node property names
var ValidNodeConfigs = func() *set.Set[string] {
	names := make([]string, len(NodeProperties))
	for i, p := range NodeProperties {
		names[i] = p.Name
	}
	return set.FromArray(names)
}()
