package rt

import (
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// Pos is a position in a tree source file. Line and Column are 1-based, they are 0 when unknown.
type Pos struct {
	File   string // absolute path
	Line   int
	Column int
}

// String returns "file:line:col" (the parts that are known). The file is relative to the current directory when
// it is inside it.
func (p Pos) String() string {
	s := displayPath(p.File)
	if p.Line > 0 {
		s += ":" + strconv.Itoa(p.Line)
		if p.Column > 0 {
			s += ":" + strconv.Itoa(p.Column)
		}
	}
	return s
}

func displayPath(path string) string {
	if path == "" {
		return "?"
	}
	wd, err := os.Getwd()
	if err != nil {
		return path
	}
	rel, err := filepath.Rel(wd, path)
	if err != nil || len(rel) >= 2 && rel[:2] == ".." {
		return path
	}
	return rel
}

// Sources holds the parsed tree source files: the top level objects by absolute file path, and the positions of
// the values in them.
type Sources struct {
	Files map[string]map[string]interface{}
	// Overlay has the contents of the files that are used instead of the files on the disk (e.g. the unsaved
	// documents of an editor), by absolute path
	Overlay map[string][]byte
	// DefaultRunner is the runner of the nodes that do not specify one, not even their parents (runtree --runner).
	// Empty means the local runner. It must be a valid runner address (runner.ParseAddress).
	DefaultRunner string

	maps  map[uintptr]*mapPositions              // by map identity
	lists map[uintptr][]Pos                      // item positions by list identity
	top   map[string]map[interface{}]interface{} // the top level mapping of each file

	errs     ParseErrors            // the errors found while loading the tree
	warnings ParseErrors            // the warnings found while loading the tree
	errIdx   map[string]*ParseError // by position and message
	includes []IncludePos           // the includes that are being loaded, the outermost first
	reported int                    // the number of reported errors, including the repeated ones
	order    map[string]int         // the order of the files, by when they were first loaded
}

func (s *Sources) pushInclude(inc IncludePos) {
	s.includes = append(s.includes, inc)
}

func (s *Sources) popInclude() {
	s.includes = s.includes[:len(s.includes)-1]
}

// report records an error that was found while loading the tree (loading continues after it). The same error is
// recorded only once (e.g. when an object is included several times), with its first include chain.
func (s *Sources) report(err error) {
	var list ParseErrors
	if errors.As(err, &list) {
		for _, pe := range list {
			s.report(pe)
		}
		return
	}
	var pe *ParseError
	if !errors.As(err, &pe) {
		pe = &ParseError{Message: err.Error()}
	}
	s.reported++
	s.record(pe, &s.errs)
}

// warn records a warning, like report
func (s *Sources) warn(pe *ParseError) {
	pe.Warning = true
	s.record(pe, &s.warnings)
}

func (s *Sources) record(pe *ParseError, list *ParseErrors) {
	// the messages and the node titles may contain strings that are tagged !raw
	pe.Message, pe.Node = unraw(pe.Message), unraw(pe.Node)
	key := fmt.Sprintf("%v\x00%s\x00%s", pe.Warning, pe.Pos.String(), pe.Message)
	if prev, ok := s.errIdx[key]; ok {
		prev.Count++
		return
	}
	if pe.Count == 0 {
		pe.Count = 1
	}
	if len(pe.Included) == 0 {
		// where it was included from, the innermost first
		for i := len(s.includes) - 1; i >= 0; i-- {
			pe.Included = append(pe.Included, s.includes[i])
		}
	}
	s.errIdx[key] = pe
	*list = append(*list, pe)
}

// isIncluded tells if an included object is being loaded
func (s *Sources) isIncluded() bool {
	return len(s.includes) > 0
}

// Errors returns the errors found while loading the tree (ParseErrors), or nil. They are sorted by their position
// in each file, the files are in the order they were loaded.
func (s *Sources) Errors() error {
	if len(s.errs) == 0 {
		return nil
	}
	return s.sorted(s.errs)
}

// Warnings returns the warnings found while loading the tree, sorted like the errors
func (s *Sources) Warnings() ParseErrors {
	return s.sorted(s.warnings)
}

func (s *Sources) sorted(list ParseErrors) ParseErrors {
	fileOrder := maps.Clone(s.order)
	for _, pe := range list {
		if _, ok := fileOrder[pe.Pos.File]; !ok {
			fileOrder[pe.Pos.File] = len(fileOrder)
		}
	}
	res := append(ParseErrors(nil), list...)
	sort.SliceStable(res, func(i, j int) bool {
		a, b := res[i].Pos, res[j].Pos
		if a.File != b.File {
			return fileOrder[a.File] < fileOrder[b.File]
		}
		if a.Line != b.Line {
			return a.Line < b.Line
		}
		return a.Column < b.Column
	})
	return res
}

// refPos returns the position of an undefined variable reference in the node source raw
func (s *Sources) refPos(raw map[interface{}]interface{}, r undefinedRef) Pos {
	pos := s.ValuePos(raw, r.prop)
	v := raw[r.prop]
	if r.key != "" {
		m, ok := v.(map[interface{}]interface{})
		if !ok {
			return pos
		}
		pos = s.ValuePos(m, r.key)
		v = m[r.key]
	}
	if l, ok := v.([]interface{}); ok && r.item >= 0 && r.item < len(l) {
		pos = s.ItemPos(l, r.item)
	}
	return pos
}

type mapPositions struct {
	pos    Pos // where the mapping starts
	keys   map[string]Pos
	values map[string]Pos
	order  []string // keys in source order
}

func NewSources() *Sources {
	return &Sources{
		Files: make(map[string]map[string]interface{}),
		maps:  make(map[uintptr]*mapPositions),
		lists: make(map[uintptr][]Pos),
		top:   make(map[string]map[interface{}]interface{}),

		errIdx: make(map[string]*ParseError),
		order:  make(map[string]int),
	}
}

// The parsed maps and lists are kept alive by Sources.Files, so their identities are not reused while the
// Sources exists.
func mapID(m map[interface{}]interface{}) uintptr {
	return reflect.ValueOf(m).Pointer()
}

func listID(l []interface{}) uintptr {
	if len(l) == 0 {
		return 0
	}
	return reflect.ValueOf(l).Pointer()
}

// setMapPos records the position of a mapping that was created by the parser (e.g. a run node given as a list of
// arguments), and of the given keys in it.
func (s *Sources) setMapPos(m map[interface{}]interface{}, pos Pos, keys ...string) {
	mp := &mapPositions{pos: pos, keys: make(map[string]Pos), values: make(map[string]Pos), order: keys}
	for _, k := range keys {
		mp.keys[k], mp.values[k] = pos, pos
	}
	s.maps[mapID(m)] = mp
}

// TopPos returns the position of a top level object in a parsed file, or the position of the file.
func (s *Sources) TopPos(file string, name string) Pos {
	if top, ok := s.top[file]; ok {
		return s.ValuePos(top, name)
	}
	return Pos{File: file}
}

// MapPos returns the position of a parsed mapping (the zero Pos if it is not from a source file).
func (s *Sources) MapPos(m map[interface{}]interface{}) Pos {
	if mp := s.maps[mapID(m)]; mp != nil {
		return mp.pos
	}
	return Pos{}
}

// KeyPos returns the position of a key in a parsed mapping, or the position of the mapping.
func (s *Sources) KeyPos(m map[interface{}]interface{}, key string) Pos {
	if mp := s.maps[mapID(m)]; mp != nil {
		if p, ok := mp.keys[key]; ok {
			return p
		}
		return mp.pos
	}
	return Pos{}
}

// ValuePos returns the position of the value of a key in a parsed mapping, or the position of the mapping.
func (s *Sources) ValuePos(m map[interface{}]interface{}, key string) Pos {
	if mp := s.maps[mapID(m)]; mp != nil {
		if p, ok := mp.values[key]; ok {
			return p
		}
		return mp.pos
	}
	return Pos{}
}

// ItemPos returns the position of an item of a parsed list.
func (s *Sources) ItemPos(l []interface{}, i int) Pos {
	if ps := s.lists[listID(l)]; i < len(ps) {
		return ps[i]
	}
	return Pos{}
}

// Keys returns the keys of a parsed mapping in source order (in any order if it is not from a source file).
func (s *Sources) Keys(m map[interface{}]interface{}) []interface{} {
	if mp := s.maps[mapID(m)]; mp != nil && len(mp.order) == len(m) {
		keys := make([]interface{}, len(mp.order))
		for i, k := range mp.order {
			keys[i] = k
		}
		return keys
	}
	keys := make([]interface{}, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

var yamlLineRe = regexp.MustCompile(`^yaml: line (\d+): (.*)$`)

// yaml.v3 reports 0-based line numbers for parser errors (it only adds 1 for scanner errors). These are the
// problems of its parser errors (parserc.go of v3.0.1), TestYAMLSyntaxErrorLines checks the result.
var yamlParserProblems = map[string]bool{
	"did not find expected ',' or ']'":       true,
	"did not find expected ',' or '}'":       true,
	"did not find expected '-' indicator":    true,
	"did not find expected <document start>": true,
	"did not find expected <stream-start>":   true,
	"did not find expected key":              true,
	"did not find expected node content":     true,
	"found duplicate %TAG directive":         true,
	"found duplicate %YAML directive":        true,
	"found incompatible YAML document":       true,
	"found undefined tag handle":             true,
}

// AddFile reads and parses a tree source file (if it was not parsed yet), and returns its top level objects.
func (s *Sources) AddFile(absoluteFilePath string) (map[string]interface{}, error) {
	if rawTrees, ok := s.Files[absoluteFilePath]; ok {
		return rawTrees, nil
	}
	if _, ok := s.order[absoluteFilePath]; !ok {
		s.order[absoluteFilePath] = len(s.order)
	}
	filePos := Pos{File: absoluteFilePath}
	data, ok := s.Overlay[absoluteFilePath]
	if !ok {
		fi, err := os.Stat(absoluteFilePath)
		if os.IsNotExist(err) {
			return nil, newParseError(filePos, "file not found")
		}
		if err != nil {
			return nil, newParseError(filePos, "could not open: %v", err)
		}
		if fi.Size() > MaxYamlFileSize {
			return nil, newParseError(filePos, "file is bigger than %v bytes", MaxYamlFileSize)
		}
		data, err = os.ReadFile(absoluteFilePath)
		if err != nil {
			return nil, newParseError(filePos, "could not read: %v", err)
		}
	}
	var doc yaml.Node
	err := yaml.Unmarshal(data, &doc)
	if err != nil {
		if m := yamlLineRe.FindStringSubmatch(err.Error()); m != nil {
			line, _ := strconv.Atoi(m[1])
			if yamlParserProblems[m[2]] {
				line++
			}
			return nil, newParseError(Pos{File: absoluteFilePath, Line: line}, "%s", m[2])
		}
		return nil, newParseError(filePos, "%v", err)
	}
	rawTrees := make(map[string]interface{})
	if len(doc.Content) > 0 {
		top := doc.Content[0]
		if top.Kind != yaml.MappingNode {
			return nil, newParseError(s.pos(absoluteFilePath, top), "the file must contain an object (top level names)")
		}
		v, err := s.convert(absoluteFilePath, top)
		if err != nil {
			return nil, err
		}
		s.top[absoluteFilePath] = v.(map[interface{}]interface{})
		for k, val := range v.(map[interface{}]interface{}) {
			ks, ok := k.(string)
			if !ok {
				return nil, newParseError(s.pos(absoluteFilePath, top), "top level names must be strings, got %v", k)
			}
			rawTrees[ks] = val
		}
	}
	s.Files[absoluteFilePath] = rawTrees
	return rawTrees, nil
}

func (s *Sources) pos(file string, n *yaml.Node) Pos {
	return Pos{File: file, Line: n.Line, Column: n.Column}
}

// convert converts a yaml node to the values that the parser works with (map[interface{}]interface{},
// []interface{} and scalars), and it records their positions.
// RawTag marks a string in a tree file that is used as it is, without variable substitution
const RawTag = "!raw"

func (s *Sources) convert(file string, n *yaml.Node) (interface{}, error) {
	if n.Kind != yaml.AliasNode && n.Tag != "" && n.Tag != "!" && !strings.HasPrefix(n.Tag, "!!") {
		switch {
		case n.Tag == RawTag && n.Kind == yaml.ScalarNode:
			// the start tags are replaced, so they are not substituted
			return strings.ReplaceAll(n.Value, StartTag, rawStartTag), nil
		case n.Tag == RawTag:
			return nil, newParseError(s.pos(file, n), "%s can only be used for a string", RawTag)
		default:
			return nil, newParseError(s.pos(file, n), "unknown tag %s (only %s is supported)", n.Tag, RawTag)
		}
	}
	switch n.Kind {
	case yaml.AliasNode:
		return s.convert(file, n.Alias)
	case yaml.ScalarNode:
		var v interface{}
		if err := n.Decode(&v); err != nil {
			return nil, newParseError(s.pos(file, n), "%v", err)
		}
		return v, nil
	case yaml.SequenceNode:
		l := make([]interface{}, len(n.Content))
		ps := make([]Pos, len(n.Content))
		for i, item := range n.Content {
			v, err := s.convert(file, item)
			if err != nil {
				return nil, err
			}
			l[i], ps[i] = v, s.pos(file, item)
		}
		if len(l) > 0 {
			s.lists[listID(l)] = ps
		}
		return l, nil
	case yaml.MappingNode:
		m := make(map[interface{}]interface{})
		mp := &mapPositions{pos: s.pos(file, n), keys: make(map[string]Pos), values: make(map[string]Pos)}
		set := func(k interface{}, v interface{}, kp, vp Pos) {
			ks := fmt.Sprint(k)
			if _, exists := m[k]; !exists {
				mp.order = append(mp.order, ks)
			}
			m[k] = v
			mp.keys[ks], mp.values[ks] = kp, vp
		}
		// merge keys ("<<: *anchor" or a list of anchors) first, explicit keys override them
		for i := 0; i+1 < len(n.Content); i += 2 {
			key, value := n.Content[i], n.Content[i+1]
			if key.Tag != "!!merge" {
				continue
			}
			sources := []*yaml.Node{value}
			if value.Kind == yaml.SequenceNode {
				sources = value.Content
			}
			for _, src := range sources {
				merged, err := s.convert(file, src)
				if err != nil {
					return nil, err
				}
				mm, ok := merged.(map[interface{}]interface{})
				if !ok {
					return nil, newParseError(s.pos(file, src), "a merge key (<<) needs an object")
				}
				for _, k := range s.Keys(mm) {
					set(k, mm[k], s.KeyPos(mm, fmt.Sprint(k)), s.ValuePos(mm, fmt.Sprint(k)))
				}
			}
		}
		explicit := make(map[string]Pos)
		for i := 0; i+1 < len(n.Content); i += 2 {
			key, value := n.Content[i], n.Content[i+1]
			if key.Tag == "!!merge" {
				continue
			}
			if key.Tag == RawTag {
				return nil, newParseError(s.pos(file, key), "%s can only be used for values", RawTag)
			}
			k, err := s.convert(file, key)
			if err != nil {
				return nil, err
			}
			if _, ok := k.(map[interface{}]interface{}); ok {
				return nil, newParseError(s.pos(file, key), "object keys must be scalars")
			}
			if prev, dup := explicit[fmt.Sprint(k)]; dup {
				return nil, newParseError(s.pos(file, key), "duplicate key %q (already defined at line %d)", fmt.Sprint(k),
					prev.Line)
			}
			explicit[fmt.Sprint(k)] = s.pos(file, key)
			v, err := s.convert(file, value)
			if err != nil {
				return nil, err
			}
			set(k, v, s.pos(file, key), s.pos(file, value))
		}
		s.maps[mapID(m)] = mp
		return m, nil
	}
	return nil, newParseError(s.pos(file, n), "unsupported yaml node")
}
