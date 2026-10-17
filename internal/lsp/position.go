package lsp

import (
	"fmt"
	"net/url"
	"path/filepath"
	"runtime"
	"strings"
	"unicode/utf16"

	"github.com/nagylzs/runtree/internal/rt"
)

// The positions of the parser are 1 based lines and columns (in characters), LSP positions are 0 based lines and
// UTF-16 offsets.

// lineAt returns the line of text (0 based), without the line ending
func lineAt(text string, line int) string {
	for i := 0; i < line; i++ {
		nl := strings.IndexByte(text, '\n')
		if nl < 0 {
			return ""
		}
		text = text[nl+1:]
	}
	if nl := strings.IndexByte(text, '\n'); nl >= 0 {
		text = text[:nl]
	}
	return strings.TrimSuffix(text, "\r")
}

func utf16Len(r []rune) int {
	n := 0
	for _, c := range r {
		n += len(utf16.Encode([]rune{c}))
	}
	return n
}

// runeIndex converts a UTF-16 offset in a line to a character index
func runeIndex(r []rune, offset int) int {
	n := 0
	for i, c := range r {
		if n >= offset {
			return i
		}
		n += len(utf16.Encode([]rune{c}))
	}
	return len(r)
}

// tokenEnd returns the end (character index) of the YAML token that starts at start: a quoted string, a plain
// scalar or key, or the start of a flow collection. In a flow collection ([...] or {...}), a plain scalar ends at
// a comma or at the end of the collection.
func tokenEnd(r []rune, start int) int {
	if start >= len(r) {
		return start
	}
	switch r[start] {
	case '"':
		for i := start + 1; i < len(r); i++ {
			if r[i] == '\\' {
				i++
			} else if r[i] == '"' {
				return i + 1
			}
		}
		return len(r)
	case '\'':
		for i := start + 1; i < len(r); i++ {
			if r[i] == '\'' {
				if i+1 < len(r) && r[i+1] == '\'' {
					i++
					continue
				}
				return i + 1
			}
		}
		return len(r)
	case '[', '{':
		return start + 1
	}
	flow := inFlow(r[:start])
	i := start
	for i < len(r) {
		c := r[i]
		if flow && (c == ',' || c == ']' || c == '}') || c == ':' && (i+1 == len(r) || r[i+1] == ' ') ||
			c == '#' && i > start && r[i-1] == ' ' {
			break
		}
		i++
	}
	for i > start+1 && r[i-1] == ' ' {
		i--
	}
	if i == start {
		i++
	}
	return i
}

// inFlow tells if the end of the text is in a flow collection (roughly: the quoted strings are skipped)
func inFlow(r []rune) bool {
	depth := 0
	var quote rune
	for i := 0; i < len(r); i++ {
		c := r[i]
		switch {
		case quote != 0:
			if c == '\\' && quote == '"' {
				i++ // escaped
			} else if c == quote {
				quote = 0
			}
		case c == '"' || c == '\'':
			quote = c
		case c == '[' || c == '{':
			depth++
		case (c == ']' || c == '}') && depth > 0:
			depth--
		}
	}
	return depth > 0
}

// lspRange returns the range of the token at the parser position p in text. Without a column, it is the whole line.
func lspRange(text string, p rt.Pos) Range {
	line := max(p.Line-1, 0)
	r := []rune(lineAt(text, line))
	if p.Line <= 0 || p.Column <= 0 {
		start := 0
		for start < len(r) && (r[start] == ' ' || r[start] == '\t') {
			start++
		}
		return Range{Start: Position{line, utf16Len(r[:start])}, End: Position{line, utf16Len(r)}}
	}
	start := min(p.Column-1, len(r))
	end := tokenEnd(r, start)
	return Range{Start: Position{line, utf16Len(r[:start])}, End: Position{line, utf16Len(r[:end])}}
}

func uriToPath(uri string) (string, error) {
	u, err := url.Parse(uri)
	if err != nil {
		return "", err
	}
	if u.Scheme != "file" {
		return "", fmt.Errorf("not a file URI: %s", uri)
	}
	p := u.Path
	if runtime.GOOS == "windows" {
		// file:///c:/dir/file.yaml
		if len(p) >= 3 && p[0] == '/' && p[2] == ':' {
			p = p[1:]
		}
		p = filepath.FromSlash(p)
	}
	return filepath.Clean(p), nil
}

func pathToURI(path string) string {
	p := filepath.ToSlash(path)
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	return (&url.URL{Scheme: "file", Path: p}).String()
}
