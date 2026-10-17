package lsp

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/nagylzs/runtree/internal/rt"
)

// snapshot returns the open documents, their URIs, and the last analysis
func (s *Server) snapshot() (docs map[string]string, uris map[string]string, last *result) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return maps.Clone(s.docs), maps.Clone(s.uris), s.last
}

// textOf returns the contents of a file: the open document, or the file on the disk
func textOf(docs map[string]string, path string) string {
	if t, ok := docs[path]; ok {
		return t
	}
	data, _ := os.ReadFile(path)
	return string(data)
}

func newSources(docs map[string]string) *rt.Sources {
	src := rt.NewSources()
	src.Overlay = make(map[string][]byte, len(docs))
	for f, text := range docs {
		src.Overlay[f] = []byte(text)
	}
	return src
}

// varRefAt returns the {name} variable reference at the character index col of a line
func varRefAt(r []rune, col int) (name string, start, end int, ok bool) {
	for i := 0; i < len(r); i++ {
		if r[i] != '{' {
			continue
		}
		j := slices.Index(r[i+1:], '}')
		if j < 0 {
			break
		}
		j += i + 1
		name := string(r[i+1 : j])
		if col >= i && col <= j && rt.IsVarName(name) && (i == 0 || r[i-1] != '$') &&
			!strings.Contains(string(r[:i]), rt.RawTag) {
			return name, i, j + 1, true
		}
	}
	return "", 0, 0, false
}

// hover shows the values of the variable at the position, in the nodes that the line belongs to
func (s *Server) hover(p TextDocumentPositionParams) *Hover {
	path, err := uriToPath(p.TextDocument.URI)
	if err != nil {
		return nil
	}
	docs, _, last := s.snapshot()
	r := []rune(lineAt(textOf(docs, path), p.Position.Line))
	name, start, end, ok := varRefAt(r, runeIndex(r, p.Position.Character))
	if !ok || last == nil {
		return nil
	}

	// the values, with the paths of the nodes that have them
	type entry struct {
		text  string
		nodes []string
	}
	var entries []*entry
	byText := make(map[string]*entry)
	for _, lt := range last.trees {
		for _, n := range lt.src.NodesAt(lt.tree, path, p.Position.Line+1) {
			text := "not set"
			if v, ok := n.Calculated.Vars[name]; ok {
				text = "`" + formatValue(v) + "`"
				if src, ok := n.Calculated.VarSources[name]; ok && src.Node != nil {
					if src.Node == n {
						text += fmt.Sprintf(" — %s of the node", src.Prop)
					} else {
						text += fmt.Sprintf(" — %s of *%s*", src.Prop, src.Node.Path())
					}
					if pos, ok := lt.src.VarPos(src, name); ok {
						text += fmt.Sprintf(" ([%s:%d](%s#L%d))", filepath.Base(pos.File), pos.Line,
							pathToURI(pos.File), pos.Line)
					}
				}
			}
			e, ok := byText[text]
			if !ok {
				e = &entry{text: text}
				byText[text] = e
				entries = append(entries, e)
			}
			e.nodes = append(e.nodes, n.Path())
		}
	}
	if len(entries) == 0 {
		return nil
	}

	var b strings.Builder
	fmt.Fprintf(&b, "**{%s}**", name)
	if len(entries) == 1 && len(entries[0].nodes) == 1 {
		fmt.Fprintf(&b, " %s", entries[0].text)
	} else {
		const maxEntries, maxNodes = 10, 3
		for i, e := range entries {
			if i == maxEntries {
				fmt.Fprintf(&b, "\n- … and %d more", len(entries)-maxEntries)
				break
			}
			nodes := e.nodes
			more := ""
			if len(nodes) > maxNodes {
				more = fmt.Sprintf(" and %d more", len(nodes)-maxNodes)
				nodes = nodes[:maxNodes]
			}
			fmt.Fprintf(&b, "\n- %s\n\n  in *%s*%s", e.text, strings.Join(nodes, "*, *"), more)
		}
	}
	return &Hover{
		Contents: MarkupContent{Kind: "markdown", Value: b.String()},
		Range: &Range{Start: Position{p.Position.Line, utf16Len(r[:start])},
			End: Position{p.Position.Line, utf16Len(r[:end])}},
	}
}

func formatValue(v interface{}) string {
	switch tv := v.(type) {
	case string:
		return tv
	case []interface{}:
		items := make([]string, len(tv))
		for i, item := range tv {
			items[i] = fmt.Sprint(item)
		}
		return "[" + strings.Join(items, ", ") + "]"
	}
	return fmt.Sprint(v)
}

var (
	includeKeyRe  = regexp.MustCompile(`^(\s*)(?:-\s+)?include\s*:\s*`)
	listItemRe    = regexp.MustCompile(`^(\s*)-\s+`)
	emptyIncludes = regexp.MustCompile(`^(\s*)(?:-\s+)?include\s*:\s*(#.*)?$`)
)

// includeContext tells if the character index col of a line is in an include argument, and returns where the
// argument starts
func includeContext(text string, line int, col int) (start int, ok bool) {
	r := []rune(lineAt(text, line))
	col = min(col, len(r))
	before := string(r[:col])
	argStart := -1 // in runes
	if m := includeKeyRe.FindStringIndex(before); m != nil {
		rest := []rune(before[m[1]:])
		argStart = len([]rune(before[:m[1]]))
		if len(rest) > 0 && rest[0] == '[' {
			// a flow list: the argument is after the last [ or comma
			if i := strings.LastIndexAny(string(rest), "[,"); i >= 0 {
				argStart += len([]rune(string(rest)[:i])) + 1
			}
		}
	} else if m := listItemRe.FindStringSubmatchIndex(before); m != nil {
		// an item of a block list: the list must be the value of include
		indent := m[3] - m[2]
		for l := line - 1; l >= 0; l-- {
			prev := lineAt(text, l)
			trimmed := strings.TrimSpace(prev)
			if trimmed == "" || strings.HasPrefix(trimmed, "#") {
				continue
			}
			if pm := listItemRe.FindStringSubmatchIndex(prev); pm != nil && pm[3]-pm[2] == indent {
				continue // another item of the list
			}
			if km := emptyIncludes.FindStringSubmatchIndex(prev); km != nil && km[3]-km[2] <= indent {
				argStart = len([]rune(before[:m[1]]))
			}
			break
		}
	}
	if argStart < 0 {
		return 0, false
	}
	for argStart < col && (r[argStart] == ' ' || r[argStart] == '"' || r[argStart] == '\'') {
		argStart++
	}
	return argStart, true
}

// completion offers the top level objects for include arguments: the objects of the file, and "file|name" for the
// objects of the other runtree files
func (s *Server) completion(p TextDocumentPositionParams) *CompletionList {
	path, err := uriToPath(p.TextDocument.URI)
	if err != nil {
		return nil
	}
	docs, _, last := s.snapshot()
	text := textOf(docs, path)
	r := []rune(lineAt(text, p.Position.Line))
	col := min(runeIndex(r, p.Position.Character), len(r))
	start, ok := includeContext(text, p.Position.Line, col)
	if !ok {
		return nil
	}
	rng := Range{Start: Position{p.Position.Line, utf16Len(r[:start])},
		End: Position{p.Position.Line, utf16Len(r[:col])}}

	s.mu.Lock()
	files := slices.Clone(s.files)
	s.mu.Unlock()
	if last != nil {
		files = append(files, last.files...)
	}
	for f := range docs {
		if isTreeFileName(f) {
			files = append(files, f)
		}
	}
	slices.Sort(files)
	files = slices.Compact(files)

	src := newSources(docs)
	list := &CompletionList{Items: []CompletionItem{}}
	// the objects of the file are listed first
	add := func(file, object, label, sortPrefix string) {
		item := CompletionItem{Label: label, Kind: completionKindModule, SortText: sortPrefix + label,
			FilterText: label, TextEdit: &TextEdit{Range: rng, NewText: label}}
		if m, ok := src.Files[file][object].(map[interface{}]interface{}); ok {
			if title, ok := m["title"].(string); ok {
				item.Detail = rt.Unraw(title)
			}
		}
		list.Items = append(list.Items, item)
	}
	if _, err := src.AddFile(path); err == nil {
		for _, name := range src.TopNames(path) {
			if name != "tree" {
				add(path, name, name, "0")
			}
		}
	}
	dir := filepath.Dir(path)
	for _, f := range files {
		if f == path || len(list.Items) > 1000 {
			continue
		}
		if _, err := src.AddFile(f); err != nil {
			continue
		}
		rel, err := filepath.Rel(dir, f)
		if err != nil {
			continue
		}
		for _, name := range src.TopNames(f) {
			if name != "tree" {
				add(f, name, filepath.ToSlash(rel)+"|"+name, "1")
			}
		}
	}
	return list
}
