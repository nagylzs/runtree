package rt

import (
	"errors"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strings"

	"github.com/nagylzs/runtree/internal/config"
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
		return func(mapKey, name string) {
			k := key
			if k == "" {
				k = mapKey
			}
			n.undefinedRefs = append(n.undefinedRefs, undefinedRef{prop: prop, key: k, item: item, name: name})
		}
	}
	eval := func(prop string, item int, value string, values map[string]interface{}) (string, error) {
		res, err := varEvalUndefined(value, values, "", undefined(prop, "", item))
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

	// if c.Vars["host"] == "backup.hontalan.mess.hu" && n.Parsed.DefVars != nil {
	//if n.Id == "n69" {
	//	println(n.Id)
	//}

	var err error
	c.Vars, err = calcVars(n.Parsed.Vars, n.Parsed.DefVars, iv, undefined("vars", "", -1),
		undefined("def_vars", "", -1))
	if err != nil {
		return false, err
	}
	// below this line, variable evaluation should use c.Vars and **not** n.Parsed.Vars!

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
	c.Envs, err = calcMapStringString(n.Parsed.Envs, "envs", c.Vars, nil, ie, false, undefined("envs", "", -1))
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
			c.Runner = config.DefaultRunnerAddress
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

// calcVars calculates the variables of a node from the inherited ones, its vars and its def_vars (the defaults for
// the variables that are not inherited). The values of vars can use the other variables of the same block, in any
// order (with their calculated values), and in its own value a variable means its inherited value (so that it can be
// extended, e.g. path: "{path}:/opt/bin"). The values of def_vars can use the inherited variables. onVars and
// onDefVars are called for the references to undefined variables.
func calcVars(vars, defVars, inherited map[string]interface{}, onVars, onDefVars undefinedFunc) (
	map[string]interface{}, error) {
	res := make(map[string]interface{}, len(inherited)+len(vars)+len(defVars))
	for k, v := range inherited {
		res[k] = v
	}
	for k, v := range defVars {
		if _, ok := res[k]; ok {
			continue // inherited
		}
		if _, ok := vars[k]; ok {
			continue // it is an error to have both, it is reported by the parser
		}
		if sv, ok := v.(string); ok {
			var err error
			v, err = varEvalUndefined(sv, inherited, k, onDefVars)
			if err != nil {
				return nil, &propertyError{name: "def_vars", key: k, item: -1, msg: err.Error()}
			}
		}
		res[k] = v
	}

	// the vars are calculated in the order of their references
	values := make(map[string]interface{}, len(vars))
	var resolving []string // the variables that are being calculated, to find the cycles
	var resolve func(k string) (interface{}, error)
	resolve = func(k string) (interface{}, error) {
		if v, ok := values[k]; ok {
			return v, nil
		}
		for i, r := range resolving {
			if r == k {
				return nil, &propertyError{name: "vars", key: k, item: -1,
					msg: fmt.Sprintf("the variables refer to each other: %s", strings.Join(
						append(append([]string(nil), resolving[i:]...), k), " → "))}
			}
		}
		resolving = append(resolving, k)
		defer func() { resolving = resolving[:len(resolving)-1] }()
		v := vars[k]
		if sv, ok := v.(string); ok {
			var err error
			v, err = varEvalLookup(sv, func(name string) (interface{}, bool, error) {
				if _, own := vars[name]; own && name != k {
					v, err := resolve(name)
					return v, true, err
				}
				// inherited (its own name too), or a default
				v, ok := res[name]
				return v, ok, nil
			}, k, onVars)
			if err != nil {
				var prop *propertyError
				if errors.As(err, &prop) {
					return nil, err // from the calculation of another variable
				}
				return nil, &propertyError{name: "vars", key: k, item: -1, msg: err.Error()}
			}
		}
		values[k] = v
		return v, nil
	}
	for _, k := range slices.Sorted(maps.Keys(vars)) {
		if _, err := resolve(k); err != nil {
			return nil, err
		}
	}
	for k, v := range values {
		res[k] = v
	}
	return res, nil
}

// calcMapValues calculates the string values of an object property (raw, the value of rawProp) with the variables.
// onRaw is called for the references to undefined variables.
func calcMapValues(raw map[string]interface{}, rawProp string, vars map[string]interface{}, onRaw undefinedFunc) (
	map[string]interface{}, error) {
	res := make(map[string]interface{}, len(raw))
	for k, v := range raw {
		if sv, ok := v.(string); ok {
			var err error
			v, err = varEvalUndefined(sv, vars, k, onRaw)
			if err != nil {
				return nil, &propertyError{name: rawProp, key: k, item: -1, msg: err.Error()}
			}
		}
		res[k] = v
	}
	return res, nil
}

func calcMapStringString(raw map[string]string, rawProp string, vars map[string]interface{}, defVars map[string]string,
	inheritFrom map[string]string, includeSources bool, onRaw undefinedFunc) (map[string]string, error) {
	s := make(map[string]interface{}) // this is the source
	// first put inherited values into source
	for k, v := range inheritFrom {
		s[k] = v
	}
	ihf2 := maps.Clone(s)
	vars2 := maps.Clone(vars)
	if vars2 == nil {
		vars2 = make(map[string]interface{})
	}
	var err error
	// then put var values, substitute inherited values
	for k, v := range vars2 {
		sv, ok := v.(string)
		if ok {
			v, err = varEval(sv, ihf2)
			if err != nil {
				return nil, err
			}
		}
		s[k] = v
	}
	for k, sv := range defVars {
		_, ok := s[k]
		if !ok {
			sv, err = varEval(sv, ihf2)
			if err != nil {
				return nil, err
			}
			s[k] = sv
		}
	}
	// this is the resulting map
	var r = make(map[string]string)
	if includeSources {
		// include all source values
		for k, v := range s {
			sv, ok := v.(string)
			if ok {
				r[k] = sv
			}
		}
	}
	for k, sv := range raw {
		sv, err = varEvalUndefined(sv, s, k, onRaw)
		if err != nil {
			return nil, &propertyError{name: rawProp, key: k, item: -1, msg: err.Error()}
		}
		r[k] = sv
	}
	return r, nil
}

// undefinedFunc is called for a reference to an undefined variable (name), in the value of key in an object
// property (or "")
type undefinedFunc func(key string, name string)

// varNameRe matches the variable names that are reported when they are not defined. Other text in curly braces is
// common in commands (e.g. "{}" in find, "{a,b}" in bash, "{{.Name}}" in docker), and they are left as they are.
var varNameRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_-]*$`)

// varEval substitutes variables ({name}) into a string. The substitution is done in a single pass: the substituted
// values are not evaluated again. References to undefined variables are left as they are.
func varEval(value string, values map[string]interface{}) (string, error) {
	return varEvalUndefined(value, values, "", nil)
}

// varEvalUndefined is varEval, and it calls undefined (if not nil) with key for the references to undefined
// variables. ${name} is not reported, because it is a shell variable.
func varEvalUndefined(value string, values map[string]interface{}, key string, undefined undefinedFunc) (string,
	error) {
	return varEvalLookup(value, func(name string) (interface{}, bool, error) {
		v, ok := values[name]
		return v, ok, nil
	}, key, undefined)
}

// varEvalLookup is varEvalUndefined with a function that returns the variable values
func varEvalLookup(value string, lookup func(name string) (interface{}, bool, error), key string,
	undefined undefinedFunc) (string, error) {
	if !strings.Contains(value, StartTag) {
		return value, nil
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
				undefined(key, name)
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
	return b.String(), nil
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
