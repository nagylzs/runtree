package rt

import (
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/mitchellh/mapstructure"
	"github.com/nagylzs/set"
)

const MaxYamlFileSize = 1024 * 1024

// AddTree reads and parses a tree source file into sources, and returns its top level objects.
func AddTree(absoluteFilePath string, sources *Sources) (map[string]interface{}, string, error) {
	filename, err := filepath.Abs(absoluteFilePath)
	if err != nil {
		return nil, "", err
	}
	if filename != absoluteFilePath {
		return nil, "", fmt.Errorf("AddTree: invalid argument: relative file path provided %v", absoluteFilePath)
	}
	rawTrees, err := sources.AddFile(absoluteFilePath)
	if err != nil {
		return nil, absoluteFilePath, err
	}
	return rawTrees, absoluteFilePath, nil
}

func ParseToDom(sources *Sources, filePath string, maxDepth uint) (*Tree, error) {
	return LoadTree(sources, filePath, "tree", maxDepth)
}

// LoadNode loads a node from an arbitrary map of values and adds it into a tree.
// If the node should be excluded ("ifeq" and "ifneq") then it returns a nil node.
// The returned errors are ParseErrors, with the position of the problem in the source file.
func LoadNode(defType Type, raw map[interface{}]interface{}, parent *Node, tree *Tree,
	sources *Sources, filePath string,
	maxDepth uint, idx *uint, level uint, loadInto *Node, overrideVars map[string]interface{}) (n *Node, err error) {
	defer func() {
		if err != nil {
			err = locateError(err, sources, raw, n, parent)
		}
	}()
	if maxDepth == 0 {
		return nil, fmt.Errorf("maximum depth exceeded (are the includes circular?)")
	}

	for _, k := range sources.Keys(raw) {
		ks, ok := k.(string)
		if !ok {
			return nil, &keyError{name: fmt.Sprint(k), msg: fmt.Sprintf("property names must be strings, got %v", k)}
		}
		if !ValidNodeConfigs.Contains(ks) {
			return nil, unknownPropertyError(ks)
		}
	}

	n = &Node{
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
	_, hasSeq := raw["seq"]
	_, hasPar := raw["par"]
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
			return n, propErrorf("type", "invalid type: %s (it must be run, seq or par)", typ)
		}
	} else if hasSeq {
		n.Type = TypeSeq
	} else if hasPar {
		n.Type = TypePar
	} else {
		n.Type = defType
	}
	// TODO: document this, if a node does not specify but it specifies include, then the default
	// becomes Seq
	if hasInclude && !hasType {
		n.Type = TypeSeq
	}
	if hasInclude && n.Type == TypeRun {
		return n, &keyError{name: "include", msg: "run type nodes cannot include other nodes"}
	}

	n.Id, err = getStringDef(raw, "id", "")
	if err != nil {
		return n, err
	}
	if n.Id == "" {
		n.Id = nextId()
	}
	err = tree.addNodeById(n)
	if err != nil {
		return n, propErrorf("id", "%v", err)
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

	n.Parsed.Runner, err = getStringDef(raw, "runner", "")
	if err != nil {
		return n, err
	}

	n.Parsed.Vars, err = getStringMapDef(raw, "vars", overrideVars) /* getStringMapDef */
	if err != nil {
		return n, err
	}

	n.Parsed.DefVars, err = getStringMapDef(raw, "def_vars", nil) /* getStringMapDef */
	if err != nil {
		return n, err
	}

	if n.Parsed.Vars != nil && n.Parsed.DefVars != nil {
		for k := range n.Parsed.DefVars {
			_, has := n.Parsed.Vars[k]
			if has {
				return n, propErrorf("def_vars", "variable found both in vars and def_vars: %s", k)
			}
		}
	}

	n.Parsed.IfEq, err = getStringMapDef(raw, "ifeq", nil) /* getStringMapDef */
	if err != nil {
		return n, err
	}

	n.Parsed.IfNEq, err = getStringMapDef(raw, "ifneq", nil) /* getStringMapDef */
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
		return n, propErrorf("def_vars", "cannot use def_vars when inherit_vars=false")
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

	// success_exit_codes defaults to the parent's value, for the root node it is [0]
	dsc := []int{0}
	if parent != nil {
		dsc = parent.SuccessExitCodes
	}
	n.SuccessExitCodes, err = getExitCodesDef(raw, "success_exit_codes", dsc)
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
		return n, propErrorf("status", "invalid status: %s can only be waiting or frozen", st)
	}

	n.OnError, err = loadOnError(n, raw)
	if err != nil {
		return n, err
	}
	n.ChildrenOnError, err = loadChildrenOnError(n, raw)
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

	n.Parsed.ArgsPrefix, err = getStringArrayDef(raw, "args_prefix", nil)
	if err != nil {
		return n, err
	}

	// Calculate all variables **before** loading sub-nodes.
	// It is crucial that you do not load/change additional node properties below this line,
	// except for adding new sub-nodes.
	ok, err := n.calculate(idx, level)
	if err != nil {
		return n, err
	}
	if !ok && loadInto == nil {
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
		return n, &keyError{name: "include", msg: "cannot specify both 'nodes' and 'include'"}
	}

	hasForVars, forVars, err := caclForVars(raw, n, err)
	if err != nil {
		return n, err
	}

	// iterate over the cartesian product of variable values found in for_vars
	keys := forVarsKeys(sources, raw, forVars)

	// e.g., if indices is [0, 1], it means the 0th element of keys[0] and 1st of keys[1]
	indices := make([]int, len(keys))

	for {
		combination := make(map[string]interface{})
		if hasForVars {
			for i, key := range keys {
				combination[key] = forVars[key][indices[i]]
			}
		}

		// Processing combination starts here
		if hasInclude {
			err = includeSubNodes(n, inc, raw, tree, sources, filePath, maxDepth, idx, level+1, combination, hasForVars)
			if err != nil {
				return n, err
			}
		}

		if hasNodes {
			err = loadSubNodes(n, "nodes", nodes, tree, sources, filePath, maxDepth, idx, level, combination)
			if err != nil {
				return n, err
			}
		}

		rl, ok := raw["seq"]
		if ok {
			n.Type = TypeSeq
			err = loadSubNodes(n, "seq", rl, tree, sources, filePath, maxDepth, idx, level, combination)
			if err != nil {
				return n, err
			}
		}

		rl, ok = raw["par"]
		if ok {
			n.Type = TypePar
			err = loadSubNodes(n, "par", rl, tree, sources, filePath, maxDepth, idx, level, combination)
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

	err = n.SelfCheck()
	if err != nil {
		return n, err
	}

	return n, nil

}

// forVarsKeys returns the for_vars variable names in the order they are written in the source, so that the first
// one changes the slowest (like nested loops). Without a source order (or for the fake for_vars), they are sorted.
func forVarsKeys(sources *Sources, raw map[interface{}]interface{}, forVars map[string][]interface{}) []string {
	keys := make([]string, 0, len(forVars))
	if m, ok := raw["for_vars"].(map[interface{}]interface{}); ok {
		for _, k := range sources.Keys(m) {
			if ks, ok := k.(string); ok {
				if _, ok := forVars[ks]; ok {
					keys = append(keys, ks)
				}
			}
		}
	}
	if len(keys) != len(forVars) {
		keys = keys[:0]
		for k := range forVars {
			keys = append(keys, k)
		}
		sort.Strings(keys)
	}
	return keys
}

// calculate for_vars
// when for_vars is not given; then it returns false with a fake map containing a single fake var with a single value
func caclForVars(raw map[interface{}]interface{}, n *Node, err error) (bool, map[string][]interface{}, error) {
	forVarsRaw, hasForVars := raw["for_vars"]

	if !hasForVars {
		forVars := make(map[string][]interface{})
		forVars["_"] = []interface{}{"_"}
		return false, forVars, nil
	}

	var forVars map[string]interface{} = nil
	if n.Type == TypeRun {
		return false, nil, &keyError{name: "for_vars", msg: "run type node cannot have for_vars"}
	}

	err = mapstructure.Decode(forVarsRaw, &forVars)
	if err != nil {
		return false, nil, propErrorf("for_vars", "error parsing for_vars: %s", err)
	}
	hasForVars = forVars != nil && len(forVars) > 0

	// Now, replace variable references to array values with the actual arrays.
	forVarsFinal := make(map[string][]interface{})
	for k, v := range forVars {
		var av []interface{}
		avOk := false

		sv, ok := v.(string)
		if ok {
			v2, ok := n.Calculated.Vars[sv]
			if !ok {
				return false, nil, propErrorf("for_vars", "for_vars: variable %s->%s not found", k, sv)
			}
			av, ok = v2.([]interface{})
			if !ok {
				return false, nil, propErrorf("for_vars", "for_vars: variable %s->%s is not an array", k, sv)
			}
			avOk = true
		}

		if !avOk {
			av, avOk = v.([]interface{})
		}

		if !avOk {
			return false, nil,
				propErrorf("for_vars", "for_vars: %v: value must be an array of strings or the name of an array variable", k)
		}

		forVarsFinal[k] = av
	}

	return hasForVars, forVarsFinal, nil
}

// loadOnError returns the on_error property of the node. When it is not specified, then it is inherited from the
// parent (its children_on_error, or its on_error), the default is DefaultOnError.
func loadOnError(n *Node, raw map[interface{}]interface{}) (OnError, error) {
	oe, specified, err := parseOnError(raw, "on_error")
	if err != nil || specified {
		return oe, err
	}
	if n.Parent == nil {
		return DefaultOnError, nil
	}
	return n.Parent.ChildrenOnError, nil
}

// loadChildrenOnError returns the on_error value that the sub-nodes of the node inherit: its children_on_error
// property, or its on_error when children_on_error is not specified. It must be called after loadOnError.
func loadChildrenOnError(n *Node, raw map[interface{}]interface{}) (OnError, error) {
	oe, specified, err := parseOnError(raw, "children_on_error")
	if err != nil || specified {
		return oe, err
	}
	return n.OnError, nil
}

// parseOnError parses an on_error value (a string or a list of strings: a status and/or a sibling operation),
// it also tells if it was specified.
func parseOnError(raw map[interface{}]interface{}, key string) (OnError, bool, error) {
	var err error
	var one []string = nil
	// read it as a string
	v, ok := raw[key]
	if ok {
		v2, ok := v.(string)
		if ok {
			one = strings.Split(v2, ",")
		}
	}
	// read it as an array
	if one == nil {
		one, err = getStringArrayDef(raw, key, nil)
	}
	if err != nil {
		return DefaultOnError, true, err
	}
	if one == nil {
		return DefaultOnError, false, nil
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
			return DefaultOnError, true, propErrorf(key, "invalid value for %s: %s", key, v)
		}
	}
	if stc > 1 {
		return DefaultOnError, true, propErrorf(key, "invalid value for %s, cannot specify multiple statuses", key)
	}
	if sbc > 1 {
		return DefaultOnError, true, propErrorf(key, "invalid value for %s, cannot specify multiple operations", key)
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
	return res, true, nil
}

// loadSubNodes loads sub-nodes from a list (the value of the key property: nodes, seq or par)
func loadSubNodes(parent *Node, key string, rl interface{}, tree *Tree,
	sources *Sources, filePath string,
	maxDepth uint, idx *uint, level uint, overrideVars map[string]interface{}) error {
	l, ok := rl.([]interface{})
	if !ok {
		return propErrorf(key, "the value of %s must be a list of nodes, got %T", key, rl)
	}
	for i, rsub := range l {
		var node *Node
		var err error
		switch sub := rsub.(type) {
		case map[interface{}]interface{}:
			node, err = LoadNode(TypeRun, sub, parent, tree, sources, filePath, maxDepth-1, idx, level+1, nil, overrideVars)
		case []interface{}:
			node, err = LoadRunNodeFromArgs(sub, sources.ItemPos(l, i), parent, tree, sources, filePath, maxDepth, idx,
				level+1, overrideVars)
		default:
			return &ParseError{Pos: sources.ItemPos(l, i), Node: nodePath(parent),
				Message: fmt.Sprintf("a node must be an object, or a list of arguments, got %T", rsub)}
		}
		if err != nil {
			return err
		}
		// ifeq, ifneq
		if node != nil {
			parent.Nodes = append(parent.Nodes, node)
		}
	}
	return nil
}

// includeSubNodes includes sub-nodes when specified via "include" property (rl is its value in raw)
func includeSubNodes(parent *Node, rl interface{}, raw map[interface{}]interface{}, tree *Tree,
	sources *Sources, filePath string,
	maxDepth uint, idx *uint, level uint, overrideVars map[string]interface{},
	preventReduce bool) error {

	var names []string
	var itemPos func(i int) Pos
	if name, ok := rl.(string); ok {
		names = []string{name}
		itemPos = func(int) Pos { return sources.ValuePos(raw, "include") }
	} else {
		err := mapstructure.Decode(rl, &names)
		if err != nil {
			return propErrorf("include", "invalid include argument (expected a string or a list of strings): %v", err)
		}
		l, _ := rl.([]interface{})
		itemPos = func(i int) Pos { return sources.ItemPos(l, i) }
	}

	for i, name := range names {
		var rawTrees map[string]interface{}
		var includeFilePath string
		var localName string
		incPos := itemPos(i)
		includeError := func(format string, args ...any) error {
			return &ParseError{Pos: incPos, Message: fmt.Sprintf(format, args...), Node: nodePath(parent)}
		}

		// relative or absolute import?
		iidx := strings.Index(name, "|")
		if iidx < 0 {
			rawTrees = sources.Files[filePath]
			includeFilePath = filePath
			localName = name
		} else {
			// relative to the file that contains the include
			includeFilePath = filepath.Join(filepath.Dir(filePath), name[:iidx])
			localName = name[iidx+1:]
			var err error
			rawTrees, err = sources.AddFile(includeFilePath)
			if err != nil {
				var pe *ParseError
				if errors.As(err, &pe) && pe.Pos.Line > 0 {
					// an error inside the included file
					pe.Included = append(pe.Included, IncludePos{Pos: incPos, Name: name})
					return pe
				}
				return includeError("cannot include %s: %s: %s", name, displayPath(includeFilePath), errorMessage(err))
			}
		}
		item, ok := rawTrees[localName]
		if !ok {
			return includeError("cannot include %s: %q is not found in %s", name, localName, displayPath(includeFilePath))
		}
		itemRaw, ok := item.(map[interface{}]interface{})
		if !ok {
			return includeError("cannot include %s: it should be an object, got %T", name, item)
		}

		// canReduce=true means that the parent only had a single "include" given with a single name,
		// in this case we load the node into the parent, instead of adding it as a child
		var loadInto *Node = nil
		canReduce := !preventReduce
		if len(names) != 1 {
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
		node, err := LoadNode(TypeRun, itemRaw, parent, tree, sources, includeFilePath, maxDepth-1, idx, level+1, loadInto,
			overrideVars)
		if err != nil {
			var pe *ParseError
			if errors.As(err, &pe) {
				pe.Included = append(pe.Included, IncludePos{Pos: incPos, Name: name})
			}
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

// errorMessage returns the message of a ParseError (without its position), or the error string
func errorMessage(err error) string {
	var pe *ParseError
	if errors.As(err, &pe) {
		return pe.Message
	}
	return err.Error()
}

// LoadRunNodeFromArgs loads a run node that is given as a list of arguments (at pos)
func LoadRunNodeFromArgs(args []interface{}, pos Pos, parent *Node, tree *Tree,
	sources *Sources, filePath string,
	maxDepth uint, idx *uint, level uint, overrideVars map[string]interface{}) (*Node, error) {
	rl := make(map[interface{}]interface{})
	rl["args"] = args
	sources.setMapPos(rl, pos, "args")
	return LoadNode(TypeRun, rl, parent, tree, sources, filePath, maxDepth-1, idx, level+1, nil, overrideVars)
}

func getString(raw map[interface{}]interface{}, name string) (string, error) {
	v, ok := raw[name]
	if !ok {
		return "", fmt.Errorf("missing property %s", name)
	}
	v2, ok := v.(string)
	if !ok {
		return "", propErrorf(name, "invalid %s (expected string)", name)
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
		return "", propErrorf(name, "invalid %s (expected string)", name)
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
		return nil, propErrorf(name, "invalid %s (expected map of strings)", name)
	}
	res := make(map[string]interface{})
	for k, v := range v2 {
		k2, ok2 := k.(string)
		if !ok2 {
			return nil, propErrorf(name, "invalid key type in %s (expected string)", name)
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
		return nil, propErrorf(name, "invalid %s (expected map of strings)", name)
	}
	res := make(map[string]string)
	for k, v := range v2 {
		k2, ok2 := k.(string)
		if !ok2 {
			return nil, propErrorf(name, "invalid key type in %s (expected string)", name)
		}
		v2, ok2 := v.(string)
		if !ok2 {
			return nil, propErrorf(name, "invalid value type in %s (expected string)", name)
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
		// YAML 1.2 (yaml.v3) gives these as strings, YAML 1.1 used them as booleans
		switch strings.TrimSpace(strings.ToLower(v3)) {
		case "true", "yes", "y", "1", "on":
			return true, nil
		case "false", "no", "n", "0", "off":
			return false, nil
		default:
			return false, propErrorf(name, "invalid boolean value in %s (expected bool)", name)
		}
	}
	v4, ok := v.(int)
	if !ok {
		return false, propErrorf(name, "invalid boolean value in %s (expected bool)", name)
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
		return nil, propErrorf(name, "invalid value in %s (expected string array)", name)
	}
	res := make([]string, 0, len(v2))
	for _, v := range v2 {
		v2, ok := v.(string)
		if !ok {
			return nil, propErrorf(name, "invalid value in %s (expected string array)", name)
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
		return 0, propErrorf(name, "invalid value in %s (expected int)", name)
	}
	return v2, nil
}

// getExitCodesDef returns a non-empty list of process exit codes (0-255)
func getExitCodesDef(raw map[interface{}]interface{}, name string, defval []int) ([]int, error) {
	v, ok := raw[name]
	if !ok {
		return defval, nil
	}
	v2, ok := v.([]interface{})
	if !ok || len(v2) == 0 {
		return nil, propErrorf(name, "invalid value in %s (expected a list of exit codes)", name)
	}
	res := make([]int, 0, len(v2))
	for _, v := range v2 {
		code, ok := v.(int)
		if !ok || code < 0 || code > 255 {
			return nil, propErrorf(name, "invalid value in %s: %v (exit codes are integers between 0 and 255)", name, v)
		}
		res = append(res, code)
	}
	return res, nil
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
			// TODO: maybe, if args is empty but args_prefix is not empty, then we could allow it?
			return &keyError{name: "args", msg: "a run node needs args"}
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
