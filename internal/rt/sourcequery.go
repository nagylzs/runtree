package rt

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"
)

// Queries on the parsed sources, for editors (the language server).

// IncludeTarget returns the file and the top level object name that an include argument (name) refers to, from the
// file filePath: "name" is in the same file, "file|name" is relative to the directory of filePath.
func IncludeTarget(filePath, name string) (file string, object string) {
	i := strings.Index(name, "|")
	if i < 0 {
		return filePath, name
	}
	return filepath.Join(filepath.Dir(filePath), name[:i]), name[i+1:]
}

// IncludeRef is an include argument in a source file
type IncludeRef struct {
	Pos  Pos    // the position of the argument
	Name string // the include argument, e.g. "servers.yaml|upgrade"
}

// IncludeRefs returns the include arguments in a file that was added, in source order
func (s *Sources) IncludeRefs(file string) []IncludeRef {
	var refs []IncludeRef
	var walk func(v interface{})
	walk = func(v interface{}) {
		switch tv := v.(type) {
		case map[interface{}]interface{}:
			for _, k := range s.Keys(tv) {
				ks, _ := k.(string)
				if ks == "include" {
					switch inc := tv[k].(type) {
					case string:
						refs = append(refs, IncludeRef{Pos: s.ValuePos(tv, ks), Name: inc})
					case []interface{}:
						for i, item := range inc {
							if name, ok := item.(string); ok {
								refs = append(refs, IncludeRef{Pos: s.ItemPos(inc, i), Name: name})
							}
						}
					}
					continue
				}
				walk(tv[k])
			}
		case []interface{}:
			for _, item := range tv {
				walk(item)
			}
		}
	}
	if top, ok := s.top[file]; ok {
		walk(top)
	}
	return refs
}

// TopNames returns the top level object names of a file that was added, in source order
func (s *Sources) TopNames(file string) []string {
	var names []string
	for _, k := range s.Keys(s.top[file]) {
		if ks, ok := k.(string); ok {
			names = append(names, ks)
		}
	}
	return names
}

// LoadedFiles returns the files that were loaded (or tried to be loaded), in the order they were loaded
func (s *Sources) LoadedFiles() []string {
	files := make([]string, 0, len(s.order))
	for f := range s.order {
		files = append(files, f)
	}
	slices.SortFunc(files, func(a, b string) int { return s.order[a] - s.order[b] })
	return files
}

// TopKeyPos returns the position of the name of a top level object in a file that was added
func (s *Sources) TopKeyPos(file, name string) (Pos, bool) {
	top, ok := s.top[file]
	if !ok {
		return Pos{}, false
	}
	if _, ok := top[name]; !ok {
		return Pos{}, false
	}
	return s.KeyPos(top, name), true
}

type span struct {
	file       string
	start, end int // lines
}

// span returns the lines of the source of an object: from its first line to the last line of its values
func (s *Sources) span(m map[interface{}]interface{}) span {
	p := s.MapPos(m)
	sp := span{file: p.File, start: p.Line, end: p.Line}
	var walk func(v interface{})
	walk = func(v interface{}) {
		switch tv := v.(type) {
		case map[interface{}]interface{}:
			for _, k := range s.Keys(tv) {
				ks := fmt.Sprint(k)
				sp.end = max(sp.end, s.KeyPos(tv, ks).Line, s.ValuePos(tv, ks).Line)
				walk(tv[k])
			}
		case []interface{}:
			for i, item := range tv {
				sp.end = max(sp.end, s.ItemPos(tv, i).Line)
				walk(item)
			}
		}
	}
	walk(m)
	return sp
}

// NodesAt returns the nodes of the tree that were loaded from the innermost node object of the source that contains
// the line of the file. There can be more nodes, e.g. when the object is included more than once, or for the
// combinations of for_vars.
func (s *Sources) NodesAt(t *Tree, file string, line int) []*Node {
	type candidate struct {
		raw  map[interface{}]interface{}
		size int
	}
	var best *candidate
	var nodes []*Node
	spans := make(map[uintptr]span)
	for _, n := range t.Nodes {
		if n == nil {
			continue
		}
		for _, raw := range n.sources {
			sp, ok := spans[mapID(raw)]
			if !ok {
				sp = s.span(raw)
				spans[mapID(raw)] = sp
			}
			if sp.file != file || line < sp.start || line > sp.end {
				continue
			}
			size := sp.end - sp.start
			switch {
			case best == nil || size < best.size:
				best = &candidate{raw: raw, size: size}
				nodes = []*Node{n}
			case mapID(raw) == mapID(best.raw):
				nodes = append(nodes, n)
			}
		}
	}
	return nodes
}

// VarPos returns the position where the variable is set (see VarSource)
func (s *Sources) VarPos(src VarSource, name string) (Pos, bool) {
	if src.Node == nil {
		return Pos{}, false
	}
	for _, raw := range src.Node.sources {
		if m, ok := raw[src.Prop].(map[interface{}]interface{}); ok {
			if _, ok := m[name]; ok {
				return s.KeyPos(m, name), true
			}
		}
	}
	return Pos{}, false
}
