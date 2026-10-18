package rt

import (
	"errors"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strings"

	"github.com/nagylzs/runtree/internal/runner"
	"github.com/nagylzs/set"
)

const StartTag = "{"
const EndTag = "}"

// undefinedRef is a reference ({name}) to a variable that is not set, in a property value of the node
type undefinedRef struct {
	prop string // the property
	key  string // the key in an object value (vars, envs, for_vars, ...), or ""
	item int    // the index of the item in a list value (of the key), or -1
	name string // the variable name
}

func (n *Node) calculate(idx *uint, level uint) (bool, error) {
	c := &Calculated{Idx: *idx, Level: level}
	n.undefinedRefs = nil
	// records the references to undefined variables in prop (key, item)
	undefined := func(prop, key string, item int) undefinedFunc {
		return func(valueKey string, valueItem int, name string) {
			k, i := key, item
			if k == "" {
				k = valueKey
			}
			if i < 0 {
				i = valueItem
			}
			n.undefinedRefs = append(n.undefinedRefs, undefinedRef{prop: prop, key: k, item: i, name: name})
		}
	}
	eval := func(prop string, item int, value string, values map[string]interface{}) (string, error) {
		res, err := varEvalUndefined(value, values, "", -1, undefined(prop, "", item))
		if err != nil {
			return "", &propertyError{name: prop, item: item, msg: err.Error()}
		}
		return res, nil
	}
	evalArray := func(prop string, items []string) ([]string, error) {
		res := make([]string, len(items))
		for i, v := range items {
			sv, err := eval(prop, i, v, c.Vars)
			if err != nil {
				return nil, err
			}
			res[i] = sv
		}
		return res, nil
	}
	evalSet := func(prop string, items []string) (*set.Set[string], error) {
		arr, err := evalArray(prop, items)
		if err != nil {
			return nil, err
		}
		return set.FromArray(arr), nil
	}

	// Inherit from this
	iv := make(map[string]interface{})
	if n.Parent != nil && n.InheritVars {
		for k, v := range n.Parent.Calculated.Vars {
			iv[k] = v
		}
	}
	// the for_vars variables of the parent are like inherited ones: the vars of the node override them, and def_vars
	// are not used for them
	for k, v := range n.Parsed.LoopVars {
		iv[k] = v
	}

	var err error
	c.Vars, err = calcVars(n.Parsed.Vars, n.Parsed.DefVars, iv, undefined("vars", "", -1),
		undefined("def_vars", "", -1))
	if err != nil {
		return false, err
	}
	// below this line, variable evaluation should use c.Vars and **not** n.Parsed.Vars!
	c.VarSources = make(map[string]VarSource, len(c.Vars))
	for k := range c.Vars {
		_, own := n.Parsed.Vars[k]
		_, loop := n.Parsed.LoopVars[k]
		var inherited bool
		if n.Parent != nil && n.InheritVars {
			_, inherited = n.Parent.Calculated.Vars[k]
		}
		switch {
		case own:
			c.VarSources[k] = VarSource{Node: n, Prop: "vars"}
		case loop:
			c.VarSources[k] = VarSource{Node: n.Parent, Prop: "for_vars"}
		case inherited:
			c.VarSources[k] = n.Parent.Calculated.VarSources[k]
		default:
			c.VarSources[k] = VarSource{Node: n, Prop: "def_vars"}
		}
	}

	// when the node is excluded, only these are reported
	refs := len(n.undefinedRefs)
	c.IfEq, err = calcMapValues(n.Parsed.IfEq, "ifeq", c.Vars, undefined("ifeq", "", -1))
	if err != nil {
		return false, err
	}
	c.IfNEq, err = calcMapValues(n.Parsed.IfNEq, "ifneq", c.Vars, undefined("ifneq", "", -1))
	if err != nil {
		return false, err
	}
	excluded := func() (bool, error) {
		n.undefinedRefs = n.undefinedRefs[refs:]
		return false, nil
	}

	for k, v1 := range c.IfEq {
		v2, ok := c.Vars[k]
		if !ok {
			v2 = ""
		}
		if v1 != v2 {
			return excluded()
		}
	}
	for k, v1 := range c.IfNEq {
		v2, ok := c.Vars[k]
		if !ok {
			v2 = ""
		}
		if v1 == v2 {
			return excluded()
		}
	}

	// we will keep this node, register new index
	*idx += 1

	c.Title, err = eval("title", -1, n.Parsed.Title, c.Vars)
	if err != nil {
		return false, err
	}
	c.Description, err = eval("description", -1, n.Parsed.Description, c.Vars)
	if err != nil {
		return false, err
	}

	// Inherit from this
	ie := make(map[string]string)
	if n.Parent != nil && n.InheritEnvs {
		for k, v := range n.Parent.Calculated.Envs {
			ie[k] = v
		}
	}
	c.Envs, err = calcEnvs(n.Parsed.Envs, c.Vars, ie, undefined("envs", "", -1))
	if err != nil {
		return false, err
	}

	// args_prefix can be inherited from parent
	if n.Parsed.ArgsPrefix != nil {
		arr, err := evalArray("args_prefix", n.Parsed.ArgsPrefix)
		if err != nil {
			return false, err
		}
		c.ArgsPrefix = arr
	} else if n.Parent != nil && n.Parent.Calculated.ArgsPrefix != nil {
		c.ArgsPrefix = n.Parent.Calculated.ArgsPrefix
	} else {
		c.ArgsPrefix = make([]string, 0)
	}
	// args cannot be inherited, and it is prefixed
	if n.Parsed.Args != nil {
		args := make([]string, 0)
		if c.ArgsPrefix != nil {
			args = append(args, c.ArgsPrefix...)
		}
		arr, err := evalArray("args", n.Parsed.Args)
		if err != nil {
			return false, err
		}
		c.Args = append(args, arr...)
	}

	if n.Parsed.CWD == "" && n.Parent != nil {
		// like args_prefix: the calculated value of the parent (with its variables), through any number of levels
		c.CWD = n.Parent.Calculated.CWD
	} else {
		c.CWD, err = eval("cwd", -1, n.Parsed.CWD, c.Vars)
		if err != nil {
			return false, err
		}
	}
	if n.Parsed.Runner == "" && n.Parent != nil {
		// the parent is already calculated, so this is inherited through any number of levels
		c.Runner = n.Parent.Calculated.Runner
	} else {
		c.Runner, err = eval("runner", -1, n.Parsed.Runner, c.Vars)
		if err != nil {
			return false, err
		}
		if c.Runner == "" {
			c.Runner = n.Tree.defaultRunner
		}
		if _, err := runner.ParseAddress(c.Runner); err != nil {
			return false, propErrorf("runner", "%s", err)
		}
	}
	if n.Parsed.XLocks == nil && n.Parent != nil {
		// exclusive locks must not be inherited, they are exclusive!
		// c.XLocks = n.Parent.Calculated.XLocks
		c.XLocks = set.NewSet[string]()
	} else {
		c.XLocks, err = evalSet("xlocks", n.Parsed.XLocks)
		if err != nil {
			return false, err
		}
	}
	if n.Parsed.RLocks == nil && n.Parent != nil {
		// TODO: is it a problem that it references to the same object?
		c.RLocks = n.Parent.Calculated.RLocks
	} else {
		c.RLocks, err = evalSet("rlocks", n.Parsed.RLocks)
		if err != nil {
			return false, err
		}
	}
	// provides must not be inherited: a node provides its resources when the node itself succeeds
	c.Provides, err = evalSet("provides", n.Parsed.Provides)
	if err != nil {
		return false, err
	}

	req := set.NewSet[string]()
	if n.InheritRequires && n.Parent != nil && n.Parent.Calculated.Requires != nil {
		req.UnionInPlace(n.Parent.Calculated.Requires)
	}
	if n.Parsed.Requires != nil {
		st, err := evalSet("requires", n.Parsed.Requires)
		if err != nil {
			return false, err
		}
		req.UnionInPlace(st)
	}
	c.Requires = req

	n.Calculated = *c

	return true, nil
}

func (n *Node) FillNodeList(list []*Node) {
	list[n.Calculated.Idx] = n
	for _, sn := range n.Nodes {
		sn.FillNodeList(list)
	}
}

// calcVars calculates the variables of a node from the inherited ones, its def_vars (the defaults for the variables
// that are not inherited) and its vars. The values of def_vars can use the inherited variables and the other
// defaults. The values of vars can use all of these, and the other variables of the same block. A variable means its
// inherited value in its own value (so that it can be extended, e.g. path: "{path}:/opt/bin"). onVars and onDefVars
// are called for the references to undefined variables.
func calcVars(vars, defVars, inherited map[string]interface{}, onVars, onDefVars undefinedFunc) (
	map[string]interface{}, error) {
	res := make(map[string]interface{}, len(inherited)+len(vars)+len(defVars))
	for k, v := range inherited {
		res[k] = v
	}
	used := make(map[string]interface{}, len(defVars))
	for k, v := range defVars {
		_, isInherited := inherited[k]
		_, inVars := vars[k] // it is an error to have both, it is reported by the parser
		if !isInherited && !inVars {
			used[k] = v
		}
	}
	defaults, err := calcBlock(used, "def_vars", inherited, onDefVars)
	if err != nil {
		return nil, err
	}
	for k, v := range defaults {
		res[k] = v
	}
	own, err := calcBlock(vars, "vars", maps.Clone(res), onVars)
	if err != nil {
		return nil, err
	}
	for k, v := range own {
		res[k] = v
	}
	return res, nil
}

// calcBlock calculates the values of a block of variables (the value of prop). The values can use the other
// variables of the block in any order (with their calculated values), and the variables of base (a variable in its
// own value too). The strings and the strings in lists are calculated, other values are kept as they are.
func calcBlock(block map[string]interface{}, prop string, base map[string]interface{}, on undefinedFunc) (
	map[string]interface{}, error) {
	values := make(map[string]interface{}, len(block))
	var resolving []string // the variables that are being calculated, to find the cycles
	var resolve func(k string) (interface{}, error)
	resolve = func(k string) (interface{}, error) {
		if v, ok := values[k]; ok {
			return v, nil
		}
		for i, r := range resolving {
			if r == k {
				return nil, &propertyError{name: prop, key: k, item: -1,
					msg: fmt.Sprintf("the variables refer to each other: %s", strings.Join(
						append(append([]string(nil), resolving[i:]...), k), " → "))}
			}
		}
		resolving = append(resolving, k)
		defer func() { resolving = resolving[:len(resolving)-1] }()
		lookup := func(name string) (interface{}, bool, error) {
			if _, local := block[name]; local && name != k {
				v, err := resolve(name)
				return v, true, err
			}
			v, ok := base[name]
			return v, ok, nil
		}
		eval := func(s string, item int) (string, error) {
			res, err := varEvalLookup(s, lookup, k, item, on)
			if err != nil {
				var prop2 *propertyError
				if errors.As(err, &prop2) {
					return "", err // from the calculation of another variable
				}
				return "", &propertyError{name: prop, key: k, item: item, msg: err.Error()}
			}
			return res, nil
		}
		v := block[k]
		switch tv := v.(type) {
		case string:
			sv, err := eval(tv, -1)
			if err != nil {
				return nil, err
			}
			v = sv
		case []interface{}:
			l := make([]interface{}, len(tv))
			for i, item := range tv {
				l[i] = item
				if s, ok := item.(string); ok {
					sv, err := eval(s, i)
					if err != nil {
						return nil, err
					}
					l[i] = sv
				}
			}
			v = l
		}
		values[k] = v
		return v, nil
	}
	for _, k := range slices.Sorted(maps.Keys(block)) {
		if _, err := resolve(k); err != nil {
			return nil, err
		}
	}
	return values, nil
}

// calcMapValues calculates the string values of an object property (raw, the value of rawProp) with the variables.
// onRaw is called for the references to undefined variables.
func calcMapValues(raw map[string]interface{}, rawProp string, vars map[string]interface{}, onRaw undefinedFunc) (
	map[string]interface{}, error) {
	res := make(map[string]interface{}, len(raw))
	for k, v := range raw {
		if sv, ok := v.(string); ok {
			var err error
			v, err = varEvalUndefined(sv, vars, k, -1, onRaw)
			if err != nil {
				return nil, &propertyError{name: rawProp, key: k, item: -1, msg: err.Error()}
			}
		}
		res[k] = v
	}
	return res, nil
}

// calcEnvs calculates the envs of a node: the inherited ones, updated with its own envs. Their values can use the
// inherited envs and the variables (that override the envs with the same name). on is called for the references to
// undefined variables.
func calcEnvs(envs map[string]string, vars map[string]interface{}, inherited map[string]string, on undefinedFunc) (
	map[string]string, error) {
	values := make(map[string]interface{}, len(inherited)+len(vars))
	for k, v := range inherited {
		values[k] = v
	}
	for k, v := range vars {
		values[k] = v
	}
	res := make(map[string]string, len(inherited)+len(envs))
	for k, v := range inherited {
		res[k] = v
	}
	for k, v := range envs {
		sv, err := varEvalUndefined(v, values, k, -1, on)
		if err != nil {
			return nil, &propertyError{name: "envs", key: k, item: -1, msg: err.Error()}
		}
		res[k] = sv
	}
	return res, nil
}

// undefinedFunc is called for a reference to an undefined variable (name), in the value of key in an object
// property (or ""), in its item (or -1)
type undefinedFunc func(key string, item int, name string)

// rawStartTag replaces the start tags in the strings that are tagged !raw in the source, so that they are not
// substituted. The calculated values have the start tags again (see unraw).
const rawStartTag = "\uE000"

// Unraw returns a string of the source (e.g. a title) as it is written: without the replaced start tags of !raw
func Unraw(s string) string {
	return unraw(s)
}

// unraw replaces the raw start tags back
func unraw(s string) string {
	if !strings.Contains(s, rawStartTag) {
		return s
	}
	return strings.ReplaceAll(s, rawStartTag, StartTag)
}

// varNameRe matches the variable names that are reported when they are not defined. Other text in curly braces is
// common in commands (e.g. "{}" in find, "{a,b}" in bash, "{{.Name}}" in docker), and they are left as they are.
var varNameRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_-]*$`)

// IsVarName tells if a {name} reference is reported when the variable is not defined (see varNameRe)
func IsVarName(name string) bool {
	return varNameRe.MatchString(name)
}

// varEval substitutes variables ({name}) into a string. The substitution is done in a single pass: the substituted
// values are not evaluated again. References to undefined variables are left as they are.
func varEval(value string, values map[string]interface{}) (string, error) {
	return varEvalUndefined(value, values, "", -1, nil)
}

// varEvalUndefined is varEval, and it calls undefined (if not nil) with key and item for the references to undefined
// variables. ${name} is not reported, because it is a shell variable.
func varEvalUndefined(value string, values map[string]interface{}, key string, item int, undefined undefinedFunc) (
	string, error) {
	return varEvalLookup(value, func(name string) (interface{}, bool, error) {
		v, ok := values[name]
		return v, ok, nil
	}, key, item, undefined)
}

// varEvalLookup is varEvalUndefined with a function that returns the variable values
func varEvalLookup(value string, lookup func(name string) (interface{}, bool, error), key string, item int,
	undefined undefinedFunc) (string, error) {
	if !strings.Contains(value, StartTag) {
		return unraw(value), nil
	}
	var b strings.Builder
	pos := 0
	for {
		i := strings.Index(value[pos:], StartTag)
		if i < 0 {
			break
		}
		start := pos + i
		b.WriteString(value[pos:start])
		j := strings.Index(value[start+len(StartTag):], EndTag)
		if j < 0 {
			pos = start
			break
		}
		name := value[start+len(StartTag) : start+len(StartTag)+j]
		v, ok, err := lookup(name)
		if err != nil {
			return "", err
		}
		if !ok {
			if undefined != nil && varNameRe.MatchString(name) && (start == 0 || value[start-1] != '$') {
				undefined(key, item, name)
			}
			// it is not a variable, continue after the start tag (there can be a variable in it, e.g. "{{name}}")
			b.WriteString(StartTag)
			pos = start + len(StartTag)
			continue
		}
		sv, ok := v.(string)
		if !ok {
			return "", fmt.Errorf("cannot substitute {%s}: it is %s, not a string", name, describeValue(v))
		}
		b.WriteString(sv)
		pos = start + len(StartTag) + j + len(EndTag)
	}
	b.WriteString(value[pos:])
	return unraw(b.String()), nil
}

/*
func varEvalToStr(v interface{}, inheritFrom map[string]string) string {
	v2 := varEval(v, inheritFrom)
	v3, ok := v2.(string)
	if ok {
		return v3
	}
	return fmt.Sprintf("%v", v2)
}

// varEvalStringToString substitutes string variable values into a structure/value
func varEval(v interface{}, inheritFrom map[string]string) interface{} {
	switch sub := v.(type) {
	case string:
		return varEvalStringToString(sub, inheritFrom)
	case []interface{}:
		items := make([]interface{}, len(sub))
		for i, v := range sub {
			items[i] = varEval(v, inheritFrom)
		}
		return items
	case map[interface{}]interface{}:
		items := make(map[interface{}]interface{})
		for k, v := range sub {
			items[k.(string)] = varEval(v, inheritFrom)
		}
		return items
	default:
		return fmt.Errorf("invalid node type: %T", sub)
	}

}

// varEvalStringToString substitutes string variable values into a string
func varEvalStringToString(v string, values map[string]string) string {
	if !strings.Contains(v, StartTag) {
		return v
	}
	for k, v := range values {
		v = strings.Replace(v, StartTag+k+EndTag, v, -1)
	}
	return v
}
*/
