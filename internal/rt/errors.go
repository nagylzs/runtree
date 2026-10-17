package rt

import (
	"errors"
	"fmt"
	"strings"
)

// ParseError is an error in a tree source file, with its position. Error() returns a "file:line:col: message"
// line, followed by the node path and the include chain (if known).
type ParseError struct {
	Pos      Pos
	Message  string
	Node     string       // path of the node, if known
	Included []IncludePos // where the object with the error was included from, the innermost first
	Count    int          // how many times it was found (e.g. in for_vars combinations, or included several times)
	Warning  bool         // it is only a warning, the tree can be used
	Code     string       // the kind of the warning (CodeUndefinedVariable, ...), or ""
}

// The codes of the warnings
const (
	CodeUndefinedVariable = "undefined-variable"
	CodeEmptyForVars      = "empty-for-vars"
)

// IncludePos is the position of an include property value
type IncludePos struct {
	Pos  Pos
	Name string // the include argument, e.g. "servers.yaml|upgrade"
}

func (e *ParseError) Error() string {
	var b strings.Builder
	b.WriteString(e.Pos.String())
	b.WriteString(": ")
	if e.Warning {
		b.WriteString("warning: ")
	}
	b.WriteString(e.Message)
	if e.Count > 1 {
		fmt.Fprintf(&b, " (found %d times)", e.Count)
	}
	if e.Node != "" {
		b.WriteString("\n  in node: ")
		b.WriteString(e.Node)
	}
	for _, inc := range e.Included {
		fmt.Fprintf(&b, "\n  included from %s (include %q)", inc.Pos, inc.Name)
	}
	return b.String()
}

// ParseErrors is the list of the errors in a tree, in the order they were found. The same error (the same position
// and message) is listed only once.
type ParseErrors []*ParseError

func (e ParseErrors) Error() string {
	msgs := make([]string, len(e))
	for i, pe := range e {
		msgs[i] = pe.Error()
	}
	return strings.Join(msgs, "\n")
}

func (e ParseErrors) Unwrap() []error {
	errs := make([]error, len(e))
	for i, pe := range e {
		errs[i] = pe
	}
	return errs
}

func newParseError(pos Pos, format string, args ...any) *ParseError {
	return &ParseError{Pos: pos, Message: fmt.Sprintf(format, args...)}
}

// propertyError is an error in the value of a node property. LoadNode replaces it with a ParseError at the
// position of the value.
type propertyError struct {
	name string
	key  string // the key of the wrong value in an object value, or ""
	item int    // the index of the wrong item in a list value (of the key), or -1
	msg  string
}

func (e *propertyError) Error() string {
	return e.msg
}

func propErrorf(name string, format string, args ...any) error {
	return &propertyError{name: name, item: -1, msg: fmt.Sprintf(format, args...)}
}

// itemErrorf returns an error about an item of a list property value, it is positioned at the item
func itemErrorf(name string, item int, format string, args ...any) error {
	return &propertyError{name: name, item: item, msg: fmt.Sprintf(format, args...)}
}

// describeValue describes the type of a YAML value, for error messages
func describeValue(v interface{}) string {
	switch v.(type) {
	case nil:
		return "empty (null)"
	case bool:
		return "a boolean"
	case int, int64, uint64, float64:
		return "a number"
	case string:
		return "a string"
	case []interface{}:
		return "a list"
	case map[interface{}]interface{}:
		return "an object"
	}
	return fmt.Sprintf("%T", v)
}

// keyError is an error about a property name (e.g. an unknown property). LoadNode replaces it with a ParseError at
// the position of the key.
type keyError struct {
	name string
	msg  string
}

func (e *keyError) Error() string {
	return e.msg
}

// locateError converts an error that happened while loading the node from raw to a ParseError. Errors from
// sub-nodes and included files are already ParseErrors, they are returned as they are.
func locateError(err error, sources *Sources, raw map[interface{}]interface{}, n *Node, parent *Node) error {
	var pe *ParseError
	if errors.As(err, &pe) {
		return pe
	}
	pos := sources.MapPos(raw)
	var prop *propertyError
	var key *keyError
	if errors.As(err, &prop) {
		pos = sources.refPos(raw, undefinedRef{prop: prop.name, key: prop.key, item: prop.item})
	} else if errors.As(err, &key) {
		pos = sources.KeyPos(raw, key.name)
	}
	path := nodePath(n)
	if n == nil || nodeLabel(n) == "" {
		// the error is found before the node got its title or id, use the ones in the source
		path = joinPath(nodePath(parent), rawLabel(raw))
	}
	return &ParseError{Pos: pos, Message: err.Error(), Node: path}
}

func joinPath(parts ...string) string {
	res := ""
	for _, p := range parts {
		if p == "" {
			continue
		}
		if res != "" {
			res += " › "
		}
		res += p
	}
	return res
}

// rawLabel returns the title or the id of a node in the source
func rawLabel(raw map[interface{}]interface{}) string {
	for _, key := range []string{"title", "id"} {
		if s, ok := raw[key].(string); ok && s != "" {
			return s
		}
	}
	return ""
}

func nodeLabel(n *Node) string {
	if n.Calculated.Title != "" {
		return n.Calculated.Title
	}
	if n.Parsed.Title != "" {
		return n.Parsed.Title
	}
	return n.Id
}

// nodePath returns the titles (or ids) of the node and its parents, e.g. "upgrade all › upgrade servers › deploy"
func nodePath(n *Node) string {
	if n == nil {
		return ""
	}
	if n.Parent == nil {
		return nodeLabel(n)
	}
	return joinPath(nodePath(n.Parent), nodeLabel(n))
}

// unknownPropertyError returns the error for an unknown node property, with a suggestion if there is a similar
// valid property name.
func unknownPropertyError(name string) error {
	msg := fmt.Sprintf("unknown property %q", name)
	if s := suggestProperty(name); s != "" {
		msg += fmt.Sprintf(" (did you mean %q?)", s)
	}
	return &keyError{name: name, msg: msg}
}

func suggestProperty(name string) string {
	best, bestDist := "", -1
	for _, valid := range ValidNodeConfigs.List() {
		d := editDistance(name, valid)
		if bestDist < 0 || d < bestDist || d == bestDist && valid < best {
			best, bestDist = valid, d
		}
	}
	if bestDist >= 1 && bestDist <= 2 && bestDist <= len(name)/3+1 {
		return best
	}
	return ""
}

// editDistance is the Levenshtein distance of two strings
func editDistance(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	prev := make([]int, len(rb)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(ra); i++ {
		cur := make([]int, len(rb)+1)
		cur[0] = i
		for j := 1; j <= len(rb); j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev = cur
	}
	return prev[len(rb)]
}
