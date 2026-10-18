package lsp

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

func hoverParams(uri string, line, character int) TextDocumentPositionParams {
	return TextDocumentPositionParams{TextDocument: TextDocumentIdentifier{URI: uri},
		Position: Position{Line: line, Character: character}}
}

// A request that the server does not know gets an error, a notification is ignored, and the server goes on
func TestUnknownMethods(t *testing.T) {
	dir := writeFiles(t, map[string]string{"main.rt.yaml": mainTree, "lib.rt.yaml": libTree})
	c := startServer(t, dir)
	m := c.call("textDocument/formatting", map[string]interface{}{})
	if m.Error == nil || m.Error.Code != errMethodNotFound {
		t.Errorf("the response is %+v, want a method not found error", m.Error)
	}
	c.notify("$/setTrace", map[string]string{"value": "off"})
	c.waitDiagnostics(filepath.Join(dir, "lib.rt.yaml"), hasDiagnostics(2)) // the server goes on
}

// Invalid parameters are an error response, not a crash
func TestInvalidParams(t *testing.T) {
	c := startServer(t, t.TempDir())
	for _, method := range []string{"textDocument/hover", "textDocument/definition", "textDocument/completion"} {
		m := c.call(method, json.RawMessage(`"not an object"`))
		if m.Error == nil || m.Error.Code != errInvalidParams {
			t.Errorf("%s: the response is %+v, want an invalid params error", method, m.Error)
		}
	}
	// the server still works
	if res := c.request("textDocument/hover", hoverParams(pathToURI("/nothere.rt.yaml"), 0, 0)); string(res) != "null" {
		t.Errorf("hover: %s", res)
	}
}

// Documents that are not files (e.g. a new, unsaved editor tab) are ignored
func TestNonFileDocument(t *testing.T) {
	dir := writeFiles(t, map[string]string{"main.rt.yaml": mainTree, "lib.rt.yaml": libTree})
	c := startServer(t, dir)
	uri := "untitled:Untitled-1"
	c.notify("textDocument/didOpen", DidOpenTextDocumentParams{TextDocument: TextDocumentItem{
		URI: uri, LanguageID: "yaml", Version: 1, Text: "tree:\n  max_proc: x\n"}})
	for _, method := range []string{"textDocument/hover", "textDocument/definition", "textDocument/completion"} {
		if m := c.call(method, hoverParams(uri, 1, 3)); m.Error != nil {
			t.Errorf("%s: %s", method, m.Error.Message)
		}
	}
	c.waitDiagnostics(filepath.Join(dir, "lib.rt.yaml"), hasDiagnostics(2)) // the server goes on
	if d, ok := c.diags[uri]; ok {
		t.Errorf("diagnostics for %s: %v", uri, d)
	}
}

// The diagnostics of a document that is not saved are removed when it is closed
func TestClosedUnsavedDocument(t *testing.T) {
	dir := t.TempDir()
	c := startServer(t, dir)
	path := filepath.Join(dir, "new.rt.yaml")
	c.open(path, "tree:\n  max_proc: x\n  args: [\"true\"]\n")
	c.waitDiagnostics(path, hasDiagnostics(1))
	c.notify("textDocument/didClose", DidCloseTextDocumentParams{TextDocument: TextDocumentIdentifier{
		URI: pathToURI(path)}})
	c.waitDiagnostics(path, hasDiagnostics(0))
}

// A YAML syntax error is a diagnostic, and it goes away when it is fixed
func TestYAMLSyntaxError(t *testing.T) {
	dir := t.TempDir()
	c := startServer(t, dir)
	path := filepath.Join(dir, "broken.rt.yaml")
	c.open(path, "tree:\n  seq: [\n")
	d := c.waitDiagnostics(path, hasDiagnostics(1))
	checkDiagnostic(t, d[0], 1, rng(2, 0, 0), "did not find expected node content")
	c.change(path, "tree:\n  seq: [[\"true\"]]\n")
	c.waitDiagnostics(path, hasDiagnostics(0))
}

// Paths with spaces and non-ASCII characters are percent-encoded in the URIs
func TestEncodedPaths(t *testing.T) {
	dir := writeFiles(t, map[string]string{
		"my dir/main.rt.yaml":          "tree:\n  seq:\n    - include: \"árvíztűrő lib.rt.yaml|job\"\n",
		"my dir/árvíztűrő lib.rt.yaml": libTree,
	})
	c := startServer(t, dir)
	lib := filepath.Join(dir, "my dir", "árvíztűrő lib.rt.yaml")
	if uri := pathToURI(lib); !strings.Contains(uri, "my%20dir") {
		t.Errorf("the URI is %s", uri)
	}
	d := c.waitDiagnostics(lib, hasDiagnostics(2))
	checkDiagnostic(t, d[0], 2, rng(1, 9, 22), "undefined variable {host}")
	// an editor may encode more characters than pathToURI
	uri := strings.ReplaceAll(pathToURI(lib), "-", "%2D")
	if path, err := uriToPath(uri); err != nil || path != lib {
		t.Errorf("uriToPath(%s) = %s, %v", uri, path, err)
	}
}
