package rt

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/nagylzs/runtree/internal/events"
)

// Helpers for the rt test suite. Run all tests with: go test ./...

// loadTestTree loads a tree from YAML source, and disables the scheduler and unblocking, so nothing is started.
// The statuses can be set with setStatus.
func loadTestTree(t *testing.T, source string) *Tree {
	t.Helper()
	tree, err := parseTestTree(t, source)
	if err != nil {
		t.Fatal(err)
	}
	return tree
}

// parseTestTree is like loadTestTree, but it returns the parse error instead of failing the test.
func parseTestTree(t *testing.T, source string) (*Tree, error) {
	t.Helper()
	fn := filepath.Join(t.TempDir(), "tree.yaml")
	if err := os.WriteFile(fn, []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	allTrees := make(map[string]map[string]interface{})
	_, filename, err := AddTree(fn, allTrees)
	if err != nil {
		return nil, err
	}
	tree, err := ParseToDom(allTrees, filename, 30)
	if err != nil {
		return nil, err
	}
	tree.awaker = events.NewDebouncer(func(int) {}, true)
	tree.unblocker = events.NewDebouncer(func(int) {}, false)
	return tree, nil
}

// setStatus sets the status of a node, and registers its provided resources like updateStatus does.
// The tests have no runner clients, so a status recalculation would make every run node waiting. To survive it,
// Success is set as a manual status, and Failed as a start error (it becomes on_error status) for run nodes.
func setStatus(tree *Tree, id string, st Status) {
	n := tree.NodeById[id]
	n.Status = st
	if n.Type == TypeRun {
		n.WantManualStatus, n.StartError = false, ""
		switch st {
		case StatusSuccess:
			n.WantManualStatus, n.ManualStatus = true, StatusSuccess
		case StatusFailed:
			n.StartError = "failed in a test"
		}
	}
	if providerStatus(st) {
		for _, name := range n.Calculated.Provides.List() {
			if !tree.provided.Contains(name) {
				tree.provided.Add(name)
				tree.providedBy[name] = n
			}
		}
	}
}

func ids(nodes []*Node) []string {
	result := make([]string, len(nodes))
	for i, n := range nodes {
		result[i] = n.Id
	}
	return result
}
