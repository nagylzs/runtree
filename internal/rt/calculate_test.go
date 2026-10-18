package rt

import (
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/nagylzs/runtree/internal/runner"
)

func TestProvidesNotInherited(t *testing.T) {
	tree := loadTestTree(t, `
tree:
  id: root
  provides: ["all-done"]
  seq:
    - type: run
      id: child
      args: ["true"]
    - type: run
      id: other
      provides: ["other-done"]
      args: ["true"]
`)
	for id, want := range map[string][]string{
		"root":  {"all-done"},
		"child": {},
		"other": {"other-done"},
	} {
		got := tree.NodeById[id].Calculated.Provides.List()
		if !slices.Equal(got, want) {
			t.Errorf("%s provides %v, want %v", id, got, want)
		}
	}
}

func TestRunner(t *testing.T) {
	tree := loadTestTree(t, `
tree:
  id: root
  vars:
    host: "10.0.0.1"
  seq:
    - type: run
      id: local
      args: ["true"]
    - type: seq
      id: remote
      runner: "{host}:5001"
      nodes:
        - type: seq
          id: middle
          nodes:
            - type: run
              id: deep
              args: ["true"]
        - type: run
          id: own
          runner: "10.0.0.2:5002"
          args: ["true"]
`)
	for id, want := range map[string]string{
		"root":   runner.LocalAddress,
		"local":  runner.LocalAddress,
		"remote": "10.0.0.1:5001",
		"middle": "10.0.0.1:5001",
		"deep":   "10.0.0.1:5001",
		"own":    "10.0.0.2:5002",
	} {
		if got := tree.NodeById[id].Calculated.Runner; got != want {
			t.Errorf("%s runner is %q, want %q", id, got, want)
		}
	}
}

// The default runner (runtree --runner) is used by the nodes that do not specify a runner, not even their parents.
// An explicit local runner is local.
func TestDefaultRunner(t *testing.T) {
	dir := writeFiles(t, map[string]string{"tree.yaml": `
tree:
  id: root
  seq:
    - id: plain
      args: ["true"]
    - id: empty
      runner: ""
      args: ["true"]
    - id: here
      runner: local
      seq:
        - id: inherited
          args: ["true"]
    - id: own
      runner: "10.0.0.2:5002"
      args: ["true"]
`})
	sources := NewSources()
	sources.DefaultRunner = "tcp://10.0.0.9:5000"
	_, filename, err := AddTree(filepath.Join(dir, "tree.yaml"), sources)
	if err != nil {
		t.Fatal(err)
	}
	tree, err := ParseToDom(sources, filename, 30)
	if err != nil {
		t.Fatal(err)
	}
	for id, want := range map[string]string{
		"root":      "tcp://10.0.0.9:5000",
		"plain":     "tcp://10.0.0.9:5000",
		"empty":     "tcp://10.0.0.9:5000",
		"here":      runner.LocalAddress,
		"inherited": runner.LocalAddress,
		"own":       "10.0.0.2:5002",
	} {
		if got := tree.NodeById[id].Calculated.Runner; got != want {
			t.Errorf("%s runner is %q, want %q", id, got, want)
		}
	}
}

func TestCWD(t *testing.T) {
	tree := loadTestTree(t, `
tree:
  id: root
  seq:
    - type: run
      id: nocwd
      args: ["true"]
    - type: seq
      id: project
      vars:
        app: "billing"
      cwd: "/srv/{app}"
      nodes:
        - type: seq
          id: middle
          nodes:
            - type: run
              id: deep
              args: ["true"]
        - type: run
          id: own
          cwd: "/tmp/{app}"
          args: ["true"]
`)
	for id, want := range map[string]string{
		"root":    "", // the current directory of the runner
		"nocwd":   "",
		"project": "/srv/billing",
		"middle":  "/srv/billing",
		"deep":    "/srv/billing",
		"own":     "/tmp/billing",
	} {
		if got := tree.NodeById[id].Calculated.CWD; got != want {
			t.Errorf("%s cwd is %q, want %q", id, got, want)
		}
	}
}

// The for_vars variables are like inherited ones: they are set for sub-nodes that have their own vars too, the vars
// of the sub-node override them, and def_vars are not used for them.
func TestForVarsWithSubNodeVars(t *testing.T) {
	tree := loadTestTree(t, `tree:
  vars:
    env: prod
  for_vars:
    server: ["{env}-01", "{env}-02"]
  seq:
    - include: job
    - title: own vars
      vars:
        port: "22"
      args: [ssh, "{server}:{port}"]
    - title: override
      vars:
        server: fixed
      args: [ssh, "{server}"]

job:
  title: job on {server}
  vars:
    user: root
  def_vars:
    server: default
  args: [ssh, "{user}@{server}"]
`)
	var got []string
	for _, n := range tree.Root.Nodes {
		got = append(got, strings.Join(n.Calculated.Args, " "))
	}
	want := []string{
		"ssh root@prod-01", "ssh prod-01:22", "ssh fixed",
		"ssh root@prod-02", "ssh prod-02:22", "ssh fixed",
	}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("args are\n%v\nwant\n%v", got, want)
	}
}

// def_vars are used when the variable is not set by for_vars
func TestDefVarsWithoutForVars(t *testing.T) {
	tree := loadTestTree(t, `tree:
  include: job

job:
  def_vars:
    server: default
  args: [ssh, "{server}"]
`)
	if got := strings.Join(tree.Root.Calculated.Args, " "); got != "ssh default" {
		t.Errorf("args are %q, want %q", got, "ssh default")
	}
}

// The values of a vars block use the variables of the same block (in any order, with their calculated values), and
// in its own value a variable means the inherited value.
func TestVarsSameBlock(t *testing.T) {
	tree := loadTestTree(t, `tree:
  vars:
    target: build
    path: /usr/bin
  seq:
    - vars:
        c: "{b}-z"
        command: "make {target}"
        target: test
        b: "{a}-y"
        a: x
        path: "{path}:/opt/bin"
        url: "http://{host}"
      def_vars:
        host: localhost
      ifeq:
        c: "x-y-z"
      args: [echo, "{command}", "{c}", "{path}", "{url}"]
`)
	if len(tree.Root.Nodes) != 1 {
		t.Fatalf("%d sub-nodes, want 1 (ifeq must use the calculated value)", len(tree.Root.Nodes))
	}
	got := strings.Join(tree.Root.Nodes[0].Calculated.Args, "|")
	if want := "echo|make test|x-y-z|/usr/bin:/opt/bin|http://localhost"; got != want {
		t.Errorf("args are %q, want %q", got, want)
	}
}

// A variable that refers to itself, and is not inherited, is undefined.
func TestVarsSelfReferenceNotInherited(t *testing.T) {
	_, warnings := loadWithWarnings(t, map[string]string{"tree.yaml": `tree:
  vars:
    path: "{path}:/opt/bin"
  args: [echo, "{path}"]
`})
	checkWarnings(t, warnings, []wantWarning{{"tree.yaml", 3, 11, "undefined variable {path}"}})
}

func TestVarsCycle(t *testing.T) {
	_, err := parseTestTree(t, `tree:
  vars:
    a: "{b}"
    b: "x{c}"
    c: "{a}"
  args: [echo, "{a}"]
`)
	var pe *ParseError
	if !errors.As(err, &pe) {
		t.Fatalf("a ParseError was expected, got %v", err)
	}
	checkPos(t, pe.Pos, "tree.yaml", 3, 8)
	checkContains(t, pe.Message, "the variables refer to each other: a → b → c → a")
}

// def_vars can use the inherited variables and the other defaults (with their final values), but not the vars of
// the same node.
func TestDefVarsSameBlock(t *testing.T) {
	tree := loadTestTree(t, `tree:
  seq:
    - include: job
    - vars:
        host: srv
      include: job

job:
  def_vars:
    url: "http://{host}:{port}"
    host: localhost
    port: "8080"
  args: [curl, "{url}"]
`)
	var got []string
	for _, n := range tree.Root.Nodes {
		got = append(got, strings.Join(n.Calculated.Args, " "))
	}
	if want := "curl http://localhost:8080|curl http://srv:8080"; strings.Join(got, "|") != want {
		t.Errorf("args are %q, want %q", strings.Join(got, "|"), want)
	}
}

func TestDefVarsCycle(t *testing.T) {
	_, err := parseTestTree(t, `tree:
  def_vars:
    a: "{b}"
    b: "{a}"
  args: [echo, "{a}"]
`)
	var pe *ParseError
	if !errors.As(err, &pe) {
		t.Fatalf("a ParseError was expected, got %v", err)
	}
	checkPos(t, pe.Pos, "tree.yaml", 3, 8)
	checkContains(t, pe.Message, "the variables refer to each other: a → b → a")
}

// The strings in list variables are calculated where the variable is defined.
func TestVarsListItems(t *testing.T) {
	tree := loadTestTree(t, `tree:
  vars:
    env: prod
    hosts: ["{env}-01", "{env}-02"]
  seq:
    - vars:
        env: dev
      for_vars:
        host: hosts
      seq:
        - args: [ssh, "{host}"]
`)
	var got []string
	for _, n := range tree.Root.Nodes[0].Nodes {
		got = append(got, strings.Join(n.Calculated.Args, " "))
	}
	if want := "ssh prod-01|ssh prod-02"; strings.Join(got, "|") != want {
		t.Errorf("args are %q, want %q", strings.Join(got, "|"), want)
	}
}
