package lsp

import (
	"encoding/json"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

const hoverMain = `tree:
  title: root
  vars:
    user: admin
  for_vars:
    env: [dev, prod]
  seq:
    - include: lib.rt.yaml|job
    - title: local
      args: [ssh, "{user}@{env}"]
`

const hoverLib = `job:
  title: job
  def_vars:
    host: localhost
  args: [ssh, "{user}@{host}", "{nope}"]
`

func (c *testClient) hover(path string, line, character int) *Hover {
	c.t.Helper()
	res := c.request("textDocument/hover", TextDocumentPositionParams{
		TextDocument: TextDocumentIdentifier{URI: pathToURI(path)}, Position: Position{line, character}})
	var h *Hover
	if err := json.Unmarshal(res, &h); err != nil {
		c.t.Fatal(err)
	}
	return h
}

func TestHover(t *testing.T) {
	dir := writeFiles(t, map[string]string{"main.rt.yaml": hoverMain, "lib.rt.yaml": hoverLib})
	main, lib := filepath.Join(dir, "main.rt.yaml"), filepath.Join(dir, "lib.rt.yaml")
	c := startServer(t, dir)
	c.open(main, hoverMain)
	c.waitDiagnostics(lib, hasDiagnostics(1)) // the analysis is done
	for _, tc := range []struct {
		path            string
		line, character int
		want            []string
		r               Range
	}{
		// the same value in both for_vars combinations
		{main, 9, 20, []string{"**{user}**", "`admin` — vars of *root* ([main.rt.yaml:4]", "in *root › local*, *root › local*"},
			rng(9, 19, 25)},
		{main, 9, 27, []string{"`dev` — for_vars of *root*", "`prod` — for_vars of *root*"}, rng(9, 26, 31)},
		{lib, 4, 25, []string{"`localhost` — def_vars of the node ([lib.rt.yaml:4]", "in *root › job*, *root › job*"},
			rng(4, 22, 28)},
		{lib, 4, 32, []string{"**{nope}**", "not set"}, rng(4, 32, 38)},
	} {
		h := c.hover(tc.path, tc.line, tc.character)
		if h == nil {
			t.Errorf("%s:%d:%d: no hover", filepath.Base(tc.path), tc.line, tc.character)
			continue
		}
		for _, w := range tc.want {
			if !strings.Contains(h.Contents.Value, w) {
				t.Errorf("%s:%d:%d: hover is\n%s\nit does not contain %q", filepath.Base(tc.path), tc.line,
					tc.character, h.Contents.Value, w)
			}
		}
		if h.Range == nil || *h.Range != tc.r {
			t.Errorf("%s:%d:%d: range is %+v, want %+v", filepath.Base(tc.path), tc.line, tc.character, h.Range, tc.r)
		}
	}
	// not a variable reference
	if h := c.hover(main, 3, 6); h != nil {
		t.Errorf("hover is %+v, want none", h)
	}
}

// (the third line ends with a space)
const completionMain = "tree:\n  seq:\n    - include: \n" + `    - include: [local1, li]
  include:
    - lo
other:
  title: Other object
  args: ["true"]
local1:
  args: ["true"]
`

func (c *testClient) completion(path string, line, character int) *CompletionList {
	c.t.Helper()
	res := c.request("textDocument/completion", TextDocumentPositionParams{
		TextDocument: TextDocumentIdentifier{URI: pathToURI(path)}, Position: Position{line, character}})
	var l *CompletionList
	if err := json.Unmarshal(res, &l); err != nil {
		c.t.Fatal(err)
	}
	return l
}

func TestCompletion(t *testing.T) {
	dir := writeFiles(t, map[string]string{"main.rt.yaml": completionMain, "lib/lib.rt.yaml": hoverLib})
	main := filepath.Join(dir, "main.rt.yaml")
	c := startServer(t, dir)
	c.open(main, completionMain)
	c.waitDiagnostics(main, func(d []Diagnostic) bool { return len(d) > 0 }) // the analysis is done
	for _, tc := range []struct {
		line, character int
		start           int // of the replaced range
	}{
		{2, 15, 15}, // after "include: "
		{3, 25, 24}, // in the flow list, after "li"
		{5, 8, 6},   // in the block list, after "lo"
	} {
		l := c.completion(main, tc.line, tc.character)
		if l == nil {
			t.Errorf("%d:%d: no completion", tc.line, tc.character)
			continue
		}
		var labels []string
		for _, item := range l.Items {
			labels = append(labels, item.Label)
			if item.TextEdit == nil || item.TextEdit.Range != rng(tc.line, tc.start, tc.character) {
				t.Errorf("%d:%d: %s: text edit is %+v", tc.line, tc.character, item.Label, item.TextEdit)
			}
			if item.Label == "other" && item.Detail != "Other object" {
				t.Errorf("the detail of other is %q", item.Detail)
			}
		}
		slices.Sort(labels)
		if want := []string{"lib/lib.rt.yaml|job", "local1", "other"}; !slices.Equal(labels, want) {
			t.Errorf("%d:%d: items are %v, want %v", tc.line, tc.character, labels, want)
		}
	}
	// not an include
	if l := c.completion(main, 1, 6); l != nil {
		t.Errorf("completion is %+v, want none", l)
	}
}

func TestIncludeContext(t *testing.T) {
	for _, tc := range []struct {
		text      string
		line, col int
		start     int
		ok        bool
	}{
		{"x:\n  include: \"ab", 1, 14, 12, true},
		{"x:\n  include: [a, 'b", 1, 17, 16, true},
		{"x:\n  include:\n  - a\n  # c\n  - b", 4, 5, 4, true},
		{"x:\n  include:\n    - a\n  nodes:\n    - b", 4, 7, 0, false},
		{"x:\n  title: include: a", 1, 20, 0, false},
	} {
		start, ok := includeContext(tc.text, tc.line, tc.col)
		if ok != tc.ok || ok && start != tc.start {
			t.Errorf("%q %d:%d: %d, %v, want %d, %v", tc.text, tc.line, tc.col, start, ok, tc.start, tc.ok)
		}
	}
}
