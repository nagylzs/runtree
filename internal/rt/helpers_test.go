package rt

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/nagylzs/runtree/internal/events"
	"github.com/nagylzs/runtree/internal/runner"
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
	sources := NewSources()
	_, filename, err := AddTree(fn, sources)
	if err != nil {
		return nil, err
	}
	tree, err := ParseToDom(sources, filename, 30)
	if err != nil {
		return nil, err
	}
	tree.awaker = events.NewDebouncer(func(int) {}, true)
	tree.unblocker = events.NewDebouncer(func(int) {}, false)
	tree.logger = slog.New(slog.NewTextHandler(io.Discard, nil))
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

// fakeTerminalHelper records internal errors, everything else does nothing.
type fakeTerminalHelper struct {
	internalErrors []string
}

func (f *fakeTerminalHelper) GetWinSize() runner.WinSize               { return runner.WinSize{} }
func (f *fakeTerminalHelper) AfterPtyStarted(*Node)                    {}
func (f *fakeTerminalHelper) ProcStateChanged(*Node, runner.ProcState) {}
func (f *fakeTerminalHelper) OnNodeChanged(*Node, Status, bool)        {}
func (f *fakeTerminalHelper) OnClientError(bool, *Node, runner.Error)  {}
func (f *fakeTerminalHelper) OnInternalError(message string) {
	f.internalErrors = append(f.internalErrors, message)
}

// checkUnlocked fails the test if the tree lock cannot be acquired within a second.
func checkUnlocked(t *testing.T, tree *Tree) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		tree.Lock("checkUnlocked")
		tree.UnLock()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("the tree is still locked")
	}
}

// sprintf is fmt.Sprintf, for YAML templates
func sprintf(format string, args ...any) string {
	return fmt.Sprintf(format, args...)
}
