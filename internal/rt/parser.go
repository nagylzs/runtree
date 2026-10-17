package rt

import (
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

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
	// Errors are reported (to sources), and loading continues with a default value, so that all errors of the tree
	// are found. The tree can only be used when there are no errors.
	reportedBefore := sources.reported
	// the path of the node, as it will be in the tree: a node that is loaded into its parent replaces it
	pathOf := func() string {
		if loadInto != nil && n != nil && n != parent && nodeLabel(n) != "" {
			return joinPath(nodePath(parent.Parent), nodeLabel(n))
		}
		return nodePath(n)
	}
	report := func(err error) {
		err = locateError(err, sources, raw, n, parent)
		var pe *ParseError
		if errors.As(err, &pe) && loadInto != nil && n != nil && n != parent && nodeLabel(n) != "" {
			pe.Node = pathOf()
		}
		sources.report(err)
	}
	// warnings about the references to undefined variables
	reportUndefined := func() {
		for _, r := range n.undefinedRefs {
			msg := fmt.Sprintf("undefined variable {%s}", r.name)
			if parent != nil && !n.InheritVars {
				if _, ok := parent.Calculated.Vars[r.name]; ok {
					msg += " (it is not inherited, because inherit_vars is false)"
				}
			} else if _, ok := n.Parsed.Vars[r.name]; ok && r.prop == "def_vars" {
				msg += " (def_vars cannot use the vars of the same node)"
			} else if sources.isIncluded() {
				msg += ", it is not set where it is included"
			}
			sources.warn(&ParseError{Pos: sources.refPos(raw, r), Message: msg, Node: pathOf()})
		}
		n.undefinedRefs = nil
	}
	if maxDepth == 0 {
		report(fmt.Errorf("maximum depth exceeded (are the includes circular?)"))
		return nil, nil
	}

	for _, k := range sources.Keys(raw) {
		ks, ok := k.(string)
		if !ok {
			report(&keyError{name: fmt.Sprint(k), msg: fmt.Sprintf("property names must be strings, got %v", k)})
		} else if !ValidNodeConfigs.Contains(ks) {
			report(unknownPropertyError(ks))
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
		typ = strings.TrimSpace(strings.ToLower(typ))
		switch {
		case err != nil:
			report(err)
			n.Type = defType
		case typ == "run":
			n.Type = TypeRun
		case typ == "seq":
			n.Type = TypeSeq
		case typ == "par":
			n.Type = TypePar
		default:
			report(propErrorf("type", "invalid type: %q (it must be run, seq or par)", typ))
			n.Type = defType
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
		report(&keyError{name: "include", msg: "run type nodes cannot include other nodes"})
		return nil, nil
	}

	// the title is loaded first, so that errors about the id can show the title
	n.Parsed.Title, err = getStringDef(raw, "title", "")
	if err != nil {
		report(err)
	}

	n.Id, err = getStringDef(raw, "id", "")
	if err != nil {
		report(err)
	}
	if n.Id == "" {
		n.Id, n.generatedId = tree.newId(), true
	}
	err = tree.addNodeById(n)
	if err != nil {
		report(propErrorf("id", "%v", err))
		n.Id, n.generatedId = tree.newId(), true // so that the node can be registered
		_ = tree.addNodeById(n)
	}

	n.Parsed.Description, err = getStringDef(raw, "description", "")
	if err != nil {
		report(err)
	}

	n.Parsed.CWD, err = getStringDef(raw, "cwd", "")
	if err != nil {
		report(err)
	}

	n.Parsed.Runner, err = getStringDef(raw, "runner", "")
	if err != nil {
		report(err)
	}

	n.Parsed.Vars, err = getStringMapDef(raw, "vars", nil) /* getStringMapDef */
	if err != nil {
		report(err)
	}
	n.Parsed.LoopVars = overrideVars

	n.Parsed.DefVars, err = getStringMapDef(raw, "def_vars", nil) /* getStringMapDef */
	if err != nil {
		report(err)
	}

	if n.Parsed.Vars != nil && n.Parsed.DefVars != nil {
		for k := range n.Parsed.DefVars {
			_, has := n.Parsed.Vars[k]
			if has {
				report(propErrorf("def_vars", "variable found both in vars and def_vars: %s", k))
			}
		}
	}

	n.Parsed.IfEq, err = getStringMapDef(raw, "ifeq", nil) /* getStringMapDef */
	if err != nil {
		report(err)
	}

	n.Parsed.IfNEq, err = getStringMapDef(raw, "ifneq", nil) /* getStringMapDef */
	if err != nil {
		report(err)
	}

	n.InheritVars, err = getBoolDef(raw, "inherit_vars", true)
	if err != nil {
		report(err)
	}

	if !n.InheritVars && len(n.Parsed.DefVars) > 0 {
		report(propErrorf("def_vars", "cannot use def_vars when inherit_vars=false"))
	}

	n.Parsed.Envs, err = getStringStringMapDef(raw, "envs", nil)
	if err != nil {
		report(err)
	}

	n.SystemEnvs, err = getBoolDef(raw, "system_envs", true)
	if err != nil {
		report(err)
	}

	n.InheritEnvs, err = getBoolDef(raw, "inherit_envs", true)
	if err != nil {
		report(err)
	}

	n.Parsed.XLocks, err = getStringArrayDef(raw, "xlocks", nil)
	if err != nil {
		report(err)
	}

	n.Parsed.RLocks, err = getStringArrayDef(raw, "rlocks", nil)
	if err != nil {
		report(err)
	}

	n.Parsed.Provides, err = getStringArrayDef(raw, "provides", nil)
	if err != nil {
		report(err)
	}

	n.Parsed.Requires, err = getStringArrayDef(raw, "requires", nil)
	if err != nil {
		report(err)
	}

	// max_proc defaults to the parent's max_proc value, for the root node it is -1
	dmp := -1
	if parent != nil {
		dmp = parent.MaxProc
	}
	n.MaxProc, err = getIntDef(raw, "max_proc", dmp)
	if err != nil {
		report(err)
	}

	// success_exit_codes defaults to the parent's value, for the root node it is [0]
	dsc := []int{0}
	if parent != nil {
		dsc = parent.SuccessExitCodes
	}
	n.SuccessExitCodes, err = getExitCodesDef(raw, "success_exit_codes", dsc)
	if err != nil {
		report(err)
	}

	st := "waiting"
	if parent != nil {
		st = strings.ToLower(StatusName(parent.Status))
	}
	st, err = getStringDef(raw, "status", st)
	if err != nil {
		report(err)
	}
	st = strings.TrimSpace(strings.ToLower(st))
	if st == "waiting" {
		n.Status = StatusWaiting
	} else if st == "frozen" {
		n.Status = StatusFrozen
	} else {
		report(propErrorf("status", "invalid status: %q (it can only be waiting or frozen)", st))
		n.Status = StatusWaiting
	}

	n.OnError, err = loadOnError(n, raw)
	if err != nil {
		report(err)
	}
	n.ChildrenOnError, err = loadChildrenOnError(n, raw)
	if err != nil {
		report(err)
	}

	n.Expanded, err = getBoolDef(raw, "expanded", false)
	if err != nil {
		report(err)
	}

	n.ExpandOnActive, err = getBoolDef(raw, "expand_on_active", true)
	if err != nil {
		report(err)
	}

	n.CollapseOnFinished, err = getBoolDef(raw, "collapse_on_finished", true)
	if err != nil {
		report(err)
	}

	n.Parsed.Args, err = getStringArrayDef(raw, "args", nil)
	if err != nil {
		report(err)
	}

	n.Parsed.ArgsPrefix, err = getStringArrayDef(raw, "args_prefix", nil)
	if err != nil {
		report(err)
	}

	// Calculate all variables **before** loading sub-nodes.
	// It is crucial that you do not load/change additional node properties below this line,
	// except for adding new sub-nodes.
	ok, err := n.calculate(idx, level)
	if err != nil {
		// the variables are needed for the sub-nodes too, so they are not loaded
		report(err)
		return nil, nil
	}
	reportUndefined()
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
		report(&keyError{name: "include", msg: "cannot specify both 'nodes' and 'include'"})
		return n, nil
	}

	hasForVars, forVars, err := caclForVars(raw, n, err)
	if err != nil {
		report(err)
	}
	reportUndefined()

	// iterate over the cartesian product of variable values found in for_vars
	keys := forVarsKeys(sources, raw, forVars)

	// e.g., if indices is [0, 1], it means the 0th element of keys[0] and 1st of keys[1]
	indices := make([]int, len(keys))

	// there are no combinations when a list is empty
	noCombinations := false
	for _, key := range keys {
		if len(forVars[key]) == 0 {
			m, _ := raw["for_vars"].(map[interface{}]interface{})
			sources.warn(&ParseError{Pos: sources.ValuePos(m, key), Node: nodePath(n),
				Message: fmt.Sprintf("for_vars: %s is an empty list, there are no sub-nodes", key)})
			noCombinations = true
		}
	}

	for !noCombinations {
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
				report(err)
			}
		}

		if hasNodes {
			err = loadSubNodes(n, "nodes", nodes, tree, sources, filePath, maxDepth, idx, level, combination)
			if err != nil {
				report(err)
			}
		}

		rl, ok := raw["seq"]
		if ok {
			n.Type = TypeSeq
			err = loadSubNodes(n, "seq", rl, tree, sources, filePath, maxDepth, idx, level, combination)
			if err != nil {
				report(err)
			}
		}

		rl, ok = raw["par"]
		if ok {
			n.Type = TypePar
			err = loadSubNodes(n, "par", rl, tree, sources, filePath, maxDepth, idx, level, combination)
			if err != nil {
				report(err)
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

	// it would only repeat the consequences of the errors that were already reported (in this node, its sub-nodes,
	// or in the node that was loaded into this one)
	if sources.reported == reportedBefore {
		if err := n.SelfCheck(); err != nil {
			report(err)
		}
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

	m, ok := forVarsRaw.(map[interface{}]interface{})
	if !ok {
		return false, nil, propErrorf("for_vars", "invalid for_vars: it is %s, expected an object (variable: values)",
			describeValue(forVarsRaw))
	}
	forVars = make(map[string]interface{}, len(m))
	for k, v := range m {
		ks, ok := k.(string)
		if !ok {
			return false, nil, propErrorf("for_vars", "invalid for_vars: variable names must be strings, got %v", k)
		}
		forVars[ks] = v
	}
	hasForVars = forVars != nil && len(forVars) > 0

	// Now, replace variable references to array values with the actual arrays.
	forVarsFinal := make(map[string][]interface{})
	for k, v := range forVars {
		var av []interface{}
		avOk := false
		inline := false // a list variable is calculated already

		sv, ok := v.(string)
		if ok {
			v2, ok := n.Calculated.Vars[sv]
			if !ok {
				return false, nil, propErrorf("for_vars", "invalid for_vars: %s: there is no variable %q", k, sv)
			}
			av, ok = v2.([]interface{})
			if !ok {
				return false, nil, propErrorf("for_vars", "invalid for_vars: %s: variable %q is %s, expected a list", k,
					sv, describeValue(v2))
			}
			avOk = true
		}

		if !avOk {
			av, avOk = v.([]interface{})
			inline = avOk
		}

		if !avOk {
			return false, nil,
				propErrorf("for_vars", "invalid for_vars: %v: it is %s, expected a list of strings or the name of a "+
					"list variable", k, describeValue(v))
		}

		// the values can contain the variables of this node
		values := make([]interface{}, len(av))
		for i, item := range av {
			s, ok := item.(string)
			if !ok {
				return false, nil, propErrorf("for_vars", "invalid for_vars: %v: item %d is %s, expected a string", k, i+1,
					describeValue(item))
			}
			if !inline {
				values[i] = s
				continue
			}
			ref := undefinedRef{prop: "for_vars", key: k, item: i}
			values[i], err = varEvalUndefined(s, n.Calculated.Vars, "", -1, func(_ string, _ int, name string) {
				ref.name = name
				n.undefinedRefs = append(n.undefinedRefs, ref)
			})
			if err != nil {
				return false, nil, propErrorf("for_vars", "invalid for_vars: %v: %v", k, err)
			}
		}
		forVarsFinal[k] = values
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
		switch v2 := v.(type) {
		case string:
			one = strings.Split(v2, ",")
		case []interface{}:
		default:
			return DefaultOnError, true, propErrorf(key, "invalid %s: it is %s, expected a status and/or an "+
				"operation, e.g. \"paused, skip\"", key, describeValue(v))
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
			return DefaultOnError, true, propErrorf(key, "invalid %s: %q (expected a status: failed, paused or "+
				"success, and/or an operation: none, skip or freeze)", key, v)
		}
	}
	if stc > 1 {
		return DefaultOnError, true, propErrorf(key, "invalid %s: it cannot have more than one status", key)
	}
	if sbc > 1 {
		return DefaultOnError, true, propErrorf(key, "invalid %s: it cannot have more than one operation", key)
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
		return propErrorf(key, "invalid %s: it is %s, expected a list of nodes", key, describeValue(rl))
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
			sources.report(&ParseError{Pos: sources.ItemPos(l, i), Node: nodePath(parent),
				Message: fmt.Sprintf("a node must be an object, or a list of arguments, got %T", rsub)})
			continue
		}
		if err != nil {
			sources.report(err)
			continue
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
		l, ok := rl.([]interface{})
		if !ok {
			return propErrorf("include", "invalid include: it is %s, expected an object name or a list of them",
				describeValue(rl))
		}
		for i, item := range l {
			name, ok := item.(string)
			if !ok {
				return itemErrorf("include", i, "invalid include: item %d is %s, expected an object name", i+1,
					describeValue(item))
			}
			names = append(names, name)
		}
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
					sources.pushInclude(IncludePos{Pos: incPos, Name: name})
					sources.report(pe)
					sources.popInclude()
				} else {
					sources.report(includeError("cannot include %s: %s: %s", name, displayPath(includeFilePath),
						errorMessage(err)))
				}
				continue
			}
		}
		item, ok := rawTrees[localName]
		if !ok {
			sources.report(includeError("cannot include %s: %q is not found in %s", name, localName,
				displayPath(includeFilePath)))
			continue
		}
		itemRaw, ok := item.(map[interface{}]interface{})
		if !ok {
			sources.report(includeError("cannot include %s: it should be an object, got %T", name, item))
			continue
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
		sources.pushInclude(IncludePos{Pos: incPos, Name: name})
		node, err := LoadNode(TypeRun, itemRaw, parent, tree, sources, includeFilePath, maxDepth-1, idx, level+1, loadInto,
			overrideVars)
		if err != nil {
			sources.report(err)
		}
		sources.popInclude()
		if err != nil {
			continue
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
		return "", propErrorf(name, "invalid %s: it is %s, expected a string", name, describeValue(v))
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
		return "", propErrorf(name, "invalid %s: it is %s, expected a string", name, describeValue(v))
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
		return nil, propErrorf(name, "invalid %s: it is %s, expected an object (name: value)", name,
			describeValue(v))
	}
	res := make(map[string]interface{})
	for k, v := range v2 {
		k2, ok2 := k.(string)
		if !ok2 {
			return nil, propErrorf(name, "invalid %s: the names must be strings, got %v", name, k)
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
		return nil, propErrorf(name, "invalid %s: it is %s, expected an object (name: value)", name,
			describeValue(v))
	}
	res := make(map[string]string)
	for k, v := range v2 {
		k2, ok2 := k.(string)
		if !ok2 {
			return nil, propErrorf(name, "invalid %s: the names must be strings, got %v", name, k)
		}
		v2, ok2 := v.(string)
		if !ok2 {
			msg := "invalid %s: the value of %s is %s, expected a string"
			switch v.(type) {
			case bool, int, int64, uint64, float64:
				msg += " (put it in quotes)"
			}
			return nil, &propertyError{name: name, key: k2, item: -1,
				msg: fmt.Sprintf(msg, name, k2, describeValue(v))}
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
			return false, propErrorf(name, "invalid %s: %q (expected true or false)", name, v3)
		}
	}
	v4, ok := v.(int)
	if !ok {
		return false, propErrorf(name, "invalid %s: it is %s, expected true or false", name, describeValue(v))
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
		return nil, propErrorf(name, "invalid %s: it is %s, expected a list of strings", name, describeValue(v))
	}
	res := make([]string, 0, len(v2))
	for i, v := range v2 {
		v2, ok := v.(string)
		if !ok {
			msg := "invalid %s: item %d is %s, expected a string"
			switch v.(type) {
			case bool, int, int64, uint64, float64:
				msg += " (put it in quotes)"
			}
			return nil, itemErrorf(name, i, msg, name, i+1, describeValue(v))
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
		return 0, propErrorf(name, "invalid %s: it is %s, expected an integer", name, describeValue(v))
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
		return nil, propErrorf(name, "invalid %s: it is %s, expected a list of exit codes", name, describeValue(v))
	}
	res := make([]int, 0, len(v2))
	for i, v := range v2 {
		code, ok := v.(int)
		if !ok || code < 0 || code > 255 {
			return nil, itemErrorf(name, i, "invalid %s: %v (exit codes are integers between 0 and 255)", name, v)
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
