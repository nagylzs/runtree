package config

import (
	"os"
	"reflect"
	"strings"
	"testing"
)

// longOptions returns the long option names of an options struct (with the embedded structs)
func longOptions(t reflect.Type) []string {
	var res []string
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if f.Anonymous {
			res = append(res, longOptions(f.Type)...)
			continue
		}
		if name := f.Tag.Get("long"); name != "" {
			res = append(res, name)
		}
	}
	return res
}

// Every option of runtree and rtrunner is documented in docs/reference/cli.md
func TestOptionsDocumented(t *testing.T) {
	b, err := os.ReadFile("../../docs/reference/cli.md")
	if err != nil {
		t.Fatal(err)
	}
	doc := string(b)
	sections := map[string]reflect.Type{
		"## runtree\n":          reflect.TypeOf(CLIArgs{}),
		"## runtree-headless\n": reflect.TypeOf(HeadlessCLIArgs{}),
		"## rtrunner\n":         reflect.TypeOf(RtRunnerCLIArgs{}),
	}
	for heading, typ := range sections {
		start := strings.Index(doc, heading)
		if start < 0 {
			t.Fatalf("%q is not in cli.md", heading)
		}
		section := doc[start+len(heading):]
		if end := strings.Index(section, "\n## "); end >= 0 {
			section = section[:end]
		}
		options := longOptions(typ)
		if len(options) < 5 {
			t.Fatalf("%s has %d options", heading, len(options))
		}
		for _, name := range options {
			if !strings.Contains(section, "`--"+name) {
				t.Errorf("--%s is not documented under %s", name, strings.TrimSpace(heading))
			}
		}
	}
}
