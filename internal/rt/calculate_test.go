package rt

import (
	"slices"
	"testing"

	"github.com/nagylzs/runtree/internal/config"
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
		"root":   config.DefaultRunnerAddress,
		"local":  config.DefaultRunnerAddress,
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
