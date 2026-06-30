package rt

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mitchellh/mapstructure"
	"github.com/nagylzs/set"
	"gopkg.in/yaml.v2"
)

const MaxYamlFileSize = 1024 * 1024

var ValidNodeConfigs = set.FromArray([]string{
	"id", "title", "description",
	"args", "argsprefix",
	"cwd", "vars", "defvars", "builtin_vars", "inherit_vars", "system_envs", "inherit_envs",
	"provides", "requires",
	"on_error", "max_proc", "envs", "type", "rlocks", "xlocks", "nodes",
	"par", "seq", "run", "include", "status",
	"ifeq", "ifneq",
	"for_vars",
})

func AddTree(inputFile string, allTrees map[string]map[string]interface{}) (map[string]interface{}, string, error) {
	fi, err := os.Stat(inputFile)
	if err != nil {
		return nil, "", fmt.Errorf("could not stat input file %v: %w", inputFile, err)
	}
	filename := filepath.Base(inputFile)
	if fi.Size() > MaxYamlFileSize {
		return nil, filename, fmt.Errorf("input file %v bigger than %v bytes", inputFile, MaxYamlFileSize)
	}
	data, err := os.ReadFile(inputFile)
	if err != nil {
		return nil, filename, fmt.Errorf("could not read input file %v: %w", inputFile, err)
	}
	var rawTrees map[string]interface{}
	err = yaml.Unmarshal(data, &rawTrees)
	if err != nil {
		return nil, filename, fmt.Errorf("could not parse input file %v: %w", inputFile, err)
	}
	allTrees[filename] = rawTrees
	return rawTrees, filename, nil
}

func ParseToDom(allTrees map[string]map[string]interface{}, filename string, maxDepth uint) (*Tree, error) {
	return LoadTree(allTrees, filename, "tree", maxDepth)
}

// LoadNode loads a node from an arbitrary map of values and adds it into a tree.
// If the node should be excluded ("ifeq" and "ifneq") then it returns a nil node.
func LoadNode(defType Type, raw map[interface{}]interface{}, parent *Node, tree *Tree,
	allTrees map[string]map[string]interface{}, filename string,
	maxDepth uint, idx *uint, level uint, loadInto *Node, overrideVars map[string]string) (*Node, error) {
	if maxDepth == 0 {
		return nil, fmt.Errorf("maximum depth exceeded")
	}

	n := &Node{
		Parent:          parent,
		Tree:            tree,
		MissingRequired: set.NewSet[string](),
		CannotXAcquire:  set.NewSet[string](),
		CannotRAcquire:  set.NewSet[string](),
	}
	if tree.Root == nil {
		tree.Root = n
	}

	_, hasType := raw["type"]
	_, hasInclude := raw["include"]
	if hasType {
		typ, err := getString(raw, "type")
		if err != nil {
			return n, err
		}
		typ = strings.TrimSpace(strings.ToLower(typ))
		switch typ {
		case "run":
			n.Type = TypeRun
		case "seq":
			n.Type = TypeSeq
		case "par":
			n.Type = TypePar
		default:
			return n, fmt.Errorf("invalid type: %s", typ)
		}
	} else {
		n.Type = defType
	}
	// TODO: document this, if a node does not specify but it specifies include, then the default
	// becomes Seq
	if hasInclude && !hasType {
		n.Type = TypeSeq
	}
	if hasInclude && n.Type == TypeRun {
		return n, fmt.Errorf("invalid type: run type nodes cannot include other nodes")
	}

	var err error
	n.Id, err = getStringDef(raw, "id", "")
	if err != nil {
		return n, err
	}
	if n.Id == "" {
		n.Id = nextId()
	}
	err = tree.addNodeById(n)
	if err != nil {
		return n, err
	}

	n.Parsed.Title, err = getStringDef(raw, "title", "")
	if err != nil {
		return n, err
	}

	n.Parsed.Description, err = getStringDef(raw, "description", "")
	if err != nil {
		return n, err
	}

	n.Parsed.CWD, err = getStringDef(raw, "cwd", "")
	if err != nil {
		return n, err
	}

	n.Parsed.Vars, err = getStringStringMapDef(raw, "vars", overrideVars) /* getStringMapDef */
	if err != nil {
		return n, err
	}

	n.Parsed.DefVars, err = getStringStringMapDef(raw, "defvars", nil) /* getStringMapDef */
	if err != nil {
		return n, err
	}

	if n.Parsed.Vars != nil && n.Parsed.DefVars != nil {
		for k := range n.Parsed.DefVars {
			_, has := n.Parsed.Vars[k]
			if !has {
				return n, fmt.Errorf("variable found both in vars and defvars: %s", k)
			}
		}
	}

	n.Parsed.IfEq, err = getStringStringMapDef(raw, "ifeq", nil) /* getStringMapDef */
	if err != nil {
		return n, err
	}

	n.Parsed.IfNEq, err = getStringStringMapDef(raw, "ifneq", nil) /* getStringMapDef */
	if err != nil {
		return n, err
	}

	n.BuiltinVars, err = getBoolDef(raw, "builtin_vars", true)
	if err != nil {
		return n, err
	}

	n.InheritVars, err = getBoolDef(raw, "inherit_vars", true)
	if err != nil {
		return n, err
	}

	if !n.InheritVars && len(n.Parsed.DefVars) > 0 {
		return n, fmt.Errorf("cannot use defvars when inherit_vars=false")
	}

	n.Parsed.Envs, err = getStringStringMapDef(raw, "envs", nil)
	if err != nil {
		return n, err
	}

	n.SystemEnvs, err = getBoolDef(raw, "system_envs", true)
	if err != nil {
		return n, err
	}

	n.InheritEnvs, err = getBoolDef(raw, "inherit_envs", true)
	if err != nil {
		return n, err
	}

	n.Parsed.XLocks, err = getStringArrayDef(raw, "xlocks", nil)
	if err != nil {
		return n, err
	}

	n.Parsed.RLocks, err = getStringArrayDef(raw, "rlocks", nil)
	if err != nil {
		return n, err
	}

	n.Parsed.Provides, err = getStringArrayDef(raw, "provides", nil)
	if err != nil {
		return n, err
	}

	n.Parsed.Requires, err = getStringArrayDef(raw, "requires", nil)
	if err != nil {
		return n, err
	}

	// max_proc defaults to the parent's max_proc value, for the root node it is -1
	dmp := -1
	if parent != nil {
		dmp = parent.MaxProc
	}
	n.MaxProc, err = getIntDef(raw, "max_proc", dmp)
	if err != nil {
		return n, err
	}

	st := "waiting"
	if parent != nil {
		st = strings.ToLower(StatusName(parent.Status))
	}
	st, err = getStringDef(raw, "status", st)
	if err != nil {
		return n, err
	}
	st = strings.TrimSpace(strings.ToLower(st))
	if st == "waiting" {
		n.Status = StatusWaiting
	} else if st == "frozen" {
		n.Status = StatusFrozen
	} else {
		return n, fmt.Errorf("invalid status: %s can only be waiting or frozen", st)
	}

	n.OnError, err = loadOnError(n, raw)
	if err != nil {
		return n, err
	}

	n.Expanded, err = getBoolDef(raw, "expanded", false)
	if err != nil {
		return n, err
	}

	n.ExpandOnActive, err = getBoolDef(raw, "expand_on_active", true)
	if err != nil {
		return n, err
	}

	n.CollapseOnFinished, err = getBoolDef(raw, "collapse_on_finished", true)
	if err != nil {
		return n, err
	}

	n.Parsed.Args, err = getStringArrayDef(raw, "args", nil)
	if err != nil {
		return n, err
	}

	n.Parsed.ArgsPrefix, err = getStringArrayDef(raw, "argsprefix", nil)
	if err != nil {
		return n, err
	}

	// Calculate all variables **before** loading sub-nodes.
	// It is crucial that you do not load/change additional node properties below this line,
	// except for adding new sub-nodes.
	if !n.calculate(idx, level) && loadInto == nil {
		return nil, nil
	}

	// if we want to load this node into another one, then we copy all props here
	if loadInto != nil {
		parent.loadFrom(n)
		*idx -= 1
		delete(n.Tree.NodeById, n.Id)
		n = parent
	} else {
		n.Nodes = make([]*Node, 0)
	}

	inc, hasInclude := raw["include"]
	nodes, hasNodes := raw["nodes"]

	if hasInclude && hasNodes {
		return n, fmt.Errorf("cannot specify both 'nodes' and 'include'")
	}

	forVarsRaw, hasForVars := raw["for_vars"]
	var forVars map[string][]string = nil
	if hasForVars {
		if n.Type == TypeRun {
			return n, fmt.Errorf("run type node cannot have for_vars")
		}

		err = mapstructure.Decode(forVarsRaw, &forVars)
		if err != nil {
			return n, fmt.Errorf("error parsing for_vars: %s", err)
		}
		hasForVars = forVars != nil && len(forVars) > 0
	}

	// create a technical for_var to avoid code duplication
	if !hasForVars {
		forVars = make(map[string][]string)
		forVars["_"] = []string{"_"}
	}

	// iterate over the cartesian product of variable values found in for_vars
	keys := make([]string, 0, len(forVars))
	for k := range forVars {
		keys = append(keys, k)
	}

	// e.g., if indices is [0, 1], it means the 0th element of keys[0] and 1st of keys[1]
	indices := make([]int, len(keys))

	for {
		combination := make(map[string]string)
		if hasForVars {
			for i, key := range keys {
				combination[key] = forVars[key][indices[i]]
			}
		}

		// Processing combination starts here
		if hasInclude {
			err = includeSubNodes(n, inc, tree, allTrees, filename, maxDepth, idx, level+1, combination)
			if err != nil {
				return n, fmt.Errorf("include %s: %w", inc, err)
			}
		}

		if hasNodes {
			err = loadSubNodes(n, nodes, tree, allTrees, filename, maxDepth, idx, level, combination)
			if err != nil {
				return n, err
			}
		}

		rl, ok := raw["seq"]
		if ok {
			n.Type = TypeSeq
			err = loadSubNodes(n, rl, tree, allTrees, filename, maxDepth, idx, level, combination)
			if err != nil {
				return n, err
			}
		}

		rl, ok = raw["par"]
		if ok {
			n.Type = TypePar
			err = loadSubNodes(n, rl, tree, allTrees, filename, maxDepth, idx, level, combination)
			if err != nil {
				return n, err
			}
		}
		// Processing combination ends here

		// 4. Increment indices to move to the next combination (like odometer/counter)
		next := len(keys) - 1
		for next >= 0 {
			indices[next]++

			// If the index is within bounds of the current slice, we are good to go
			if indices[next] < len(forVars[keys[next]]) {
				break
			}

			// If it overflows, reset this index to 0 and carry over to the previous key
			indices[next] = 0
			next--
		}

		// If 'next' goes below 0, it means all combinations have been exhausted
		if next < 0 {
			break
		}
	}

	for k, _ := range raw {
		ks, ok := k.(string)
		if !ok || !ValidNodeConfigs.Contains(ks) {
			return n, fmt.Errorf("invalid config: unknown property: %v", k)
		}
	}

	// TODO: run node self-check, raise error if there is a conflict/error in the settings
	// TODO: raise error if there is an unexpected setting in the YAML
	err = n.SelfCheck()
	if err != nil {
		return n, err
	}

	return n, nil

}

func loadOnError(n *Node, raw map[interface{}]interface{}) (OnError, error) {
	var err error
	var one []string = nil
	// read it as a string
	v, ok := raw["on_error"]
	if ok {
		v2, ok := v.(string)
		if ok {
			one = strings.Split(v2, ",")
		}
	}
	// read it as an array
	if one == nil {
		one, err = getStringArrayDef(raw, "on_error", nil)
	}
	// Is it specified?
	if err == nil && one == nil {
		// OnError was not specified
		if n.Parent == nil {
			// no parent -> return default
			return DefaultOnError, nil
		}
		// inherit from parent
		return n.Parent.OnError, nil
	}

	if err != nil {
		return DefaultOnError, err
	}
	failed, paused, success, none, skip, freeze := false, false, false, false, false, false
	stc, sbc := 0, 0
	for _, v := range one {
		v = strings.TrimSpace(strings.ToLower(v))
		switch v {
		case "failed":
			failed = true
			stc++
		case "paused":
			paused = true
			stc++
		case "success":
			success = true
			stc++
		case "none":
			none = true
			sbc++
		case "skip":
			skip = true
			sbc++
		case "freeze":
			freeze = true
			sbc++
		default:
			return DefaultOnError, fmt.Errorf("invalid value for on_error: %s", v)
		}
	}
	if stc > 1 {
		return DefaultOnError, fmt.Errorf("invalid value for on_error, cannot specify multiple statuses")
	}
	if sbc > 1 {
		return DefaultOnError, fmt.Errorf("invalid value for on_error, cannot specify multiple operations")
	}
	res := DefaultOnError
	if stc > 0 {
		if failed {
			res.Status = StatusFailed
		}
		if paused {
			res.Status = StatusPaused
		}
		if success {
			res.Status = StatusSuccess
		}
	}
	if sbc > 0 {
		if none {
			res.Siblings = OpNone
		}
		if skip {
			res.Siblings = OpSkip
		}
		if freeze {
			res.Siblings = OpFreeze
		}
	}
	return res, nil
}

// loadSubNodes loads sub-nodes when specified via "nodes" property
func loadSubNodes(parent *Node, rl interface{}, tree *Tree,
	allTrees map[string]map[string]interface{}, filename string,
	maxDepth uint, idx *uint, level uint, overrideVars map[string]string) error {
	l, ok := rl.([]interface{})
	if !ok {
		// TODO: allow use "seq", "par", "run" instead of "nodes"
		return fmt.Errorf("invalid node type: %T", l)
	}
	for _, rsub := range l {
		switch sub := rsub.(type) {
		case map[interface{}]interface{}:
			node, err := LoadNode(TypeRun, sub, parent, tree, allTrees, filename, maxDepth-1, idx, level+1, nil, overrideVars)
			if err != nil {
				return err
			}
			// ifeq, ifneq
			if node != nil {
				parent.Nodes = append(parent.Nodes, node)
			}
		case []interface{}:
			node, err := LoadRunNodeFromArgs(sub, parent, tree, allTrees, filename, maxDepth, idx, level+1, overrideVars)
			if err != nil {
				return err
			}
			// ifeq, ifneq
			if node != nil {
				parent.Nodes = append(parent.Nodes, node)
			}
		default:
			return fmt.Errorf("invalid node type: %T", rsub)
		}
	}
	return nil
}

// loadSubNodes includes sub-nodes when specified via "include" property
func includeSubNodes(parent *Node, rl interface{}, tree *Tree,
	allTrees map[string]map[string]interface{}, filename string,
	maxDepth uint, idx *uint, level uint, overrideVars map[string]string) error {

	var ok bool

	var sources []string

	source, ok := rl.(string)
	if ok {
		sources = make([]string, 1)
		sources[0] = source
	}
	if !ok {
		err := mapstructure.Decode(rl, &sources)
		if err != nil {
			return fmt.Errorf("invalid include argument: %v", err.Error())
		}
	}

	for _, name := range sources {
		var rawTrees map[string]interface{}
		var localName string

		// relative or absolute import?
		iidx := strings.Index(name, ":")
		if iidx < 0 {
			rawTrees = allTrees[filename]
			localName = name
		} else {
			filePath := name[:iidx]
			localName = name[iidx+1:]
			rawTrees, ok = allTrees[filePath]
			if !ok {
				var err error
				rawTrees, filename, err = AddTree(filePath, allTrees)
				if err != nil {
					return fmt.Errorf("cannot load %v: %v", name, err.Error())
				}
			}
		}
		item, ok := rawTrees[localName]
		if !ok {
			return fmt.Errorf("cannot include %s, not found", name)
		}
		raw, ok := item.(map[interface{}]interface{})
		if !ok {
			return fmt.Errorf("cannot include %s, should be an object, got %T", name, item)
		}

		// canReduce=true means that the parent only had a single "include" given with a single name,
		// in this case we load the node into the parent, instead of adding it as a child
		var loadInto *Node = nil
		canReduce := true
		if len(sources) != 1 {
			canReduce = false
		}
		if parent.Calculated.RLocks != nil && parent.Calculated.RLocks.Size() > 0 {
			canReduce = false
		}
		if parent.Calculated.XLocks != nil && parent.Calculated.XLocks.Size() > 0 {
			canReduce = false
		}
		if canReduce {
			loadInto = parent
		}
		node, err := LoadNode(TypeRun, raw, parent, tree, allTrees, filename, maxDepth-1, idx, level+1, loadInto, overrideVars)
		if err != nil {
			return err
		}
		// ifeq, ifneq
		if node == nil {
			continue
		}
		if loadInto == nil {
			parent.Nodes = append(parent.Nodes, node)
		}
	}
	return nil
}

func LoadRunNodeFromArgs(args []interface{}, parent *Node, tree *Tree,
	allTrees map[string]map[string]interface{}, filename string,
	maxDepth uint, idx *uint, level uint, overrideVars map[string]string) (*Node, error) {
	rl := make(map[interface{}]interface{})
	rl["args"] = args
	return LoadNode(TypeRun, rl, parent, tree, allTrees, filename, maxDepth-1, idx, level+1, nil, overrideVars)
}

func getString(raw map[interface{}]interface{}, name string) (string, error) {
	v, ok := raw[name]
	if !ok {
		return "", fmt.Errorf("missing %s", name)
	}
	v2, ok := v.(string)
	if !ok {
		return "", fmt.Errorf("invalid %s (expected string)", name)
	}
	return v2, nil
}

func getStringDef(raw map[interface{}]interface{}, name string, defval string) (string, error) {
	v, ok := raw[name]
	if !ok {
		return defval, nil
	}
	v2, ok := v.(string)
	if !ok {
		return "", fmt.Errorf("invalid %s (expected string)", name)
	}
	return v2, nil
}

// This is not used at this moment, but if you want to support non-string variable values then you are going to need it.
func getStringMapDef(raw map[interface{}]interface{}, name string, defval map[string]interface{}) (map[string]interface{}, error) {
	v, ok := raw[name]
	if !ok {
		return defval, nil
	}
	v2, ok := v.(map[interface{}]interface{})
	if !ok {
		return nil, fmt.Errorf("invalid %s (expected map of strings)", name)
	}
	res := make(map[string]interface{})
	for k, v := range v2 {
		k2, ok2 := k.(string)
		if !ok2 {
			return nil, fmt.Errorf("invalid key type in %s (expected string)", name)
		}
		res[k2] = v
	}
	return res, nil
}

func getStringStringMapDef(raw map[interface{}]interface{}, name string, defval map[string]string) (map[string]string, error) {
	v, ok := raw[name]
	if !ok {
		return defval, nil
	}
	v2, ok := v.(map[interface{}]interface{})
	if !ok {
		return nil, fmt.Errorf("invalid %s (expected map of strings)", name)
	}
	res := make(map[string]string)
	for k, v := range v2 {
		k2, ok2 := k.(string)
		if !ok2 {
			return nil, fmt.Errorf("invalid key type in %s (expected string)", name)
		}
		v2, ok2 := v.(string)
		if !ok2 {
			return nil, fmt.Errorf("invalid value type in %s (expected string)", name)
		}
		res[k2] = v2
	}
	return res, nil
}

func getBoolDef(raw map[interface{}]interface{}, name string, defval bool) (bool, error) {
	v, ok := raw[name]
	if !ok {
		return defval, nil
	}
	v2, ok := v.(bool)
	if ok {
		return v2, nil
	}
	v3, ok := v.(string)
	if ok {
		switch strings.TrimSpace(strings.ToLower(v3)) {
		case "true":
		case "yes":
		case "1":
		case "on":
			return true, nil
		case "false":
		case "no":
		case "0":
		case "off":
			return false, nil
		default:
			return false, fmt.Errorf("invalid boolean value in %s (expected bool)", name)
		}
	}
	v4, ok := v.(int)
	if !ok {
		return false, fmt.Errorf("invalid boolean value in %s (expected bool)", name)
	}
	return v4 != 0, nil
}

func getStringArrayDef(raw map[interface{}]interface{}, name string, defval []string) ([]string, error) {
	v, ok := raw[name]
	if !ok {
		return defval, nil
	}
	v2, ok := v.([]interface{})
	if !ok {
		return nil, fmt.Errorf("invalid value in %s (expected string array)", name)
	}
	res := make([]string, 0, len(v2))
	for _, v := range v2 {
		v2, ok := v.(string)
		if !ok {
			return nil, fmt.Errorf("invalid value in %s (expected string array)", name)
		}
		res = append(res, v2)
	}
	return res, nil
}

func getIntDef(raw map[interface{}]interface{}, name string, defval int) (int, error) {
	v, ok := raw[name]
	if !ok {
		return defval, nil
	}
	v2, ok := v.(int)
	if !ok {
		return 0, fmt.Errorf("invalid value in %s (expected int)", name)
	}
	return v2, nil
}

var nid int64

func nextId() string {
	nid++
	return fmt.Sprintf("n%d", nid)
}

/*
func randomId() string {
	return uuid.Must(uuid.NewRandom()).String()
}
*/

func (n *Node) SelfCheck() error {
	if n.Type == TypeRun {
		if n.Parsed.Args == nil || len(n.Parsed.Args) == 0 {
			// TODO: maybe, if args is empty but argsprefix is not empty, then we could allow it?
			return errors.New("selfcheck run node: no arguments")
		}
	}
	st := n.OnError.Status
	if st != StatusFailed && st != StatusPaused && st != StatusSuccess {
		return fmt.Errorf("invalid OnError.Status: %s", StatusName(n.OnError.Status))
	}
	es := n.OnError.Siblings
	if es != OpNone && es != OpSkip && es != OpFreeze {
		return fmt.Errorf("invalid OnError.Siblings: %s", OpName(n.OnError.Siblings))
	}

	return nil
}
