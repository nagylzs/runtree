package lsp

import (
	"bufio"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type testClient struct {
	t      *testing.T
	out    *transport // to the server
	msgs   chan *message
	nextID int
	diags  map[string][]Diagnostic // the last published diagnostics, by URI
}

// startServer starts a server with the workspace folder dir, and initializes it
func startServer(t *testing.T, dir string) *testClient {
	t.Helper()
	cr, cw := io.Pipe() // client -> server
	sr, sw := io.Pipe() // server -> client
	srv := NewServer(cr, sw, io.Discard)
	srv.Delay = 10 * time.Millisecond
	done := make(chan int, 1)
	go func() {
		done <- srv.Run()
		_ = sw.Close()
	}()
	c := &testClient{t: t, out: &transport{out: cw}, msgs: make(chan *message, 100), diags: make(map[string][]Diagnostic)}
	go func() {
		in := &transport{in: bufio.NewReader(sr)}
		for {
			m, err := in.read()
			if err != nil {
				close(c.msgs)
				return
			}
			c.msgs <- m
		}
	}()
	c.request("initialize", InitializeParams{WorkspaceFolders: []WorkspaceFolder{{URI: pathToURI(dir), Name: "w"}}})
	c.notify("initialized", struct{}{})
	t.Cleanup(func() {
		c.request("shutdown", nil)
		c.notify("exit", nil)
		select {
		case code := <-done:
			if code != 0 {
				t.Errorf("exit code %d", code)
			}
		case <-time.After(5 * time.Second):
			t.Errorf("the server did not exit")
		}
	})
	return c
}

func (c *testClient) notify(method string, params interface{}) {
	c.t.Helper()
	if err := c.out.notify(method, params); err != nil {
		c.t.Fatal(err)
	}
}

// receive handles a message from the server, it returns false on timeout
func (c *testClient) receive(timeout <-chan time.Time) (*message, bool) {
	select {
	case m, ok := <-c.msgs:
		if !ok {
			c.t.Fatal("the server closed the connection")
		}
		if m.Method == "textDocument/publishDiagnostics" {
			var p PublishDiagnosticsParams
			if err := json.Unmarshal(m.Params, &p); err != nil {
				c.t.Fatal(err)
			}
			c.diags[p.URI] = p.Diagnostics
		}
		return m, true
	case <-timeout:
		return nil, false
	}
}

func (c *testClient) request(method string, params interface{}) json.RawMessage {
	c.t.Helper()
	c.nextID++
	id := json.RawMessage(strings.TrimSpace(string(mustJSON(c.t, c.nextID))))
	if err := c.out.write(message{JSONRPC: "2.0", ID: id, Method: method, Params: mustJSON(c.t, params)}); err != nil {
		c.t.Fatal(err)
	}
	timeout := time.After(5 * time.Second)
	for {
		m, ok := c.receive(timeout)
		if !ok {
			c.t.Fatalf("no response to %s", method)
		}
		if string(m.ID) == string(id) && m.Method == "" {
			if m.Error != nil {
				c.t.Fatalf("%s: %s", method, m.Error.Message)
			}
			return m.Result
		}
	}
}

// waitDiagnostics waits until the diagnostics of the file satisfy cond, and returns them
func (c *testClient) waitDiagnostics(path string, cond func([]Diagnostic) bool) []Diagnostic {
	c.t.Helper()
	uri := pathToURI(path)
	timeout := time.After(5 * time.Second)
	for {
		if d, ok := c.diags[uri]; ok && cond(d) {
			return d
		}
		if _, ok := c.receive(timeout); !ok {
			c.t.Fatalf("timeout, the diagnostics of %s are %v", filepath.Base(path), c.diags[uri])
		}
	}
}

func (c *testClient) open(path, text string) {
	c.notify("textDocument/didOpen", DidOpenTextDocumentParams{TextDocument: TextDocumentItem{
		URI: pathToURI(path), LanguageID: "yaml", Version: 1, Text: text}})
}

func (c *testClient) change(path, text string) {
	c.notify("textDocument/didChange", map[string]interface{}{
		"textDocument":   map[string]interface{}{"uri": pathToURI(path), "version": 2},
		"contentChanges": []map[string]string{{"text": text}},
	})
}

func mustJSON(t *testing.T, v interface{}) json.RawMessage {
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func writeFiles(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, content := range files {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func hasDiagnostics(n int) func([]Diagnostic) bool {
	return func(d []Diagnostic) bool { return len(d) == n }
}

func checkDiagnostic(t *testing.T, d Diagnostic, severity int, r Range, msg string) {
	t.Helper()
	if d.Severity != severity || d.Range != r || !strings.Contains(d.Message, msg) || d.Source != "runtree" {
		t.Errorf("diagnostic is %+v, want severity %d, range %+v, message %q", d, severity, r, msg)
	}
}

func rng(line, start, end int) Range {
	return Range{Start: Position{line, start}, End: Position{line, end}}
}

const mainTree = `tree:
  title: main
  seq:
    - include: lib.rt.yaml|job
`

const libTree = `job:
  title: job on {host}
  args: echo
`

// The errors and warnings of an included file are published for that file, with the include chain.
func TestDiagnosticsOfIncludedFile(t *testing.T) {
	dir := writeFiles(t, map[string]string{"main.rt.yaml": mainTree, "lib.rt.yaml": libTree})
	c := startServer(t, dir)
	c.open(filepath.Join(dir, "main.rt.yaml"), mainTree)
	d := c.waitDiagnostics(filepath.Join(dir, "lib.rt.yaml"), hasDiagnostics(2))
	checkDiagnostic(t, d[0], SeverityWarning, rng(1, 9, 22), "undefined variable {host}, it is not set where it is included")
	checkDiagnostic(t, d[1], SeverityError, rng(2, 8, 12), "invalid args: it is a string, expected a list of strings")
	if d[0].Code != "undefined-variable" {
		t.Errorf("code is %q", d[0].Code)
	}
	if len(d[1].RelatedInformation) != 1 {
		t.Fatalf("related information is %v, want the include", d[1].RelatedInformation)
	}
	rel := d[1].RelatedInformation[0]
	if rel.Location.URI != pathToURI(filepath.Join(dir, "main.rt.yaml")) || rel.Location.Range != rng(3, 15, 30) {
		t.Errorf("related information is %+v", rel)
	}
	c.waitDiagnostics(filepath.Join(dir, "main.rt.yaml"), hasDiagnostics(0))
}

// The open documents are checked with their unsaved contents.
func TestUnsavedChanges(t *testing.T) {
	dir := writeFiles(t, map[string]string{"main.rt.yaml": "tree:\n  args: [\"true\"]\n"})
	main := filepath.Join(dir, "main.rt.yaml")
	c := startServer(t, dir)
	c.open(main, "tree:\n  args: [\"true\"]\n")
	c.waitDiagnostics(main, hasDiagnostics(0))
	c.change(main, "tree:\n  args: [\"true\"]\n  on_eror: paused\n")
	d := c.waitDiagnostics(main, hasDiagnostics(1))
	checkDiagnostic(t, d[0], SeverityError, rng(2, 2, 9), `unknown property "on_eror" (did you mean "on_error"?)`)
	c.change(main, "tree:\n  args: [\"true\"]\n  on_error: paused\n")
	c.waitDiagnostics(main, hasDiagnostics(0))
}

// A library file that no tree includes: its objects are checked, but the variables are not.
func TestLibraryFile(t *testing.T) {
	dir := writeFiles(t, map[string]string{"lib.rt.yaml": libTree})
	lib := filepath.Join(dir, "lib.rt.yaml")
	c := startServer(t, dir)
	c.open(lib, libTree)
	d := c.waitDiagnostics(lib, hasDiagnostics(1))
	checkDiagnostic(t, d[0], SeverityError, rng(2, 8, 12), "invalid args")
}

// The trees of the workspace are found (also when they are not named *.rt.yaml), and the files that they include
// are checked as part of them.
func TestWorkspaceTree(t *testing.T) {
	dir := writeFiles(t, map[string]string{
		"deploy.yaml":     strings.ReplaceAll(mainTree, "lib.rt.yaml", "lib/lib.yaml"),
		"lib/lib.yaml":    libTree,
		"other.yaml":      "kind: Deployment\n",
		".git/x/tree.yml": "tree:\n  bogus: 1\n",
	})
	lib := filepath.Join(dir, "lib", "lib.yaml")
	c := startServer(t, dir)
	c.open(lib, libTree)
	d := c.waitDiagnostics(lib, hasDiagnostics(2))
	if len(d[0].RelatedInformation) != 1 ||
		d[0].RelatedInformation[0].Location.URI != pathToURI(filepath.Join(dir, "deploy.yaml")) {
		t.Errorf("related information is %+v, want the include in deploy.yaml", d[0].RelatedInformation)
	}
	if len(c.diags) != 1 {
		t.Errorf("diagnostics are published for %d files, want only lib.yaml: %v", len(c.diags), c.diags)
	}
}

// Go to definition on an include argument goes to the object.
func TestDefinition(t *testing.T) {
	main := "tree:\n  include: [lib.rt.yaml|job, \"local\"]\nlocal:\n  args: [\"true\"]\n"
	dir := writeFiles(t, map[string]string{"main.rt.yaml": main, "lib.rt.yaml": "# lib\njob:\n  args: [\"true\"]\n"})
	c := startServer(t, dir)
	c.open(filepath.Join(dir, "main.rt.yaml"), main)
	for _, tc := range []struct {
		character int
		file      string
		r         Range
	}{
		{12, "lib.rt.yaml", rng(1, 0, 3)},  // the start of lib.rt.yaml|job
		{27, "lib.rt.yaml", rng(1, 0, 3)},  // its end
		{31, "main.rt.yaml", rng(2, 0, 5)}, // "local"
	} {
		res := c.request("textDocument/definition", TextDocumentPositionParams{
			TextDocument: TextDocumentIdentifier{URI: pathToURI(filepath.Join(dir, "main.rt.yaml"))},
			Position:     Position{Line: 1, Character: tc.character},
		})
		var loc *Location
		if err := json.Unmarshal(res, &loc); err != nil || loc == nil {
			t.Fatalf("character %d: no location (%s)", tc.character, res)
		}
		if loc.URI != pathToURI(filepath.Join(dir, tc.file)) || loc.Range != tc.r {
			t.Errorf("character %d: location is %+v, want %s %+v", tc.character, loc, tc.file, tc.r)
		}
	}
	res := c.request("textDocument/definition", TextDocumentPositionParams{
		TextDocument: TextDocumentIdentifier{URI: pathToURI(filepath.Join(dir, "main.rt.yaml"))},
		Position:     Position{Line: 0, Character: 1},
	})
	if string(res) != "null" {
		t.Errorf("result is %s, want null (not an include)", res)
	}
}

// The ranges are in UTF-16 code units.
func TestDiagnosticRangeUTF16(t *testing.T) {
	text := "tree:\n  args: [\"árvíztűrő 😀\", 5]\n"
	dir := writeFiles(t, map[string]string{"main.rt.yaml": text})
	main := filepath.Join(dir, "main.rt.yaml")
	c := startServer(t, dir)
	c.open(main, text)
	d := c.waitDiagnostics(main, hasDiagnostics(1))
	// the emoji is 2 UTF-16 code units
	checkDiagnostic(t, d[0], SeverityError, rng(1, 25, 26), "item 2 is a number")
}

func TestTokenEnd(t *testing.T) {
	for _, tc := range []struct {
		line string
		end  int
	}{
		{`"a\"b", x`, 6},
		{`'it''s' x`, 7},
		{`plain value # comment`, 11},
		{`key: value`, 3},
		{`[a, b]`, 1},
		{``, 0},
		{`job on {host}`, 13},
	} {
		if got := tokenEnd([]rune(tc.line), 0); got != tc.end {
			t.Errorf("tokenEnd(%q) is %d, want %d", tc.line, got, tc.end)
		}
	}
	// in a flow collection
	for _, tc := range []struct {
		line       string
		start, end int
	}{
		{`[a b, c]`, 1, 4},
		{`{k: "x]", v: y}`, 13, 14},
		{`["{", plain}x]`, 6, 11},
		{`["a", b c]`, 6, 9},
		{`k: "a\"[" more}`, 10, 15}, // not in a flow collection, the [ is in a string
	} {
		if got := tokenEnd([]rune(tc.line), tc.start); got != tc.end {
			t.Errorf("tokenEnd(%q, %d) is %d, want %d", tc.line, tc.start, got, tc.end)
		}
	}
}
