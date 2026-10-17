package lsp

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/nagylzs/runtree/internal/config"
	"github.com/nagylzs/runtree/internal/rt"
)

// treeKeyRe finds the top level "tree" object of a tree file
var treeKeyRe = regexp.MustCompile(`(?m)^tree[ \t]*:`)

// skipDirs are not searched for tree files (and the hidden directories)
var skipDirs = map[string]bool{"node_modules": true, "vendor": true, "dist": true}

const maxScannedFiles = 10000

func isYAMLFile(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	return ext == ".yaml" || ext == ".yml"
}

// isTreeFileName tells if the file is named like a tree file (*.rt.yaml)
func isTreeFileName(path string) bool {
	base := strings.ToLower(filepath.Base(path))
	return strings.HasSuffix(base, ".rt.yaml") || strings.HasSuffix(base, ".rt.yml")
}

// findFiles returns the tree files (the YAML files that have a top level "tree" object) in the directories, and the
// runtree files: the tree files and the files that are named *.rt.yaml
func findFiles(dirs []string) (trees []string, files []string) {
	found := make(map[string]bool)
	named := make(map[string]bool)
	scanned := 0
	for _, dir := range dirs {
		_ = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.IsDir() {
				if path != dir && (skipDirs[d.Name()] || strings.HasPrefix(d.Name(), ".")) {
					return filepath.SkipDir
				}
				return nil
			}
			if !isYAMLFile(path) {
				return nil
			}
			if scanned++; scanned > maxScannedFiles {
				return filepath.SkipAll
			}
			if info, err := d.Info(); err != nil || info.Size() > rt.MaxYamlFileSize {
				return nil
			}
			if data, err := os.ReadFile(path); err == nil && treeKeyRe.Match(data) {
				found[path] = true
			}
			if isTreeFileName(path) || found[path] {
				named[path] = true
			}
			return nil
		})
	}
	return sortedKeys(found), sortedKeys(named)
}

// result is the result of an analysis
type result struct {
	diags map[string][]Diagnostic
	trees []loadedTree // the trees (also the ones with errors, with their nodes that could be loaded)
	files []string     // the runtree files: the files that the trees include, and the open *.rt.yaml files
}

type loadedTree struct {
	tree *rt.Tree
	src  *rt.Sources
}

type analysis struct {
	docs  map[string]string // the open documents, by path
	texts map[string]string // the contents of the files, for the positions
	diags map[string][]Diagnostic
	seen  map[string]bool
}

// analyze loads the trees (the tree files of the workspace and the open documents that are tree files), and returns
// the diagnostics of every runtree file: the files that the trees include, and the open *.rt.yaml files. The open
// documents are used instead of the files on the disk.
func analyze(docs map[string]string, workspaceTrees []string) *result {
	res := &result{}
	a := &analysis{docs: docs, texts: make(map[string]string), diags: make(map[string][]Diagnostic),
		seen: make(map[string]bool)}
	overlay := make(map[string][]byte, len(docs))
	for p, text := range docs {
		overlay[p] = []byte(text)
	}

	roots := make(map[string]bool)
	for _, p := range workspaceTrees {
		if text, open := docs[p]; !open || treeKeyRe.MatchString(text) {
			roots[p] = true
		}
	}
	for p, text := range docs {
		if treeKeyRe.MatchString(text) {
			roots[p] = true
		}
	}
	reached := make(map[string]bool)
	for _, root := range sortedKeys(roots) {
		src := rt.NewSources()
		src.Overlay = overlay
		_, filename, err := rt.AddTree(root, src)
		if err == nil {
			var tree *rt.Tree
			tree, err = rt.LoadPartialTree(src, filename, "tree", config.DefaultMaxDepth)
			if tree != nil {
				res.trees = append(res.trees, loadedTree{tree: tree, src: src})
			}
		}
		a.addErrors(err, root)
		for _, w := range src.Warnings() {
			a.add(w)
		}
		for _, f := range src.LoadedFiles() {
			reached[f] = true
		}
	}

	// The open library files that no tree includes: every top level object is checked, but not the variables (they
	// are set where the objects are included).
	for _, p := range sortedKeys(docs) {
		if reached[p] || !isTreeFileName(p) {
			continue
		}
		reached[p] = true
		src := rt.NewSources()
		src.Overlay = overlay
		if _, err := src.AddFile(p); err != nil {
			a.addErrors(err, p)
			continue
		}
		for _, name := range src.TopNames(p) {
			objSrc := rt.NewSources()
			objSrc.Overlay = overlay
			_, err := objSrc.AddFile(p)
			if err == nil {
				_, err = rt.LoadTree(objSrc, p, name, config.DefaultMaxDepth)
			}
			a.addErrors(err, p)
			for _, w := range objSrc.Warnings() {
				if w.Code != rt.CodeUndefinedVariable {
					a.add(w)
				}
			}
			for _, f := range objSrc.LoadedFiles() {
				reached[f] = true
			}
		}
	}

	for f := range reached {
		if a.diags[f] == nil {
			a.diags[f] = []Diagnostic{}
		}
	}
	for _, d := range a.diags {
		slices.SortStableFunc(d, func(x, y Diagnostic) int {
			if x.Range.Start.Line != y.Range.Start.Line {
				return x.Range.Start.Line - y.Range.Start.Line
			}
			return x.Range.Start.Character - y.Range.Start.Character
		})
	}
	res.diags = a.diags
	res.files = sortedKeys(reached)
	return res
}

func (a *analysis) addErrors(err error, file string) {
	if err == nil {
		return
	}
	var list rt.ParseErrors
	var pe *rt.ParseError
	switch {
	case errors.As(err, &list):
		for _, e := range list {
			a.add(e)
		}
	case errors.As(err, &pe):
		a.add(pe)
	default:
		a.add(&rt.ParseError{Pos: rt.Pos{File: file}, Message: err.Error()})
	}
}

func (a *analysis) add(pe *rt.ParseError) {
	file := pe.Pos.File
	if file == "" {
		return
	}
	key := fmt.Sprintf("%s\x00%d\x00%d\x00%v\x00%s", file, pe.Pos.Line, pe.Pos.Column, pe.Warning, pe.Message)
	if a.seen[key] {
		return
	}
	a.seen[key] = true

	msg := pe.Message
	if pe.Count > 1 {
		msg += fmt.Sprintf(" (found %d times)", pe.Count)
	}
	if pe.Node != "" {
		msg += "\nin node: " + pe.Node
	}
	d := Diagnostic{Range: lspRange(a.text(file), pe.Pos), Severity: SeverityError, Code: pe.Code, Source: "runtree",
		Message: msg}
	if pe.Warning {
		d.Severity = SeverityWarning
	}
	for _, inc := range pe.Included {
		d.RelatedInformation = append(d.RelatedInformation, DiagnosticRelatedInformation{
			Location: Location{URI: pathToURI(inc.Pos.File), Range: lspRange(a.text(inc.Pos.File), inc.Pos)},
			Message:  fmt.Sprintf("included from here (include %q)", inc.Name),
		})
	}
	a.diags[file] = append(a.diags[file], d)
}

// text returns the contents of a file: the open document, or the file on the disk
func (a *analysis) text(file string) string {
	if text, ok := a.docs[file]; ok {
		return text
	}
	if text, ok := a.texts[file]; ok {
		return text
	}
	data, _ := os.ReadFile(file)
	a.texts[file] = string(data)
	return a.texts[file]
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}
