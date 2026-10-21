//go:build windows

package rt

import (
	"strings"
	"testing"
	"time"

	"github.com/nagylzs/runtree/internal/runner"
)

// On Windows, the processes of the local runner run in ConPTY pseudo consoles: their output has terminal control
// sequences around the lines, so the tests look for the lines in it. The trees run unattended (like with
// runtree-headless): the GUI does not run on Windows, there is no terminal socket.

// windowsTerminalHelper gives the size of the terminals like runtree-headless
type windowsTerminalHelper struct {
	fakeTerminalHelper
}

func (h *windowsTerminalHelper) GetWinSize() runner.WinSize {
	return runner.WinSize{Rows: 40, Cols: 120}
}

// loadWindowsTree loads a tree that runs unattended on the local runner, and records the output of its nodes
func loadWindowsTree(t *testing.T, source string) (*Tree, *testRecorder) {
	t.Helper()
	tree := loadTestTree(t, source)
	tree.SetTerminalHelper(&windowsTerminalHelper{})
	tree.SetUnattended()
	recorder := &testRecorder{}
	tree.SetOutputRecorder(recorder)
	return tree, recorder
}

// checkContains checks that the output of a start ("id start") contains want, it may arrive a bit later than the exit
func (r *testRecorder) checkContains(t *testing.T, key, want string) {
	t.Helper()
	var out string
	for deadline := time.Now().Add(2 * time.Second); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
		if out, _ = r.get(key); strings.Contains(out, want) {
			return
		}
	}
	t.Errorf("the output of %s is %q, want %q in it", key, out, want)
}

// The local runner runs the commands, the nodes get their output and exit codes
func TestLocalRunnerOnWindows(t *testing.T) {
	tree, recorder := loadWindowsTree(t, `
tree:
  id: root
  par:
    - {id: hello, args: [cmd, /c, "echo hello & echo world"]}
    - {id: fails, args: [cmd, /c, "exit /b 3"]}
`)
	runTimeoutTree(t, tree, func() bool { return StatusFinished(tree.Root.Status) })
	tree.RLock("TestLocalRunnerOnWindows")
	hello, fails := tree.NodeById["hello"], tree.NodeById["fails"]
	statuses := StatusName(hello.Status) + " " + StatusName(fails.Status) + " " + StatusName(tree.Root.Status)
	exitCode := -1
	if fails.Client != nil {
		exitCode = fails.Client.LastState().ExitCode
	}
	events := eventTexts(tree, 0)
	tree.RUnlock()
	if statuses != "Success Failed Failed" || exitCode != 3 {
		t.Errorf("hello, fails and root are %s, the exit code of fails is %d", statuses, exitCode)
	}
	checkEventsInOrder(t, events, `hello start INFO starting: cmd /c "echo hello & echo world"`,
		"hello exited INFO exited with code 0")
	checkEventsInOrder(t, events, "fails exited WARN exited with code 3")
	// ConPTY may write control sequences between them (e.g. the title of the console)
	recorder.checkContains(t, "hello 1", "hello\r\n")
	recorder.checkContains(t, "hello 1", "world\r\n")
}

// A node that runs longer than its timeout gets Ctrl+C, typed into its pseudo console: it stops ping
func TestTimeoutCtrlCOnWindows(t *testing.T) {
	tree, _ := loadWindowsTree(t, `
tree:
  id: root
  seq:
    - {id: slow, timeout: 1s, args: [ping, -n, "60", "127.0.0.1"]}
    - {id: next, args: [cmd, /c, "echo next"]}
`)
	began := time.Now()
	runTimeoutTree(t, tree, func() bool { return StatusFinished(tree.Root.Status) })
	elapsed := time.Since(began)
	tree.RLock("TestTimeoutCtrlCOnWindows")
	slow := tree.NodeById["slow"]
	status, timedOut := StatusName(tree.Root.Status)+" "+StatusName(slow.Status), slow.TimedOut
	next := tree.NodeById["next"].Starts
	events := eventTexts(tree, 0)
	tree.RUnlock()
	if status != "Failed Failed" || !timedOut || next != 0 {
		t.Errorf("root and slow: %s, timed out: %v, next started %d times", status, timedOut, next)
	}
	if elapsed < time.Second || elapsed > 5*time.Second {
		t.Errorf("the tree ran for %v", elapsed)
	}
	checkEventsInOrder(t, events, "slow timeout WARN timed out after 1s, typing Ctrl+C",
		"slow status WARN Running → Failed (timed out after 1s)")
	for _, e := range events {
		if strings.Contains(e, "sending SIG") {
			t.Errorf("Ctrl+C was not enough: %s", e)
		}
	}
}
