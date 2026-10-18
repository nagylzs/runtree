package rt

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"testing"
)

var updateSchema = flag.Bool("update-schema", false, "rewrite schema/runtree.schema.json")

const schemaFile = "../../schema/runtree.schema.json"

// The published schema must be the one generated from NodeProperties.
func TestSchemaFileIsUpToDate(t *testing.T) {
	want, err := JSONSchema()
	if err != nil {
		t.Fatal(err)
	}
	if *updateSchema {
		if err := os.WriteFile(schemaFile, want, 0644); err != nil {
			t.Fatal(err)
		}
		return
	}
	got, err := os.ReadFile(schemaFile)
	if err != nil || !bytes.Equal(got, want) {
		t.Errorf("schema/runtree.schema.json is out of date, regenerate it with:\n" +
			"  go test ./internal/rt/ -run TestSchemaFileIsUpToDate -update-schema")
	}
}

func TestSchemaHasAllProperties(t *testing.T) {
	b, err := JSONSchema()
	if err != nil {
		t.Fatal(err)
	}
	var schema struct {
		Defs struct {
			Node struct {
				Properties map[string]struct {
					Description string `json:"description"`
				} `json:"properties"`
				AdditionalProperties bool `json:"additionalProperties"`
			} `json:"node"`
		} `json:"$defs"`
	}
	if err := json.Unmarshal(b, &schema); err != nil {
		t.Fatal(err)
	}
	node := schema.Defs.Node
	if len(node.Properties) != len(NodeProperties) || node.AdditionalProperties {
		t.Errorf("the node schema has %d properties (additional: %v), want %d", len(node.Properties),
			node.AdditionalProperties, len(NodeProperties))
	}
	for _, p := range NodeProperties {
		if node.Properties[p.Name].Description == "" {
			t.Errorf("%s has no description in the schema", p.Name)
		}
	}
}

// Every property is documented in the README, and the README does not list unknown properties.
