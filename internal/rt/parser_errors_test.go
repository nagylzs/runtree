package rt

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeFiles writes the files (name -> content) into a new temporary directory, and returns the directory.
func writeFiles(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// loadFile loads a tree file like runtree does.
func loadFile(path string) (*Tree, error) {
	sources := NewSources()
	_, filename, err := AddTree(path, sources)
	if err != nil {
		return nil, err
	}
	return ParseToDom(sources, filename, 30)
}

// parseError loads the tree file, and returns its ParseError (the test fails if there is none).
func parseError(t *testing.T, path string) *ParseError {
	t.Helper()
	_, err := loadFile(path)
	var pe *ParseError
	if !errors.As(err, &pe) {
		t.Fatalf("a ParseError was expected, got %v", err)
	}
	return pe
}

func checkPos(t *testing.T, got Pos, file string, line, column int) {
	t.Helper()
	if filepath.Base(got.File) != file || got.Line != line || got.Column != column {
		t.Errorf("position is %s:%d:%d, want %s:%d:%d", filepath.Base(got.File), got.Line, got.Column,
			file, line, column)
	}
}

func checkContains(t *testing.T, s, want string) {
	t.Helper()
	if !strings.Contains(s, want) {
		t.Errorf("%q does not contain %q", s, want)
	}
}

func TestErrorInIncludedFile(t *testing.T) {
	dir := writeFiles(t, map[string]string{
		"main.yaml": `tree:
  id: root
  title: "upgrade all"
  seq:
    - include: ["servers.yaml|upgrade"]
`,
		"servers.yaml": `upgrade:
  title: "upgrade servers"
  seq:
    - title: "build"
      args: ["make"]
    - title: "deploy"
      on_eror: paused
      args: ["make", "deploy"]
`,
	})
	pe := parseError(t, filepath.Join(dir, "main.yaml"))
	checkPos(t, pe.Pos, "servers.yaml", 7, 7)
	checkContains(t, pe.Message, `unknown property "on_eror" (did you mean "on_error"?)`)
	if pe.Node != "upgrade all › upgrade servers › deploy" {
		t.Errorf("node path: %q", pe.Node)
	}
	if len(pe.Included) != 1 || pe.Included[0].Name != "servers.yaml|upgrade" {
		t.Fatalf("include chain: %+v", pe.Included)
	}
	checkPos(t, pe.Included[0].Pos, "main.yaml", 5, 17)
}

func TestNestedIncludeChain(t *testing.T) {
	dir := writeFiles(t, map[string]string{
		"main.yaml": "tree:\n  seq:\n    - include: \"a.yaml|a\"\n",
		"a.yaml":    "a:\n  title: a\n  include: \"b.yaml|b\"\n",
		"b.yaml":    "b:\n  title: b\n  max_proc: lots\n  args: [\"true\"]\n",
	})
	pe := parseError(t, filepath.Join(dir, "main.yaml"))
	checkPos(t, pe.Pos, "b.yaml", 3, 13)
	if len(pe.Included) != 2 {
		t.Fatalf("include chain: %+v", pe.Included)
	}
	// the innermost first
	checkPos(t, pe.Included[0].Pos, "a.yaml", 3, 12)
	checkPos(t, pe.Included[1].Pos, "main.yaml", 3, 16)
}

func TestPropertyValueErrors(t *testing.T) {
	for _, tc := range []struct {
		property, value, message string
		offset                   int // of the wrong item in the value
	}{
		{"max_proc", `"many"`, "invalid max_proc: it is a string, expected an integer", 0},
		{"status", "running", `invalid status: "running"`, 0},
		{"on_error", "pausd", `invalid on_error: "pausd" (expected a status`, 0},
		{"success_exit_codes", "[0, 300]", "exit codes are integers between 0 and 255", 4},
		{"type", "loop", `invalid type: "loop"`, 0},
		{"runner", `"udp://host:5000"`, `unknown runner "udp://host:5000": the runner is local, host:port,`, 0},
		{"runner", `"ssh://root:secret@host"`, `invalid runner "ssh://root:secret@host": passwords are not`, 0},
		{"runner", "server01", `invalid runner "server01"`, 0},
	} {
		dir := writeFiles(t, map[string]string{"tree.yaml": "tree:\n  seq:\n    - title: job\n      " +
			tc.property + ": " + tc.value + "\n      args: [\"true\"]\n"})
		pe := parseError(t, filepath.Join(dir, "tree.yaml"))
		checkPos(t, pe.Pos, "tree.yaml", 4, 9+len(tc.property)+tc.offset)
		checkContains(t, pe.Message, tc.message)
		checkContains(t, pe.Node, "job")
	}
}

func TestIncludeErrors(t *testing.T) {
	dir := writeFiles(t, map[string]string{
		"main.yaml":  "tree:\n  seq:\n    - include: [\"other.yaml|nothere\"]\n    - include: [\"missing.yaml|x\"]\n",
		"other.yaml": "something:\n  args: [\"true\"]\n",
	})
	pe := parseError(t, filepath.Join(dir, "main.yaml"))
	checkPos(t, pe.Pos, "main.yaml", 3, 17) // the include in the including file
	checkContains(t, pe.Message, `"nothere" is not found`)

	dir = writeFiles(t, map[string]string{"main.yaml": "tree:\n  seq:\n    - include: [\"missing.yaml|x\"]\n"})
	pe = parseError(t, filepath.Join(dir, "main.yaml"))
	checkPos(t, pe.Pos, "main.yaml", 3, 17)
	checkContains(t, pe.Message, "missing.yaml: file not found")
}

func TestDuplicateKey(t *testing.T) {
	dir := writeFiles(t, map[string]string{"tree.yaml": "tree:\n  title: a\n  title: b\n  args: [\"true\"]\n"})
	pe := parseError(t, filepath.Join(dir, "tree.yaml"))
	checkPos(t, pe.Pos, "tree.yaml", 3, 3)
	checkContains(t, pe.Message, `duplicate key "title" (already defined at line 2)`)
}

// yaml.v3 reports the lines of parser errors 0-based, see yamlParserProblems
func TestYAMLSyntaxErrorLines(t *testing.T) {
	for _, tc := range []struct {
		source  string
		line    int
		message string
	}{
		// parser error: the list started in line 3 is not closed
		{"tree:\n  seq:\n    - args: [\"make\"\n", 3, "did not find expected ',' or ']'"},
		// scanner error: a tab character in line 2
		{"tree:\n\tseq: []\n", 2, "found character that cannot start any token"},
	} {
		dir := writeFiles(t, map[string]string{"tree.yaml": tc.source})
		pe := parseError(t, filepath.Join(dir, "tree.yaml"))
		if pe.Pos.Line != tc.line {
			t.Errorf("%q: line %d, want %d (%s)", tc.message, pe.Pos.Line, tc.line, pe.Message)
		}
		checkContains(t, pe.Message, tc.message)
	}
}

func TestAnchorsAndMergeKeys(t *testing.T) {
	dir := writeFiles(t, map[string]string{"tree.yaml": `defaults: &defaults
  envs:
    LANG: C
  args_prefix: ["ssh", "root@server"]

tree:
  seq:
    - <<: *defaults
      id: first
      args: ["uptime"]
    - <<: *defaults
      id: second
      envs:
        LANG: hu_HU.UTF-8
      args: ["df"]
`})
	tree, err := loadFile(filepath.Join(dir, "tree.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(tree.NodeById["first"].Calculated.Args, " "); got != "ssh root@server uptime" {
		t.Errorf("first args: %q", got)
	}
	if got := tree.NodeById["second"].Calculated.Envs["LANG"]; got != "hu_HU.UTF-8" {
		t.Errorf("explicit keys must override merged ones, LANG=%q", got)
	}
}

// YAML 1.2 (yaml.v3) does not convert yes/no/on/off to booleans, but they still work for boolean properties.
func TestYAML11Booleans(t *testing.T) {
	dir := writeFiles(t, map[string]string{"tree.yaml": `tree:
  seq:
    - id: a
      expanded: yes
      args: ["true"]
    - id: b
      expanded: on
      inherit_envs: no
      args: ["true"]
    - id: c
      args: [echo, yes, on]
`})
	tree, err := loadFile(filepath.Join(dir, "tree.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !tree.NodeById["a"].Expanded || !tree.NodeById["b"].Expanded || tree.NodeById["b"].InheritEnvs {
		t.Errorf("boolean properties are not parsed correctly")
	}
	// they are strings in args (with yaml.v2 they were booleans, and the tree could not be loaded)
	if got := strings.Join(tree.NodeById["c"].Calculated.Args, " "); got != "echo yes on" {
		t.Errorf("args: %q", got)
	}
}

// A run node given as a list of arguments is reported at the position of the list.
func TestShorthandRunNodeError(t *testing.T) {
	dir := writeFiles(t, map[string]string{"tree.yaml": "tree:\n  seq:\n    - [\"true\"]\n    - [\"echo\", {a: b}]\n"})
	pe := parseError(t, filepath.Join(dir, "tree.yaml"))
	// at the wrong item
	checkPos(t, pe.Pos, "tree.yaml", 4, 16)
	checkContains(t, pe.Message, "item 2 is an object")
}

// The example trees in test/ (hand-run examples) must load.
func TestExampleTrees(t *testing.T) {
	files, err := filepath.Glob("../../test/*.rt.yaml")
	if err != nil || len(files) == 0 {
		t.Fatalf("no example trees found: %v", err)
	}
	nodes := map[string]int{"test01.rt.yaml": 12, "test04.rt.yaml": 7, "test05.rt.yaml": 7, "test06.rt.yaml": 8,
		"test07.rt.yaml": 13, "test08.rt.yaml": 17, "test09.rt.yaml": 7}
	for _, f := range files {
		abs, _ := filepath.Abs(f)
		name := filepath.Base(f)
		tree, err := loadFile(abs)
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		if want, ok := nodes[name]; ok && len(tree.Nodes) != want {
			t.Errorf("%s: %d nodes, want %d", name, len(tree.Nodes), want)
		}
	}
}

// The README example of the shortest form
func TestShortestForm(t *testing.T) {
	dir := writeFiles(t, map[string]string{"tree.yaml": `tree:
  on_error: "freeze"
  seq: [ [ "make", "build" ], [ "make", "deploy" ] ]
`})
	tree, err := loadFile(filepath.Join(dir, "tree.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if len(tree.Root.Nodes) != 2 || strings.Join(tree.Root.Nodes[1].Calculated.Args, " ") != "make deploy" {
		t.Errorf("unexpected nodes")
	}
}

// "run" is not a node property (the node type is given with "type: run")
func TestRunIsNotAProperty(t *testing.T) {
	dir := writeFiles(t, map[string]string{"tree.yaml": `tree:
  run: ["make"]
  args: ["true"]
`})
	pe := parseError(t, filepath.Join(dir, "tree.yaml"))
	checkPos(t, pe.Pos, "tree.yaml", 2, 3)
	checkContains(t, pe.Message, `unknown property "run"`)
}

func TestSuggestProperty(t *testing.T) {
	for name, want := range map[string]string{
		"on_eror": "on_error", "provide": "provides", "titel": "title", "xyz": "", "arguments": "",
	} {
		if got := suggestProperty(name); got != want {
			t.Errorf("suggestProperty(%q) = %q, want %q", name, got, want)
		}
	}
}

// The for_vars example of the README: the nodes are generated in the order of the variables in the source (the first
// one changes the slowest), it must not depend on map iteration order.
func TestForVarsOrder(t *testing.T) {
	dir := writeFiles(t, map[string]string{"tree.yaml": `tree:
  vars:
    targets: ["dev", "uat"]
  for_vars:
      server: ["server01", "server02", "server03"]
      target: targets
  par:
    - args: ["ssh", "root@{server}", "make", "build", "{target}"]
`})
	want := "server01/dev server01/uat server02/dev server02/uat server03/dev server03/uat"
	for i := 0; i < 20; i++ {
		tree, err := loadFile(filepath.Join(dir, "tree.yaml"))
		if err != nil {
			t.Fatal(err)
		}
		var got []string
		for _, n := range tree.Root.Nodes {
			args := n.Calculated.Args
			got = append(got, strings.TrimPrefix(args[1], "root@")+"/"+args[len(args)-1])
		}
		if strings.Join(got, " ") != want {
			t.Fatalf("parse %d: %s, want %s", i, strings.Join(got, " "), want)
		}
	}
}

// parseErrors loads the tree file, and returns all of its errors (the test fails if there are none).
func parseErrors(t *testing.T, path string) ParseErrors {
	t.Helper()
	_, err := loadFile(path)
	var pes ParseErrors
	if !errors.As(err, &pes) {
		t.Fatalf("ParseErrors were expected, got %v", err)
	}
	return pes
}

// All errors are reported, sorted by their positions, and the consequences of an error are not reported.
func TestAllErrorsReported(t *testing.T) {
	dir := writeFiles(t, map[string]string{"tree.yaml": `tree:
  title: root
  seq:
    - title: first
      args: [echo, hi]
      max_proc: many
      expandd: true
    - title: second
      type: bogus
      args: [echo]
    - title: third
      args: echo
    - title: fourth
    - title: dup
      id: same
      args: ["true"]
    - title: dup2
      id: same
      args: ["true"]
`})
	pes := parseErrors(t, filepath.Join(dir, "tree.yaml"))
	want := []struct {
		line, col int
		msg       string
	}{
		{6, 17, "max_proc"},
		{7, 7, `unknown property "expandd"`},
		{9, 13, `invalid type: "bogus"`},
		{12, 13, "it is a string, expected a list of strings"}, // and not "a run node needs args"
		{13, 7, "a run node needs args"},
		{18, 11, "duplicate node id: same"},
	}
	if len(pes) != len(want) {
		t.Fatalf("%d errors, want %d:\n%v", len(pes), len(want), pes)
	}
	for i, w := range want {
		checkPos(t, pes[i].Pos, "tree.yaml", w.line, w.col)
		checkContains(t, pes[i].Message, w.msg)
	}
	// the node is shown by its title
	if pes[5].Node != "root › dup2" {
		t.Errorf("node is %q, want %q", pes[5].Node, "root › dup2")
	}
}

// The same error is reported once, with the number of times it was found, and with the first include chain.
func TestRepeatedErrorReportedOnce(t *testing.T) {
	dir := writeFiles(t, map[string]string{
		"main.yaml": `tree:
  seq:
    - for_vars:
        x: [a, b, c]
      include: other.yaml|part
    - include: other.yaml|part
`,
		"other.yaml": `part:
  title: part {x}
  def_vars:
    x: z
  seq:
    - args: [echo, "{x}"]
      success_exit_codes: [300]
`})
	// the def_vars of an included node do not conflict with the for_vars variables
	pes := parseErrors(t, filepath.Join(dir, "main.yaml"))
	if len(pes) != 1 {
		t.Fatalf("%d errors, want 1:\n%v", len(pes), pes)
	}
	pe := pes[0]
	checkPos(t, pe.Pos, "other.yaml", 7, 28)
	if pe.Count != 4 {
		t.Errorf("found %d times, want 4", pe.Count)
	}
	checkContains(t, pe.Error(), "(found 4 times)")
	if len(pe.Included) != 1 {
		t.Fatalf("include chain is %v, want 1 include", pe.Included)
	}
	checkPos(t, pe.Included[0].Pos, "main.yaml", 5, 16)
	checkContains(t, pe.Node, "part a")
}

// Errors are reported from all included files, each with its own include chain.
func TestErrorsInSeveralIncludedFiles(t *testing.T) {
	dir := writeFiles(t, map[string]string{
		"main.yaml": `tree:
  seq:
    - include: [a.yaml|a, b.yaml|b, missing]
    - title: bad
      xlocks: [1]
`,
		"a.yaml": `a:
  include: b.yaml|b
`,
		"b.yaml": `b:
  args: [true]
  bogus: 1
`})
	pes := parseErrors(t, filepath.Join(dir, "main.yaml"))
	if len(pes) != 4 {
		t.Fatalf("%d errors, want 4:\n%v", len(pes), pes)
	}
	// main.yaml first (it has the first error), then b.yaml
	checkPos(t, pes[0].Pos, "main.yaml", 3, 37)
	checkContains(t, pes[0].Message, `"missing" is not found`)
	checkPos(t, pes[1].Pos, "main.yaml", 5, 16)
	checkContains(t, pes[1].Message, "item 1 is a number, expected a string (put it in quotes)")
	checkPos(t, pes[2].Pos, "b.yaml", 2, 10)
	checkContains(t, pes[2].Message, "item 1 is a boolean")
	if pes[2].Count != 2 {
		t.Errorf("found %d times, want 2", pes[2].Count)
	}
	// included from a.yaml, that is included from main.yaml (the first time it was found)
	if len(pes[2].Included) != 2 {
		t.Fatalf("include chain is %v, want 2 includes", pes[2].Included)
	}
	checkPos(t, pes[2].Included[0].Pos, "a.yaml", 2, 12)
	checkPos(t, pes[2].Included[1].Pos, "main.yaml", 3, 17)
	checkPos(t, pes[3].Pos, "b.yaml", 3, 3)
	checkContains(t, pes[3].Message, `unknown property "bogus"`)
}

// Generated ids look like "n12", and they must not conflict with the ids given in the tree.
func TestGeneratedIdDoesNotConflict(t *testing.T) {
	for _, id := range []string{"n1", "n2", "n3", "n10"} {
		tree := loadTestTree(t, "tree:\n  seq:\n    - args: [\"true\"]\n    - id: "+id+"\n      args: [\"true\"]\n")
		if n := tree.NodeById[id]; n == nil || n != tree.Root.Nodes[1] {
			t.Errorf("id %s is not the node that has it", id)
		}
		if len(tree.NodeById) != 3 {
			t.Errorf("%d nodes by id, want 3", len(tree.NodeById))
		}
	}
}

func TestInvalidId(t *testing.T) {
	_, err := parseTestTree(t, "tree:\n  seq:\n    - id: \"1 x\"\n      args: [\"true\"]\n")
	var pe *ParseError
	if !errors.As(err, &pe) {
		t.Fatalf("a ParseError was expected, got %v", err)
	}
	checkPos(t, pe.Pos, "tree.yaml", 3, 11)
	checkContains(t, pe.Message, `invalid id: "1 x" must match`)
}

// An empty for_vars list means that there are no combinations, so no sub-nodes.
func TestForVarsEmptyList(t *testing.T) {
	tree, warnings := loadWithWarnings(t, map[string]string{"tree.yaml": `tree:
  vars:
    servers: []
  for_vars:
    server: servers
  par:
    - args: [ssh, "{server}"]
`})
	if len(tree.Root.Nodes) != 0 {
		t.Errorf("%d sub-nodes, want 0", len(tree.Root.Nodes))
	}
	checkWarnings(t, warnings, []wantWarning{{"tree.yaml", 5, 13, "for_vars: server is an empty list"}})
}

// Substitution errors are at the value that has the variable.
func TestSubstitutionErrorPosition(t *testing.T) {
	for _, tc := range []struct {
		source    string
		line, col int
	}{
		{"tree:\n  vars:\n    l: [a]\n  args: [echo, \"{l}\"]\n", 4, 16},
		{"tree:\n  vars:\n    l: [a]\n  envs:\n    A: \"{l}\"\n  args: [echo]\n", 5, 8},
		{"tree:\n  vars:\n    l: [a]\n  title: \"{l}\"\n  args: [echo]\n", 4, 10},
		{"tree:\n  vars:\n    l: [a]\n  seq:\n    - vars:\n        x: \"{l}\"\n      args: [echo]\n", 6, 12},
	} {
		_, err := parseTestTree(t, tc.source)
		var pe *ParseError
		if !errors.As(err, &pe) {
			t.Fatalf("a ParseError was expected, got %v", err)
		}
		checkPos(t, pe.Pos, "tree.yaml", tc.line, tc.col)
		checkContains(t, pe.Message, "cannot substitute {l}: it is a list, not a string")
	}
}
