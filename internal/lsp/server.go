package lsp

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"maps"
	"os"
	"runtime/debug"
	"slices"
	"sync"
	"time"

	"github.com/nagylzs/runtree/internal/rt"
	"github.com/nagylzs/runtree/internal/version"
)

// Server is a language server for runtree tree files: it publishes the errors and warnings of the trees (like
// runtree --check), and it finds the objects that the includes refer to.
type Server struct {
	t      *transport
	logger *log.Logger
	Delay  time.Duration // the analysis waits this much after a change (more changes may follow)

	mu        sync.Mutex
	docs      map[string]string // the open documents, by path
	uris      map[string]string // the URIs of the open documents, by path
	folders   []string          // the workspace folders
	trees     []string          // the tree files in the workspace folders
	rescan    bool              // the workspace folders must be searched for tree files again
	published map[string]bool   // the files that have (not empty) published diagnostics
	timer     *time.Timer
	shutdown  bool

	analyzeMu sync.Mutex // only one analysis runs at a time
}

func NewServer(in io.Reader, out io.Writer, logOut io.Writer) *Server {
	return &Server{
		t:         &transport{in: bufio.NewReader(in), out: out},
		logger:    log.New(logOut, "runtree-lsp: ", log.LstdFlags),
		Delay:     300 * time.Millisecond,
		docs:      make(map[string]string),
		uris:      make(map[string]string),
		published: make(map[string]bool),
	}
}

// Run serves the client until it sends an exit notification (or the input is closed), and returns the exit code.
func (s *Server) Run() int {
	for {
		m, err := s.t.read()
		if err != nil {
			if err != io.EOF {
				s.logger.Printf("read: %v", err)
			}
			return 1
		}
		if m.Method == "exit" {
			s.mu.Lock()
			defer s.mu.Unlock()
			if s.shutdown {
				return 0
			}
			return 1
		}
		s.handle(m)
	}
}

func (s *Server) handle(m *message) {
	isRequest := len(m.ID) > 0
	defer func() {
		if r := recover(); r != nil {
			s.logger.Printf("internal error in %s: %v\n%s", m.Method, r, debug.Stack())
			if isRequest {
				_ = s.t.respondError(m.ID, -32603, fmt.Sprintf("internal error: %v", r))
			}
		}
	}()
	var err error
	switch m.Method {
	case "initialize":
		var p InitializeParams
		if err = json.Unmarshal(m.Params, &p); err == nil {
			s.initialize(p)
			err = s.t.respond(m.ID, map[string]interface{}{
				"capabilities": map[string]interface{}{
					"textDocumentSync": map[string]interface{}{
						"openClose": true,
						"change":    1, // full
						"save":      map[string]bool{"includeText": false},
					},
					"definitionProvider": true,
				},
				"serverInfo": map[string]string{"name": "runtree-lsp", "version": version.Commit},
			})
		}
	case "initialized", "workspace/didChangeWatchedFiles":
		s.mu.Lock()
		s.rescan = true
		s.mu.Unlock()
		s.schedule()
	case "shutdown":
		s.mu.Lock()
		s.shutdown = true
		s.mu.Unlock()
		err = s.t.respond(m.ID, nil)
	case "textDocument/didOpen":
		var p DidOpenTextDocumentParams
		if err = json.Unmarshal(m.Params, &p); err == nil {
			err = s.setDocument(p.TextDocument.URI, &p.TextDocument.Text)
		}
	case "textDocument/didChange":
		var p DidChangeTextDocumentParams
		if err = json.Unmarshal(m.Params, &p); err == nil && len(p.ContentChanges) > 0 {
			err = s.setDocument(p.TextDocument.URI, &p.ContentChanges[len(p.ContentChanges)-1].Text)
		}
	case "textDocument/didClose":
		var p DidCloseTextDocumentParams
		if err = json.Unmarshal(m.Params, &p); err == nil {
			err = s.setDocument(p.TextDocument.URI, nil)
		}
	case "textDocument/didSave":
		// the tree files of the workspace may have changed
		s.mu.Lock()
		s.rescan = true
		s.mu.Unlock()
		s.schedule()
	case "textDocument/definition":
		var p TextDocumentPositionParams
		if err = json.Unmarshal(m.Params, &p); err == nil {
			err = s.t.respond(m.ID, s.definition(p))
		}
	default:
		// notifications that are not handled are ignored ($/cancelRequest, $/setTrace, ...)
		if isRequest {
			err = s.t.respondError(m.ID, errMethodNotFound, "method not found: "+m.Method)
		}
	}
	if err != nil {
		s.logger.Printf("%s: %v", m.Method, err)
		if isRequest {
			_ = s.t.respondError(m.ID, errInvalidParams, err.Error())
		}
	}
}

func (s *Server) initialize(p InitializeParams) {
	var uris []string
	for _, f := range p.WorkspaceFolders {
		uris = append(uris, f.URI)
	}
	if len(uris) == 0 && p.RootURI != "" {
		uris = append(uris, p.RootURI)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, uri := range uris {
		if path, err := uriToPath(uri); err == nil {
			s.folders = append(s.folders, path)
		}
	}
	s.rescan = true
}

// setDocument stores the text of an open document, or forgets it when it is closed (text is nil)
func (s *Server) setDocument(uri string, text *string) error {
	path, err := uriToPath(uri)
	if err != nil {
		return err
	}
	s.mu.Lock()
	if text == nil {
		delete(s.docs, path)
		delete(s.uris, path)
	} else {
		s.docs[path] = *text
		s.uris[path] = uri
	}
	s.mu.Unlock()
	s.schedule()
	return nil
}

// schedule starts an analysis after the delay (a new change restarts the delay)
func (s *Server) schedule() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.timer != nil {
		s.timer.Stop()
	}
	s.timer = time.AfterFunc(s.Delay, s.runAnalysis)
}

func (s *Server) runAnalysis() {
	s.analyzeMu.Lock()
	defer s.analyzeMu.Unlock()

	s.mu.Lock()
	docs := maps.Clone(s.docs)
	rescan := s.rescan
	s.rescan = false
	folders := slices.Clone(s.folders)
	s.mu.Unlock()
	if rescan {
		trees := findTrees(folders)
		s.mu.Lock()
		s.trees = trees
		s.mu.Unlock()
	}
	s.mu.Lock()
	trees := s.trees
	s.mu.Unlock()

	var diags map[string][]Diagnostic
	func() {
		defer func() {
			if r := recover(); r != nil {
				s.logger.Printf("internal error in the analysis: %v\n%s", r, debug.Stack())
			}
		}()
		diags = analyze(docs, trees)
	}()
	if diags == nil {
		return
	}

	s.mu.Lock()
	uris := maps.Clone(s.uris)
	prev := s.published
	s.published = make(map[string]bool)
	for f, d := range diags {
		if len(d) > 0 {
			s.published[f] = true
		}
	}
	s.mu.Unlock()
	for f := range prev {
		if _, ok := diags[f]; !ok {
			diags[f] = []Diagnostic{} // the problems are gone (or the file is not a runtree file anymore)
		}
	}
	for _, f := range sortedKeys(diags) {
		_, open := uris[f]
		if len(diags[f]) == 0 && !prev[f] && !open {
			continue
		}
		uri, ok := uris[f]
		if !ok {
			uri = pathToURI(f)
		}
		if err := s.t.notify("textDocument/publishDiagnostics",
			PublishDiagnosticsParams{URI: uri, Diagnostics: diags[f]}); err != nil {
			s.logger.Printf("publishDiagnostics: %v", err)
		}
	}
}

// definition returns the location of the object that an include argument at the position refers to, or nil
func (s *Server) definition(p TextDocumentPositionParams) *Location {
	path, err := uriToPath(p.TextDocument.URI)
	if err != nil {
		return nil
	}
	s.mu.Lock()
	docs := maps.Clone(s.docs)
	uris := maps.Clone(s.uris)
	s.mu.Unlock()
	src := rt.NewSources()
	src.Overlay = make(map[string][]byte, len(docs))
	for f, text := range docs {
		src.Overlay[f] = []byte(text)
	}
	if _, err := src.AddFile(path); err != nil {
		return nil
	}
	text := func(f string) string {
		if t, ok := docs[f]; ok {
			return t
		}
		data, _ := os.ReadFile(f)
		return string(data)
	}
	line := []rune(lineAt(text(path), p.Position.Line))
	col := runeIndex(line, p.Position.Character)
	for _, ref := range src.IncludeRefs(path) {
		if ref.Pos.Line != p.Position.Line+1 {
			continue
		}
		start := ref.Pos.Column - 1
		if col < start || col > tokenEnd(line, start) {
			continue
		}
		file, object := rt.IncludeTarget(path, ref.Name)
		if _, err := src.AddFile(file); err != nil {
			return nil
		}
		pos, ok := src.TopKeyPos(file, object)
		if !ok {
			return nil
		}
		uri, ok := uris[file]
		if !ok {
			uri = pathToURI(file)
		}
		return &Location{URI: uri, Range: lspRange(text(file), pos)}
	}
	return nil
}

// Main runs the language server on stdin and stdout.
func Main() int {
	// the protocol uses stdout, anything else that would be printed goes to stderr
	out := os.Stdout
	os.Stdout = os.Stderr
	return NewServer(os.Stdin, out, os.Stderr).Run()
}
