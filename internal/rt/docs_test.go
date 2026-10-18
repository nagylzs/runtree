package rt

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"unicode"
)

var updateDocs = flag.Bool("update-docs", false, "rewrite docs/reference/properties.md")

const propertiesDoc = "../../docs/reference/properties.md"

// propertyGuide is the section of the user guide about each property (from docs/reference)
var propertyGuide = map[string]string{
	"type": "../tree-files.md#nodes", "id": "../tree-files.md#nodes", "title": "../tree-files.md#nodes",
	"description": "../tree-files.md#nodes", "args": "../tree-files.md#run-nodes",
	"args_prefix": "../variables.md#command-prefix", "cwd": "../variables.md#working-directory",
	"runner": "../runners.md", "vars": "../variables.md#variables", "def_vars": "../variables.md#default-variables",
	"inherit_vars": "../variables.md#inheritance", "envs": "../variables.md#environment",
	"system_envs": "../variables.md#environment", "inherit_envs": "../variables.md#environment",
	"xlocks": "../coordination.md#locks", "rlocks": "../coordination.md#locks",
	"provides": "../coordination.md#dependencies", "requires": "../coordination.md#dependencies",
	"max_proc": "../execution.md#limiting-processes", "success_exit_codes": "../execution.md#exit-codes",
	"status": "../execution.md#frozen-nodes", "on_error": "../execution.md#errors",
	"children_on_error": "../execution.md#errors", "expanded": "../gui.md#the-tree",
	"expand_on_active": "../gui.md#the-tree", "collapse_on_finished": "../gui.md#the-tree",
	"nodes": "../tree-files.md#nodes", "seq": "../tree-files.md#nodes", "par": "../tree-files.md#nodes",
	"include": "../tree-files.md#include", "for_vars": "../tree-files.md#for_vars",
	"ifeq": "../tree-files.md#conditions", "ifneq": "../tree-files.md#conditions",
}

// kindText describes the value of a property
func kindText(p Property) string {
	switch p.Kind {
	case KindString:
		if len(p.Enum) > 0 {
			quoted := make([]string, len(p.Enum))
			for i, e := range p.Enum {
				quoted[i] = "`" + e + "`"
			}
			return strings.Join(quoted[:len(quoted)-1], ", ") + " or " + quoted[len(quoted)-1]
		}
		return "string"
	case KindStringList:
		return "list of strings"
	case KindBool:
		return "boolean"
	case KindInt:
		return "integer"
	case KindExitCodes:
		return "list of integers (0-255)"
	case KindStringMap:
		return "object of strings"
	case KindVarMap:
		return "object of strings or lists of strings"
	case KindForVars:
		return "object of lists of strings (or list variable names)"
	case KindOnError:
		return "status and/or sibling operation"
	case KindNodeList:
		return "list of nodes"
	case KindInclude:
		return "string or list of strings"
	}
	return "?"
}

// propertiesMarkdown is docs/reference/properties.md, generated from NodeProperties
func propertiesMarkdown() string {
	var b strings.Builder
	b.WriteString(`# Node properties

<!-- Generated from NodeProperties (internal/rt/properties.go), do not edit it. Regenerate it with:
     go test ./internal/rt/ -run TestPropertiesDocIsUpToDate -update-docs -->

The properties of the nodes in tree files. The guide explains how they work together: [writing tree
files](../tree-files.md), [variables](../variables.md), [how a tree runs](../execution.md).

An **inherited** property that is not set in a node gets the value of the parent node. Variables (` + "`{name}`" +
		`) can be used in the strings of ` + "`title`, `description`, `args`, `args_prefix`, `cwd`, `envs`, `runner`, " +
		"`xlocks`, `rlocks`, `provides`, `requires`, `ifeq`, `ifneq`, `for_vars`, `vars` and `def_vars`" + `.

| Property | Value | Inherited | Default |
|----------|-------|:---------:|---------|
`)
	for _, p := range NodeProperties {
		inherited, def := "", ""
		if p.Inherited {
			inherited = "yes"
		}
		if p.Default != "" {
			def = "`" + p.Default + "`"
		}
		fmt.Fprintf(&b, "| [`%s`](#%s) | %s | %s | %s |\n", p.Name, p.Name, kindText(p), inherited, def)
	}
	for _, p := range NodeProperties {
		fmt.Fprintf(&b, "\n## %s\n\n%s\n", p.Name, p.Description)
		facts := []string{"Value: " + kindText(p)}
		if p.Inherited {
			facts = append(facts, "inherited")
		}
		if p.Default != "" {
			facts = append(facts, "default: `"+p.Default+"`")
		}
		fmt.Fprintf(&b, "\n%s.", strings.Join(facts, ", "))
		if guide, ok := propertyGuide[p.Name]; ok {
			fmt.Fprintf(&b, " See [the guide](%s).", guide)
		}
		b.WriteString("\n")
	}
	return b.String()
}

func TestPropertiesDocIsUpToDate(t *testing.T) {
	want := propertiesMarkdown()
	if *updateDocs {
		if err := os.WriteFile(propertiesDoc, []byte(want), 0644); err != nil {
			t.Fatal(err)
		}
		return
	}
	got, err := os.ReadFile(propertiesDoc)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != want {
		t.Errorf("%s is out of date, regenerate it with:\n"+
			"  go test ./internal/rt/ -run TestPropertiesDocIsUpToDate -update-docs", propertiesDoc)
	}
	for _, p := range NodeProperties {
		if _, ok := propertyGuide[p.Name]; !ok {
			t.Errorf("property %s has no section of the guide in propertyGuide", p.Name)
		}
	}
}

var (
	mdLink    = regexp.MustCompile(`\]\(([^)\s]+)\)`)
	mdHeading = regexp.MustCompile(`(?m)^#{1,6} (.+)$`)
	mdFence   = regexp.MustCompile("(?ms)^```.*?^```")
)

// anchors returns the anchors of the headings of a markdown file, like GitHub makes them
func anchors(content string) map[string]bool {
	res := make(map[string]bool)
	content = mdFence.ReplaceAllString(content, "")
	for _, m := range mdHeading.FindAllStringSubmatch(content, -1) {
		var b strings.Builder
		for _, r := range strings.ToLower(m[1]) {
			switch {
			case unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-' || r == '_':
				b.WriteRune(r)
			case r == ' ':
				b.WriteRune('-')
			}
		}
		a := b.String()
		for i := 1; res[a]; i++ {
			a = fmt.Sprintf("%s-%d", strings.TrimSuffix(a, fmt.Sprintf("-%d", i-1)), i)
		}
		res[a] = true
	}
	return res
}

// The relative links of the documentation point to existing files and headings
func TestDocLinks(t *testing.T) {
	files := []string{"../../README.md", "../../examples/README.md"}
	docs, err := filepath.Glob("../../docs/*.md")
	if err != nil {
		t.Fatal(err)
	}
	refs, _ := filepath.Glob("../../docs/reference/*.md")
	files = append(append(files, docs...), refs...)
	if len(docs) < 5 {
		t.Fatalf("the docs are not found: %v", docs)
	}
	contents := make(map[string]string)
	read := func(path string) (string, bool) {
		if c, ok := contents[path]; ok {
			return c, true
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return "", false
		}
		contents[path] = string(b)
		return string(b), true
	}
	for _, f := range files {
		content, ok := read(f)
		if !ok {
			t.Errorf("%s is missing", f)
			continue
		}
		for _, m := range mdLink.FindAllStringSubmatch(mdFence.ReplaceAllString(content, ""), -1) {
			link := m[1]
			if strings.Contains(link, "://") || strings.HasPrefix(link, "mailto:") {
				continue
			}
			target, anchor, _ := strings.Cut(link, "#")
			path := f
			if target != "" {
				path = filepath.Join(filepath.Dir(f), target)
			}
			targetContent, ok := read(path)
			if !ok {
				if _, err := os.Stat(path); err != nil {
					t.Errorf("%s: the link %s points to a missing file", f, link)
				}
				continue
			}
			if anchor != "" && strings.HasSuffix(path, ".md") && !anchors(targetContent)[anchor] {
				t.Errorf("%s: the link %s points to a missing heading", f, link)
			}
		}
	}
}
