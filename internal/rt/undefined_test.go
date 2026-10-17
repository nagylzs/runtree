package rt

import (
	"path/filepath"
	"strings"
	"testing"
)

// loadWithWarnings loads a tree file (it must load), and returns its warnings
func loadWithWarnings(t *testing.T, files map[string]string) (*Tree, ParseErrors) {
	t.Helper()
	dir := writeFiles(t, files)
	sources := NewSources()
	_, filename, err := AddTree(filepath.Join(dir, "tree.yaml"), sources)
	if err != nil {
		t.Fatal(err)
	}
	tree, err := ParseToDom(sources, filename, 30)
	if err != nil {
		t.Fatal(err)
	}
	return tree, sources.Warnings()
}

type wantWarning struct {
	file      string
	line, col int
	msg       string
}

func checkWarnings(t *testing.T, got ParseErrors, want []wantWarning) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%d warnings, want %d:\n%v", len(got), len(want), got)
	}
	for i, w := range want {
		checkPos(t, got[i].Pos, w.file, w.line, w.col)
		checkContains(t, got[i].Error(), "warning: "+w.msg)
		if !got[i].Warning {
			t.Errorf("%v is not a warning", got[i])
		}
	}
}

// References to undefined variables are reported as warnings, at the position of the value (or item).
func TestUndefinedVariables(t *testing.T) {
	_, warnings := loadWithWarnings(t, map[string]string{"tree.yaml": `tree:
  title: deploy {env}
  vars:
    host: srv
    url: http://{host}:{port}
  for_vars:
    server: ["a", "{prefix}b"]
  par:
    - title: job
      description: runs {what}
      cwd: /srv/{dir}
      runner: "{runner_host}:5000"
      envs:
        A: "{host}"
        B: "{b_value}"
      args_prefix: [ssh, "{host}"]
      args: [echo, "{server}", "{missing}"]
      xlocks: ["{lock}"]
      provides: ["{res}"]
      requires: ["{req}"]
    - title: not variables
      args: [bash, -c, 'find . -exec echo {} ; ; echo ${HOME} {a,b} {{.Name}} { x }']
`})
	checkWarnings(t, warnings, []wantWarning{
		{"tree.yaml", 2, 10, "undefined variable {env}"},
		{"tree.yaml", 5, 10, "undefined variable {port}"},
		{"tree.yaml", 7, 19, "undefined variable {prefix}"},
		{"tree.yaml", 10, 20, "undefined variable {what}"},
		{"tree.yaml", 11, 12, "undefined variable {dir}"},
		{"tree.yaml", 12, 15, "undefined variable {runner_host}"},
		{"tree.yaml", 15, 12, "undefined variable {b_value}"},
		{"tree.yaml", 17, 32, "undefined variable {missing}"},
		{"tree.yaml", 18, 16, "undefined variable {lock}"},
		{"tree.yaml", 19, 18, "undefined variable {res}"},
		{"tree.yaml", 20, 18, "undefined variable {req}"},
	})
	// the same reference in the for_vars repeated nodes is reported once
	if warnings[3].Count != 2 {
		t.Errorf("found %d times, want 2", warnings[3].Count)
	}
}

// The warnings for included objects tell that the variable is not set where the object is included, and they have
// the include chain.
func TestUndefinedVariableInInclude(t *testing.T) {
	_, warnings := loadWithWarnings(t, map[string]string{
		"tree.yaml": `tree:
  seq:
    - vars:
        user: root
      include: lib.yaml|job
    - include: lib.yaml|job
`,
		"lib.yaml": `job:
  args: [ssh, "{user}@host"]
`})
	checkWarnings(t, warnings, []wantWarning{
		{"lib.yaml", 2, 15, "undefined variable {user}, it is not set where it is included"},
	})
	if inc := warnings[0].Included; len(inc) != 1 || inc[0].Pos.Line != 6 {
		t.Errorf("include chain is %v, want the second include", inc)
	}
}

func TestUndefinedVariableHints(t *testing.T) {
	_, warnings := loadWithWarnings(t, map[string]string{"tree.yaml": `tree:
  vars:
    target: build
  seq:
    - inherit_vars: false
      args: [make, "{target}"]
    - vars:
        a: x
      def_vars:
        b: "{a}"
      args: [echo, "{b}"]
`})
	checkWarnings(t, warnings, []wantWarning{
		{"tree.yaml", 6, 20, "undefined variable {target} (it is not inherited, because inherit_vars is false)"},
		{"tree.yaml", 10, 12, "undefined variable {a} (def_vars cannot use the vars of the same node)"},
	})
}

// Only the ifeq and ifneq references are reported for excluded nodes.
func TestUndefinedVariableInExcludedNode(t *testing.T) {
	tree, warnings := loadWithWarnings(t, map[string]string{"tree.yaml": `tree:
  vars:
    target: dev
  seq:
    - ifeq:
        target: "{prod_name}"
      args: [echo, "{prod_host}"]
    - args: ["true"]
`})
	checkWarnings(t, warnings, []wantWarning{
		{"tree.yaml", 6, 17, "undefined variable {prod_name}"},
	})
	if len(tree.Root.Nodes) != 1 {
		t.Errorf("%d sub-nodes, want 1", len(tree.Root.Nodes))
	}
}

// Substituted values are not evaluated again (it would depend on the order of the variables).
func TestSubstitutionSinglePass(t *testing.T) {
	for i := 0; i < 20; i++ {
		tree, _ := loadWithWarnings(t, map[string]string{"tree.yaml": `tree:
  vars:
    a: "{b}"
    c: "{{d}}"
  seq:
    - vars:
        b: x
        d: y
      args: [echo, "{a}", "{c}", "{b}{d}", "{{d}}", "{d"]
`})
		got := strings.Join(tree.Root.Nodes[0].Calculated.Args, " ")
		if want := "echo {b} {{d}} xy {y} {d"; got != want {
			t.Fatalf("args are %q, want %q", got, want)
		}
	}
}

// A string tagged !raw is used as it is: no substitution, and no warnings.
func TestRawTag(t *testing.T) {
	tree, warnings := loadWithWarnings(t, map[string]string{"tree.yaml": `tree:
  vars:
    name: user
    filter: !raw "{name}"
    hosts: [a, !raw "{b}"]
  for_vars:
    host: hosts
  seq:
    - title: !raw "jq {name}"
      envs:
        F: !raw "{x}"
      args: [bash, -c, !raw "jq '{name}' x.json", "{filter}-{name}", "{host}"]
`})
	checkWarnings(t, warnings, nil)
	var got []string
	for _, n := range tree.Root.Nodes {
		got = append(got, n.Calculated.Title+"|"+n.Calculated.Envs["F"]+"|"+strings.Join(n.Calculated.Args, "|"))
	}
	want := []string{
		"jq {name}|{x}|bash|-c|jq '{name}' x.json|{name}-user|a",
		"jq {name}|{x}|bash|-c|jq '{name}' x.json|{name}-user|{b}",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("got\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

func TestRawTagErrors(t *testing.T) {
	for _, tc := range []struct {
		source    string
		line, col int
		msg       string
	}{
		{"tree:\n  args: !raw [echo]\n", 2, 9, "!raw can only be used for a string"},
		{"tree:\n  args: [echo, !rwa x]\n", 2, 16, "unknown tag !rwa (only !raw is supported)"},
		{"tree:\n  vars:\n    !raw a: b\n  args: [echo]\n", 3, 5, "!raw can only be used for values"},
	} {
		dir := writeFiles(t, map[string]string{"tree.yaml": tc.source})
		pe := parseError(t, filepath.Join(dir, "tree.yaml"))
		checkPos(t, pe.Pos, "tree.yaml", tc.line, tc.col)
		checkContains(t, pe.Message, tc.msg)
	}
}
