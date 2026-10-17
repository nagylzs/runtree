package rt

import (
	"path/filepath"
	"slices"
	"testing"
)

const queryTree = `tree:
  title: root
  vars:
    user: root
  for_vars:
    env: [dev, prod]
  seq:
    - include: lib.yaml|job
    - title: local
      vars:
        port: "22"
      args: [ssh, "{user}@{env}:{port}"]
`

const queryLib = `job:
  title: job {env}
  def_vars:
    host: localhost
  args: [ssh, "{host}"]
`

func loadQueryTree(t *testing.T) (*Tree, *Sources, string) {
	t.Helper()
	dir := writeFiles(t, map[string]string{"tree.yaml": queryTree, "lib.yaml": queryLib})
	sources := NewSources()
	_, filename, err := AddTree(filepath.Join(dir, "tree.yaml"), sources)
	if err != nil {
		t.Fatal(err)
	}
	tree, err := LoadPartialTree(sources, filename, "tree", 30)
	if err != nil {
		t.Fatal(err)
	}
	return tree, sources, dir
}

func titles(nodes []*Node) []string {
	var res []string
	for _, n := range nodes {
		res = append(res, n.Calculated.Title)
	}
	slices.Sort(res)
	return res
}

func TestNodesAt(t *testing.T) {
	tree, sources, dir := loadQueryTree(t)
	for _, tc := range []struct {
		file string
		line int
		want []string
	}{
		{"tree.yaml", 2, []string{"root"}},
		{"tree.yaml", 12, []string{"local", "local"}}, // for every for_vars combination
		{"tree.yaml", 8, []string{"job dev", "job prod"}},
		{"lib.yaml", 5, []string{"job dev", "job prod"}},
		{"lib.yaml", 7, nil},
	} {
		got := titles(sources.NodesAt(tree, filepath.Join(dir, tc.file), tc.line))
		if !slices.Equal(got, tc.want) {
			t.Errorf("%s:%d: nodes are %v, want %v", tc.file, tc.line, got, tc.want)
		}
	}
}

func TestVarSources(t *testing.T) {
	tree, sources, dir := loadQueryTree(t)
	local := sources.NodesAt(tree, filepath.Join(dir, "tree.yaml"), 12)[0]
	job := sources.NodesAt(tree, filepath.Join(dir, "lib.yaml"), 5)[0]
	for _, tc := range []struct {
		n          *Node
		name, prop string
		node       *Node
		file       string
		line, col  int
	}{
		{local, "port", "vars", local, "tree.yaml", 11, 9},
		{local, "user", "vars", tree.Root, "tree.yaml", 4, 5},
		{local, "env", "for_vars", tree.Root, "tree.yaml", 6, 5},
		{job, "host", "def_vars", job, "lib.yaml", 4, 5},
	} {
		src, ok := tc.n.Calculated.VarSources[tc.name]
		if !ok || src.Prop != tc.prop || src.Node != tc.node {
			t.Errorf("%s: source is %+v, want %s of %s", tc.name, src, tc.prop, tc.node.Calculated.Title)
			continue
		}
		pos, ok := sources.VarPos(src, tc.name)
		if !ok {
			t.Errorf("%s: no position", tc.name)
			continue
		}
		checkPos(t, pos, tc.file, tc.line, tc.col)
	}
}

// The parts of a tree that can be loaded are returned with the errors.
func TestLoadPartialTree(t *testing.T) {
	dir := writeFiles(t, map[string]string{"tree.yaml": "tree:\n  seq:\n    - args: [\"true\"]\n    - bogus: 1\n"})
	sources := NewSources()
	_, filename, err := AddTree(filepath.Join(dir, "tree.yaml"), sources)
	if err != nil {
		t.Fatal(err)
	}
	tree, err := LoadPartialTree(sources, filename, "tree", 30)
	if err == nil || tree == nil || len(tree.Nodes) == 0 {
		t.Fatalf("tree %v, error %v: want the partial tree and the error", tree, err)
	}
	for _, n := range tree.Nodes {
		if n == nil {
			t.Fatal("nil node")
		}
	}
}
