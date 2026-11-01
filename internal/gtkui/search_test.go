package gtkui

import (
	"reflect"
	"testing"

	"github.com/nagylzs/runtree/internal/rt"
	"github.com/nagylzs/set"
)

func TestParseNodeQuery(t *testing.T) {
	for _, tc := range []struct {
		text             string
		include, exclude []string
	}{
		{"", nil, nil},
		{"  ", nil, nil},
		{"Deploy", []string{"deploy"}, nil},
		{"+deploy -UAT clone", []string{"deploy", "clone"}, []string{"uat"}},
		{`"pg clone" -"create db"`, []string{"pg clone"}, []string{"create db"}},
		{"deploy -", []string{"deploy"}, nil}, // the excluded word is being typed
		{`--force +-v`, []string{"-v"}, []string{"-force"}},
		{`"wait to calm`, []string{"wait to calm"}, nil}, // an unclosed phrase
		{`""  + -`, nil, nil},
	} {
		q := parseNodeQuery(tc.text)
		if !reflect.DeepEqual(q.include, tc.include) || !reflect.DeepEqual(q.exclude, tc.exclude) {
			t.Errorf("%q: include %q, exclude %q; want %q, %q", tc.text, q.include, q.exclude, tc.include, tc.exclude)
		}
		if q.Empty() != (tc.include == nil && tc.exclude == nil) {
			t.Errorf("%q: Empty() is %v", tc.text, q.Empty())
		}
	}
}

// The search looks in the title, the command line, the working directory, the environment variables, the locks... of
// a node
func TestNodeSearch(t *testing.T) {
	node := &rt.Node{Id: "clone-shop", Type: rt.TypeRun, Calculated: rt.Calculated{
		Title:       "shop-dev create pg clone",
		Args:        []string{"ssh", "-t", "root@devhost2", "/root/scripts/clone"},
		CWD:         "/srv/deploy",
		Runner:      "ssh://deployer@jump",
		Description: "Clones the production database.",
		Envs:        map[string]string{"PGHOST": "db1", "STAGE": "dev"},
		Provides:    set.FromArray([]string{"dev-db"}),
		XLocks:      set.FromArray([]string{"pg-cluster"}),
	}}
	text := nodeSearchText(node)
	for query, want := range map[string]bool{
		"SHOP clone":           true,  // the title
		"shop -clone":          false, // an excluded word
		"shop -uat":            true,
		"clone-shop":           true, // the id
		"devhost2":             true, // the command line
		`"-t root@"`:           true,
		"/srv/deploy":          true, // the working directory
		"deployer@jump":        true, // the runner
		"production":           true, // the description
		"pghost=db1":           true, // the environment variables
		"stage=dev -stage=uat": true,
		"dev-db":               true,  // provides
		"pg-cluster":           true,  // a lock
		`"clone clone-shop"`:   false, // a phrase does not match across the fields
		"uat":                  false,
	} {
		if got := parseNodeQuery(query).Matches(text); got != want {
			t.Errorf("%q: %v, want %v", query, got, want)
		}
	}
	// a node without a title is found by its command line, as its row shows it
	untitled := &rt.Node{Id: "n1", Calculated: rt.Calculated{Args: []string{"make", "test"}}}
	if text := nodeSearchText(untitled); !parseNodeQuery(`"make test"`).Matches(text) {
		t.Errorf("a node without a title: %q", text)
	}
}

// The tree shows the nodes that the search found where they are: with their parents
func TestSearchTree(t *testing.T) {
	root := &rt.Node{Id: "root"}
	hosts := &rt.Node{Id: "hosts", Parent: root}
	web1 := &rt.Node{Id: "web1", Parent: hosts}
	web2 := &rt.Node{Id: "web2", Parent: hosts}
	web2check := &rt.Node{Id: "web2-check", Parent: web2}
	post := &rt.Node{Id: "post", Parent: root}
	nodes := []*rt.Node{root, hosts, web1, web2, web2check, post}
	search := func(ids ...string) map[string]bool {
		shown := searchTree(nodes, func(n *rt.Node) bool {
			for _, id := range ids {
				if n.Id == id {
					return true
				}
			}
			return false
		})
		res := make(map[string]bool)
		for n, matched := range shown {
			res[n.Id] = matched
		}
		return res
	}
	for _, tc := range []struct {
		found []string
		want  map[string]bool // true: found, false: a parent of a node found
	}{
		{nil, map[string]bool{}},
		{[]string{"web1"}, map[string]bool{"root": false, "hosts": false, "web1": true}},
		{[]string{"web2", "web2-check"}, map[string]bool{"root": false, "hosts": false, "web2": true, "web2-check": true}},
		{[]string{"web2-check", "post"}, map[string]bool{"root": false, "hosts": false, "web2": false,
			"web2-check": true, "post": true}},
		{[]string{"root"}, map[string]bool{"root": true}},
	} {
		if got := search(tc.found...); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("found %v: shown %v, want %v", tc.found, got, tc.want)
		}
	}
}

func TestFoundText(t *testing.T) {
	for n, want := range map[int]string{0: "Not found", 1: "1 node found", 12: "12 nodes found"} {
		if got := foundText(n); got != want {
			t.Errorf("foundText(%d) = %q, want %q", n, got, want)
		}
	}
}
