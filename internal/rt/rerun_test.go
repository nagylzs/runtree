package rt

import (
	"net"
	"slices"
	"strings"
	"testing"
)

const rerunTree = `
tree:
  id: root
  par:
    - id: build
      provides: [binary]
      args: [sleep, "30"]
    - id: deploy
      requires: [binary]
      args: ["true"]
`

func TestPlanRun(t *testing.T) {
	tree := loadTestTree(t, rerunTree)
	build := tree.NodeById["build"]
	if plan := build.PlanOp(OpRun, true, false); len(plan.Unprovide) != 0 {
		t.Errorf("build has not provided anything yet: %v", names(plan.Unprovide))
	}
	setStatus(tree, "build", StatusSuccess)
	plan := build.PlanOp(OpRun, true, false)
	if got := names(plan.Unprovide); !slices.Equal(got, []string{"binary"}) {
		t.Fatalf("unprovide: got %v", got)
	}
	if got := ids(plan.Unprovide[0].RequiredBy); !slices.Equal(got, []string{"deploy"}) {
		t.Errorf("required by: got %v", got)
	}
	// the other operations do not unprovide
	if plan := build.PlanOp(OpFreeze, true, false); len(plan.Unprovide) != 0 {
		t.Errorf("freeze: %v", names(plan.Unprovide))
	}
}

func TestPlanRunParent(t *testing.T) {
	tree := loadTestTree(t, resetTestTree)
	for _, id := range []string{"build", "deploy", "run", "pipeline"} {
		setStatus(tree, id, StatusSuccess)
	}
	// the successful parent becomes running
	plan := tree.NodeById["deploy"].PlanRun(true)
	if got := names(plan.Unprovide); !slices.Equal(got, []string{"deployed", "pipeline-done"}) {
		t.Errorf("unprovide: got %v", got)
	}
	// binary is provided by mirror too
	setStatus(tree, "mirror", StatusSuccess)
	plan = tree.NodeById["build"].PlanRun(true)
	if got := names(plan.Unprovide); !slices.Equal(got, []string{"pipeline-done"}) {
		t.Errorf("unprovide with mirror: got %v", got)
	}
}

// succeededBuild returns the tree where build succeeded (so deploy can start), with a runner for the commands
func succeededBuild(t *testing.T, runnerAddr string) *Tree {
	tree := loadTestTree(t, rerunTree)
	tree.SetTerminalHelper(&ptyTerminalHelper{})
	for _, n := range tree.Nodes {
		n.Calculated.Runner = runnerAddr
	}
	setStatus(tree, "build", StatusSuccess)
	tree.Root.updateBlockedAll(true)
	if tree.NodeById["deploy"].Blocked {
		t.Fatal("deploy is blocked, but binary is provided")
	}
	return tree
}

func TestRerunUnprovides(t *testing.T) {
	addr := startTestRunner(t)
	for _, tc := range []struct {
		args     *OpArgs
		provided bool // binary, while build runs again
	}{
		{nil, false},                           // unprovide all (the default)
		{&OpArgs{Unprovide: []string{}}, true}, // nothing is selected in the dialog
	} {
		tree := succeededBuild(t, addr)
		build, deploy := tree.NodeById["build"], tree.NodeById["deploy"]
		seq := lastSeq(tree)
		tree.Lock("TestRerunUnprovides")
		_, err := build.PerformOp(OpRun, true, false, true, tc.args)
		status, provided, blocked := build.Status, tree.provided.Contains("binary"), deploy.Blocked
		events := eventTexts(tree, seq)
		tree.UnLock()
		killNode(t, build)

		if err != nil {
			t.Fatalf("%+v: %v", tc.args, err)
		}
		if status != StatusRunning {
			t.Errorf("%+v: build is %s", tc.args, StatusName(status))
		}
		if provided != tc.provided || blocked == tc.provided {
			t.Errorf("%+v: binary is provided: %v, deploy is blocked: %v", tc.args, provided, blocked)
		}
		unprovided := slices.Contains(events, "build unprovided INFO unprovided binary")
		if unprovided == tc.provided {
			t.Errorf("%+v: the events are\n%s", tc.args, strings.Join(events, "\n"))
		}
	}
}

func TestRerunStartFailed(t *testing.T) {
	// an address where nothing listens
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := l.Addr().String()
	_ = l.Close()

	tree := succeededBuild(t, addr)
	build := tree.NodeById["build"]
	if _, err := build.PerformOp(OpRun, true, false, true, nil); err == nil {
		t.Fatal("the start did not fail")
	}
	// the node failed (even though its success was manual), it does not provide binary anymore
	checkStatus(t, tree, "build", StatusFailed)
	if tree.provided.Contains("binary") || !tree.NodeById["deploy"].Blocked {
		t.Errorf("binary is still provided")
	}
	if events := eventTexts(tree, 0); !slices.Contains(events, "build unprovided INFO unprovided binary") {
		t.Errorf("the events are\n%s", strings.Join(events, "\n"))
	}
}
