package rt

import (
	"maps"
	"strings"

	"github.com/nagylzs/runtree/internal/config"
	"github.com/nagylzs/set"
)

const StartTag = "{"
const EndTag = "}"

func (n *Node) calculate(idx *uint, level uint) bool {
	c := &Calculated{Idx: *idx, Level: level}

	// Inherit from this
	iv := make(map[string]string)
	if n.Parent != nil && n.InheritVars {
		for k, v := range n.Parent.Calculated.Vars {
			iv[k] = v
		}
	}

	// if c.Vars["host"] == "backup.hontalan.mess.hu" && n.Parsed.DefVars != nil {
	//if n.Id == "n69" {
	//	println(n.Id)
	//}

	c.Vars = calcMap(n.Parsed.Vars, n.Parsed.Vars, n.Parsed.DefVars, iv, true)
	// below this line, variable evaluation should use c.Vars and **not** n.Parsed.Vars!

	c.IfEq = calcMap(n.Parsed.IfEq, n.Parsed.Vars, n.Parsed.DefVars, iv, false)
	c.IfNEq = calcMap(n.Parsed.IfNEq, n.Parsed.Vars, n.Parsed.DefVars, iv, false)

	for k, v1 := range c.IfEq {
		v2, ok := c.Vars[k]
		if !ok {
			v2 = ""
		}
		if v1 != v2 {
			return false
		}
	}
	for k, v1 := range c.IfNEq {
		v2, ok := c.Vars[k]
		if !ok {
			v2 = ""
		}
		if v1 == v2 {
			return false
		}
	}

	// we will keep this node, register new index
	*idx += 1

	c.Title = varEval(n.Parsed.Title, c.Vars)
	c.Description = varEval(n.Parsed.Description, c.Vars)

	// Inherit from this
	ie := make(map[string]string)
	if n.Parent != nil && n.InheritEnvs {
		for k, v := range n.Parent.Calculated.Envs {
			ie[k] = v
		}
	}
	c.Envs = calcMap(n.Parsed.Envs, c.Vars, nil, ie, false)

	// argsprefix can be inherited from parent
	if n.Parsed.ArgsPrefix != nil {
		c.ArgsPrefix = varEvalArray(n.Parsed.ArgsPrefix, c.Vars)
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
		c.Args = append(args, varEvalArray(n.Parsed.Args, c.Vars)...)
	}

	if n.Parsed.CWD == "" && n.Parent != nil {
		c.CWD = n.Parent.Parsed.CWD
	} else {
		c.CWD = varEval(n.Parsed.CWD, c.Vars)
	}
	cRunner := ""
	if n.Parsed.Runner == "" && n.Parent != nil {
		cRunner = n.Parent.Parsed.Runner
	}
	if cRunner == "" {
		cRunner = varEval(n.Parsed.Runner, c.Vars)
	}
	if cRunner == "" {
		cRunner = config.DefaultRunnerAddress
	}
	c.Runner = cRunner
	if n.Parsed.XLocks == nil && n.Parent != nil {
		// exclusive locks must not be inherited, they are exclusive!
		// c.XLocks = n.Parent.Calculated.XLocks
		c.XLocks = set.NewSet[string]()
	} else {
		c.XLocks = varEvalSet(n.Parsed.XLocks, c.Vars)
	}
	if n.Parsed.RLocks == nil && n.Parent != nil {
		// TODO: is it a problem that it references to the same object?
		c.RLocks = n.Parent.Calculated.RLocks
	} else {
		c.RLocks = varEvalSet(n.Parsed.RLocks, c.Vars)
	}
	if n.Parsed.Provides == nil && n.Parent != nil {
		// TODO: is it a problem that it references to the same object?
		c.Provides = n.Parent.Calculated.Provides
	} else {
		c.Provides = varEvalSet(n.Parsed.Provides, c.Vars)
	}

	req := set.NewSet[string]()
	if n.InheritRequires && n.Parent != nil && n.Parent.Calculated.Requires != nil {
		req.UnionInPlace(n.Parent.Calculated.Requires)
	}
	if n.Parsed.Requires != nil {
		req.UnionInPlace(varEvalSet(n.Parsed.Requires, c.Vars))
	}
	c.Requires = req

	n.Calculated = *c

	return true
}

func (n *Node) FillNodeList(list []*Node) {
	list[n.Calculated.Idx] = n
	for _, sn := range n.Nodes {
		sn.FillNodeList(list)
	}
}

func calcMap(raw map[string]string, vars map[string]string, defVars map[string]string, inheritFrom map[string]string, includeSources bool) map[string]string {
	s := make(map[string]string) // this is the source
	// first put inherited values into source
	for k, v := range inheritFrom {
		s[k] = v
	}
	vars2 := maps.Clone(vars)
	if vars2 == nil {
		vars2 = make(map[string]string)
	}
	// then put var values, substitute inherited values
	for k, v := range vars2 {
		s[k] = varEval(v, inheritFrom)
	}
	for k, v := range defVars {
		_, ok := s[k]
		if !ok {
			s[k] = varEval(v, inheritFrom)
		}
	}
	// this is the resulting map
	var r map[string]string
	if includeSources {
		// include all source values
		r = maps.Clone(s)
	} else {
		// only include the values that have keys in raw
		r = make(map[string]string)
	}
	for k, v := range raw {
		r[k] = varEval(v, s)
	}
	return r
}

// varEval substitutes variables into a string
func varEval(value string, values map[string]string) string {
	if !strings.Contains(value, StartTag) {
		return value
	}
	// TODO: handle escape sequences, prevent multi-replacement
	// Create a state machine (splitting by StartTag, EndTag) ?
	for k, v := range values {
		value = strings.Replace(value, StartTag+k+EndTag, v, -1)
	}
	return value
}

// varEvalArray substitutes variables into items of a string array
func varEvalArray(items []string, values map[string]string) []string {
	res := make([]string, len(items))
	for i, v := range items {
		res[i] = varEval(v, values)
	}
	return res
}

// varEvalSet substitutes variables into items of a string set
func varEvalSet(items []string, values map[string]string) *set.Set[string] {
	return set.FromArray(varEvalArray(items, values))
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
