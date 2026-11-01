package gtkui

import (
	"maps"
	"slices"
	"strings"
	"unicode"

	"github.com/nagylzs/runtree/internal/rt"
	"github.com/nagylzs/set"
)

// nodeQuery is the search of the navigator: the words that a node must have and the ones that it must not have
// (lower case), see parseNodeQuery
type nodeQuery struct {
	include, exclude []string
}

// parseNodeQuery parses the text of the search entry: the words are separated by spaces, a node must have all of them
// (word or +word), and none of those with a minus (-word); "two words" is a phrase (+ and - work with it too, and
// "-word" or +-word is a word that starts with a minus). The case does not matter.
func parseNodeQuery(s string) nodeQuery {
	var q nodeQuery
	for {
		s = strings.TrimLeftFunc(s, unicode.IsSpace)
		if s == "" {
			return q
		}
		exclude := false
		switch s[0] {
		case '-':
			exclude, s = true, s[1:]
		case '+':
			s = s[1:]
		}
		var word string
		if rest, ok := strings.CutPrefix(s, `"`); ok {
			word, s, _ = strings.Cut(rest, `"`) // an unclosed phrase lasts until the end
		} else {
			end := strings.IndexFunc(s, unicode.IsSpace)
			if end < 0 {
				end = len(s)
			}
			word, s = s[:end], s[end:]
		}
		if strings.TrimSpace(word) == "" {
			continue // e.g. a lone minus while the word is being typed
		}
		word = strings.ToLower(word)
		if exclude {
			q.exclude = append(q.exclude, word)
		} else {
			q.include = append(q.include, word)
		}
	}
}

// Empty tells if the query has no words: the navigator shows the nodes as without a search
func (q nodeQuery) Empty() bool {
	return len(q.include) == 0 && len(q.exclude) == 0
}

// Matches tells if a search text (see nodeSearchText) has all the words of the query, and none of the excluded ones
func (q nodeQuery) Matches(text string) bool {
	for _, w := range q.include {
		if !strings.Contains(text, w) {
			return false
		}
	}
	for _, w := range q.exclude {
		if strings.Contains(text, w) {
			return false
		}
	}
	return true
}

// nodeSearchText is what the search looks in for a node, in lower case: its title (or command line, as its row shows
// it), id, command line, working directory, runner, environment variables (NAME=value), description, the resources
// that it provides and requires, and its locks. The fields are on separate lines, so a phrase does not match across
// them. It must be called with the tree locked; it does not change after the tree is loaded.
func nodeSearchText(n *rt.Node) string {
	c := n.Calculated
	fields := []string{n.DisplayLabel(), n.Id, strings.Join(c.Args, " "), c.CWD, c.Runner, c.Description}
	for _, name := range slices.Sorted(maps.Keys(c.Envs)) {
		fields = append(fields, name+"="+c.Envs[name])
	}
	for _, s := range []*set.Set[string]{c.Provides, c.Requires, c.XLocks, c.RLocks} {
		if s != nil {
			fields = append(fields, s.List()...)
		}
	}
	return strings.ToLower(strings.Join(fields, "\n"))
}

// searchTree returns the nodes that the tree view shows for a search: the nodes that match (true), and their parents
// that do not match (false), so that the matching nodes are shown where they are in the tree
func searchTree(nodes []*rt.Node, matches func(n *rt.Node) bool) map[*rt.Node]bool {
	shown := make(map[*rt.Node]bool)
	for _, n := range nodes {
		if !matches(n) {
			continue
		}
		shown[n] = true
		for p := n.Parent; p != nil; p = p.Parent {
			if _, ok := shown[p]; ok {
				break // its parents are shown already
			}
			shown[p] = false
		}
	}
	return shown
}
