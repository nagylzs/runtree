package rt

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
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
	tree.SetLogHandler(slog.DiscardHandler)
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

// startTestRunner starts a runner on a free local port for the test, and returns its address. It runs the commands
// in ptys, like rtrunner.
func startTestRunner(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = l.Close() })
	go func() {
		for {
			conn, err := l.Accept()
			if err != nil {
				return
			}
			go func() { _ = runner.RunNewServer(context.Background(), conn, slog.New(slog.DiscardHandler)) }()
		}
	}()
	return l.Addr().String()
}

// ptyTerminalHelper connects to the pty of the started nodes like the GUI does (and drops their output), so that the
// runner client does not wait for a terminal.
type ptyTerminalHelper struct {
	fakeTerminalHelper
}

func (h *ptyTerminalHelper) AfterPtyStarted(n *Node) {
	conn, err := net.Dial("unix", n.Client.PtyPath)
	if err != nil {
		return // the node does not finish, the test fails
	}
	go func() {
		_, _ = io.Copy(io.Discard, conn)
		_ = conn.Close()
	}()
}

// killNode kills the process of a started node, and waits until it exits (it would outlive the test, because it runs
// in its own session).
func killNode(t *testing.T, n *Node) {
	t.Helper()
	waitStarted(t, n)
	n.LockTree("killNode")
	c := n.Client
	_, err := n.SendSignal(true, "SIGKILL")
	n.UnLockTree()
	if c == nil {
		return
	}
	if err != nil {
		t.Errorf("could not kill %s: %v", n.Id, err)
	}
	for deadline := time.Now().Add(5 * time.Second); ; time.Sleep(20 * time.Millisecond) {
		if ps := c.LastState(); !ps.Alive() {
			return
		}
		if time.Now().After(deadline) {
			t.Errorf("%s did not exit", n.Id)
			return
		}
	}
}

// waitStarted waits until the node is not starting anymore: it is connected to its runner and its process is started,
// or its start failed (see Node.Start)
func waitStarted(t *testing.T, n *Node) {
	t.Helper()
	for deadline := time.Now().Add(5 * time.Second); ; time.Sleep(5 * time.Millisecond) {
		n.Tree.RLock("waitStarted")
		starting := n.starting
		n.Tree.RUnlock()
		if !starting {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s is still starting", n.Id)
		}
	}
}
