package rt

import (
	"encoding/json"
	"strings"
)

// SchemaID is where the schema of tree files is published (schema/runtree.schema.json in the repository)
const SchemaID = "https://raw.githubusercontent.com/nagylzs/runtree/main/schema/runtree.schema.json"

// JSONSchema returns the JSON Schema of tree files, generated from NodeProperties. Editors (e.g. VS Code with the
// YAML extension) use it for completion, validation and hover documentation.
func JSONSchema() ([]byte, error) {
	props := make(map[string]any)
	for _, p := range NodeProperties {
		s := valueSchema(p)
		desc := p.Description
		if p.Inherited {
			desc += " Inherited from the parent node."
		}
		if p.Default != "" {
			desc += " Default: " + p.Default + "."
		}
		s["description"] = desc
		props[p.Name] = s
	}
	node := map[string]any{
		"type":                 "object",
		"properties":           props,
		"additionalProperties": false,
	}
	schema := map[string]any{
		"$schema":     "https://json-schema.org/draft/2020-12/schema",
		"$id":         SchemaID,
		"title":       "runtree tree file",
		"description": "A runtree file: the tree object, and other top level objects that can be included.",
		"type":        "object",
		"properties": map[string]any{
			"tree": map[string]any{"$ref": "#/$defs/node", "description": "The root node of the tree."},
		},
		// other top level objects can be included as nodes, non-objects (e.g. anchors) are allowed too
		"additionalProperties": ifObject(map[string]any{"$ref": "#/$defs/node"}, true),
		"$defs": map[string]any{
			"node": node,
		},
	}
	b, err := json.MarshalIndent(schema, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}

// The schemas below avoid anyOf where the type of the value tells which form it is (if/then/else, or one schema
// for several types), so that validators (and editors) report the actual problem, not just "no schema matches".

var stringOrStringList = map[string]any{"type": []string{"string", "array"}, "items": map[string]any{"type": "string"}}

// ifObject returns a schema that applies then to objects, and otherwise (for other types) to everything else
func ifObject(then any, otherwise any) map[string]any {
	return map[string]any{"if": map[string]any{"type": "object"}, "then": then, "else": otherwise}
}

func valueSchema(p Property) map[string]any {
	switch p.Kind {
	case KindString:
		if len(p.Enum) > 0 {
			return map[string]any{"type": "string", "enum": p.Enum}
		}
		return map[string]any{"type": "string"}
	case KindStringList:
		return map[string]any{"type": "array", "items": map[string]any{"type": "string"}}
	case KindBool:
		return map[string]any{"enum": []any{true, false, "yes", "no", "on", "off", "y", "n", "true", "false", "1", "0"}}
	case KindInt:
		return map[string]any{"type": "integer"}
	case KindExitCodes:
		return map[string]any{"type": "array", "minItems": 1,
			"items": map[string]any{"type": "integer", "minimum": 0, "maximum": 255}}
	case KindStringMap:
		return map[string]any{"type": "object", "additionalProperties": map[string]any{"type": "string"}}
	case KindVarMap:
		return map[string]any{"type": "object", "additionalProperties": stringOrStringList}
	case KindForVars:
		return map[string]any{"type": "object", "additionalProperties": stringOrStringList}
	case KindOnError:
		values := append(append([]string{}, OnErrorStatuses...), OnErrorSiblingOps...)
		alt := "(" + strings.Join(values, "|") + ")"
		return map[string]any{
			"if":   map[string]any{"type": "array"},
			"then": map[string]any{"minItems": 1, "maxItems": 2, "items": map[string]any{"enum": values}},
			"else": map[string]any{"type": "string", "pattern": "^\\s*" + alt + "\\s*(,\\s*" + alt + "\\s*)?$",
				"examples": values,
				// used by VS Code
				"patternErrorMessage": "expected a status (" + strings.Join(OnErrorStatuses, ", ") +
					") and/or a sibling operation (" + strings.Join(OnErrorSiblingOps, ", ") + "), e.g. paused or " +
					"\"failed, freeze\""},
		}
	case KindNodeList:
		return map[string]any{"type": "array", "items": ifObject(
			map[string]any{"$ref": "#/$defs/node"},
			map[string]any{"type": "array", "items": map[string]any{"type": "string"},
				"description": "a run node, given as a list of arguments"})}
	case KindInclude:
		return stringOrStringList
	}
	panic("valueSchema: unknown value kind of property " + p.Name)
}
