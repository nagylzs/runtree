package rt

import (
	"slices"
	"testing"
)

const resetTestTree = `
tree:
  id: root
  par:
    - type: par
      id: pipeline
      provides: ["pipeline-done"]
      nodes:
        - type: run
          id: build
          provides: ["binary"]
          args: ["true"]
        - type: run
          id: deploy
          requires: ["binary"]
          provides: ["deployed"]
          args: ["true"]
        - type: run
          id: run
          requires: ["deployed"]
          args: ["false"]
    - type: run
      id: mirror
      provides: ["binary"]
      args: ["true"]
`

func names(resources []ProvidedResource) []string {
	result := make([]string, len(resources))
	for i, r := range resources {
		result[i] = r.Name
	}
	return result
}

// the scenario from the GUI: build and deploy succeeded, run is paused
func pausedPipeline(t *testing.T) *Tree {
	tree := loadTestTree(t, resetTestTree)
	setStatus(tree, "build", StatusSuccess)
	setStatus(tree, "deploy", StatusSuccess)
	setStatus(tree, "run", StatusPaused)
	setStatus(tree, "pipeline", StatusRunning)
	setStatus(tree, "root", StatusRunning)
	return tree
}

func TestPlanResetToWaiting(t *testing.T) {
	tree := pausedPipeline(t)
	plan := tree.NodeById["pipeline"].PlanResetToWaiting(true, true)
	if got := names(plan.Unprovide); !slices.Equal(got, []string{"binary", "deployed"}) {
		t.Errorf("unprovide: got %v", got)
	}
	if got := ids(plan.Unprovide[0].ProvidedBy); !slices.Equal(got, []string{"build"}) {
		t.Errorf("binary provided by: got %v", got)
	}
	if got := ids(plan.Unprovide[0].RequiredBy); !slices.Equal(got, []string{"deploy"}) {
		t.Errorf("binary required by: got %v", got)
	}
	if len(plan.Running) != 0 {
		t.Errorf("running: got %v", ids(plan.Running))
	}

	// only the reset node's resources
	plan = tree.NodeById["build"].PlanResetToWaiting(true, false)
	if got := names(plan.Unprovide); !slices.Equal(got, []string{"binary"}) {
		t.Errorf("single node: got %v", got)
	}
}

func TestPlanResetToWaitingOtherProvider(t *testing.T) {
	tree := pausedPipeline(t)
	setStatus(tree, "mirror", StatusSuccess) // also provides binary, and it is not reset
	plan := tree.NodeById["pipeline"].PlanResetToWaiting(true, true)
	if got := names(plan.Unprovide); !slices.Equal(got, []string{"deployed"}) {
		t.Errorf("unprovide: got %v", got)
	}
}

func TestPlanResetToWaitingRunning(t *testing.T) {
	tree := pausedPipeline(t)
	setStatus(tree, "run", StatusRunning)
	plan := tree.NodeById["pipeline"].PlanResetToWaiting(true, true)
	if got := ids(plan.Running); !slices.Equal(got, []string{"run"}) {
		t.Errorf("running: got %v", got)
	}
	if got := names(plan.Unprovide); !slices.Equal(got, []string{"binary", "deployed"}) {
		t.Errorf("unprovide: got %v", got)
	}
}

func TestPlanResetToWaitingParent(t *testing.T) {
	tree := loadTestTree(t, resetTestTree)
	for _, id := range []string{"build", "deploy", "run", "pipeline"} {
		setStatus(tree, id, StatusSuccess)
	}
	// resetting one child makes the successful parent unfinished
	plan := tree.NodeById["deploy"].PlanResetToWaiting(true, false)
	if got := names(plan.Unprovide); !slices.Equal(got, []string{"deployed", "pipeline-done"}) {
		t.Errorf("unprovide: got %v", got)
	}
}

func TestUnprovideLost(t *testing.T) {
	for _, tc := range []struct {
		names []string
		want  []string // provided after the reset
	}{
		{nil, []string{}},
		{[]string{}, []string{"binary", "deployed"}},
		{[]string{"deployed"}, []string{"binary"}},
	} {
		tree := pausedPipeline(t)
		before := tree.providerNodes()
		setStatus(tree, "build", StatusWaiting)
		setStatus(tree, "deploy", StatusWaiting)
		tree.unprovideLost(before, tc.names)
		got := tree.provided.List()
		slices.Sort(got)
		if !slices.Equal(got, tc.want) {
			t.Errorf("names=%v: provided %v, want %v", tc.names, got, tc.want)
		}
		_, ok := tree.providedBy["deployed"]
		if ok != slices.Contains(tc.want, "deployed") {
			t.Errorf("names=%v: providedBy is not consistent", tc.names)
		}
	}
}

func TestUnprovideLostOtherProvider(t *testing.T) {
	tree := pausedPipeline(t)
	setStatus(tree, "mirror", StatusSuccess)
	before := tree.providerNodes()
	setStatus(tree, "build", StatusWaiting)
	tree.unprovideLost(before, nil)
	if !tree.provided.Contains("binary") {
		t.Errorf("binary is still provided by mirror, it must not be unprovided")
	}
	if tree.providedBy["binary"].Id != "mirror" {
		t.Errorf("binary must be provided by mirror, got %s", tree.providedBy["binary"].Id)
	}
}
