package rt

import (
	"fmt"
	"maps"
	"strings"

	"github.com/nagylzs/runtree/internal/config"
	"github.com/nagylzs/set"
)

const StartTag = "{"
const EndTag = "}"

func (n *Node) calculate(idx *uint, level uint) (bool, error) {
	c := &Calculated{Idx: *idx, Level: level}

	// Inherit from this
	iv := make(map[string]interface{})
	if n.Parent != nil && n.InheritVars {
		for k, v := range n.Parent.Calculated.Vars {
			iv[k] = v
		}
	}

	// if c.Vars["host"] == "backup.hontalan.mess.hu" && n.Parsed.DefVars != nil {
	//if n.Id == "n69" {
	//	println(n.Id)
	//}

	var err error
	c.Vars, err = calcMap(n.Parsed.Vars, n.Parsed.Vars, n.Parsed.DefVars, iv, true)
	if err != nil {
		return false, err
	}
	// below this line, variable evaluation should use c.Vars and **not** n.Parsed.Vars!

	c.IfEq, err = calcMap(n.Parsed.IfEq, n.Parsed.Vars, n.Parsed.DefVars, iv, false)
	if err != nil {
		return false, err
	}
	c.IfNEq, err = calcMap(n.Parsed.IfNEq, n.Parsed.Vars, n.Parsed.DefVars, iv, false)
	if err != nil {
		return false, err
	}

	for k, v1 := range c.IfEq {
		v2, ok := c.Vars[k]
		if !ok {
			v2 = ""
		}
		if v1 != v2 {
			return false, nil
		}
	}
	for k, v1 := range c.IfNEq {
		v2, ok := c.Vars[k]
		if !ok {
			v2 = ""
		}
		if v1 == v2 {
			return false, nil
		}
	}

	// we will keep this node, register new index
	*idx += 1

	c.Title, err = varEval(n.Parsed.Title, c.Vars)
	if err != nil {
		return false, err
	}
	c.Description, err = varEval(n.Parsed.Description, c.Vars)
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
	c.Envs, err = calcMapStringString(n.Parsed.Envs, c.Vars, nil, ie, false)
	if err != nil {
		return false, err
	}

	// argsprefix can be inherited from parent
	if n.Parsed.ArgsPrefix != nil {
		arr, err := varEvalArray(n.Parsed.ArgsPrefix, c.Vars)
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
		arr, err := varEvalArray(n.Parsed.Args, c.Vars)
		if err != nil {
			return false, err
		}
		c.Args = append(args, arr...)
	}

	if n.Parsed.CWD == "" && n.Parent != nil {
		c.CWD = n.Parent.Parsed.CWD
	} else {
		c.CWD, err = varEval(n.Parsed.CWD, c.Vars)
		if err != nil {
			return false, err
		}
	}
	cRunner := ""
	if n.Parsed.Runner == "" && n.Parent != nil {
		cRunner = n.Parent.Parsed.Runner
	}
	if cRunner == "" {
		cRunner, err = varEval(n.Parsed.Runner, c.Vars)
		if err != nil {
			return false, err
		}
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
		c.XLocks, err = varEvalSet(n.Parsed.XLocks, c.Vars)
		if err != nil {
			return false, err
		}
	}
	if n.Parsed.RLocks == nil && n.Parent != nil {
		// TODO: is it a problem that it references to the same object?
		c.RLocks = n.Parent.Calculated.RLocks
	} else {
		c.RLocks, err = varEvalSet(n.Parsed.RLocks, c.Vars)
		if err != nil {
			return false, err
		}
	}
	if n.Parsed.Provides == nil && n.Parent != nil {
		// TODO: is it a problem that it references to the same object?
		c.Provides = n.Parent.Calculated.Provides
	} else {
		c.Provides, err = varEvalSet(n.Parsed.Provides, c.Vars)
		if err != nil {
			return false, err
		}
	}

	req := set.NewSet[string]()
	if n.InheritRequires && n.Parent != nil && n.Parent.Calculated.Requires != nil {
		req.UnionInPlace(n.Parent.Calculated.Requires)
	}
	if n.Parsed.Requires != nil {
		st, err := varEvalSet(n.Parsed.Requires, c.Vars)
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

func calcMap(raw map[string]interface{}, vars map[string]interface{}, defVars map[string]interface{},
	inheritFrom map[string]interface{}, includeSources bool) (map[string]interface{}, error) {
	s := make(map[string]interface{}) // this is the source
	// first put inherited values into source
	for k, v := range inheritFrom {
		s[k] = v
	}
	vars2 := maps.Clone(vars)
	if vars2 == nil {
		vars2 = make(map[string]interface{})
	}
	var err error
	// then put var values, substitute inherited values
	for k, v := range vars2 {
		sv, ok := v.(string)
		if ok {
			v, err = varEval(sv, inheritFrom)
			if err != nil {
				return nil, err
			}
		}
		s[k] = v
	}
	for k, v := range defVars {
		_, ok := s[k]
		if !ok {
			sv, ok := v.(string)
			if ok {
				v, err = varEval(sv, inheritFrom)
				if err != nil {
					return nil, err
				}
			}
			s[k] = v
		}
	}
	// this is the resulting map
	var r map[string]interface{}
	if includeSources {
		// include all source values
		r = maps.Clone(s)
	} else {
		// only include the values that have keys in raw
		r = make(map[string]interface{})
	}
	for k, v := range raw {
		sv, ok := v.(string)
		if ok {
			v, err = varEval(sv, s)
			if err != nil {
				return nil, err
			}
		}
		r[k] = v
	}
	return r, nil
}

func calcMapStringString(raw map[string]string, vars map[string]interface{}, defVars map[string]string,
	inheritFrom map[string]string, includeSources bool) (map[string]string, error) {
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
		sv, err = varEval(sv, s)
		if err != nil {
			return nil, err
		}
		r[k] = sv
	}
	return r, nil
}

// varEval substitutes variables into a string
func varEval(value string, values map[string]interface{}) (string, error) {
	if !strings.Contains(value, StartTag) {
		return value, nil
	}
	// TODO: handle escape sequences, prevent multi-replacement
	// Create a state machine (splitting by StartTag, EndTag) ?
	for k, v := range values {
		pat := StartTag + k + EndTag
		hasVar := strings.Contains(value, pat)
		if hasVar {
			sv, ok := v.(string)
			if !ok {
				return "", fmt.Errorf("cannot substitute {%s}: value is not a string", k)
			}
			value = strings.Replace(value, pat, sv, -1)
		}
	}
	return value, nil
}

// varEvalString substitutes string variables into a string
func varEvalString(value string, values map[string]string) (string, error) {
	if !strings.Contains(value, StartTag) {
		return value, nil
	}
	// TODO: handle escape sequences, prevent multi-replacement
	// Create a state machine (splitting by StartTag, EndTag) ?
	for k, v := range values {
		value = strings.Replace(value, StartTag+k+EndTag, v, -1)
	}
	return value, nil
}

// varEvalArray substitutes variables into items of a string array
func varEvalArray(items []string, values map[string]interface{}) ([]string, error) {
	res := make([]string, len(items))
	for i, v := range items {
		sv, err := varEval(v, values)
		if err != nil {
			return nil, err
		}
		res[i] = sv
	}
	return res, nil
}

// varEvalSet substitutes variables into items of a string set
func varEvalSet(items []string, values map[string]interface{}) (*set.Set[string], error) {
	arr, err := varEvalArray(items, values)
	if err != nil {
		return nil, err
	}
	return set.FromArray(arr), nil
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
