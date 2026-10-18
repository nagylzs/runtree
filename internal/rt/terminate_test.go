package rt

import (
	"slices"
	"strings"
	"testing"
	"time"
)

// When runtree exits, the processes of the remote runners get SIGINT, then SIGTERM, and their exits are logged. The
// local ones are not signalled (the local runner terminates them, see runner.StopLocal).
func TestTerminateRemote(t *testing.T) {
	saved := remoteGrace
	remoteGrace = []time.Duration{500 * time.Millisecond, 500 * time.Millisecond}
	t.Cleanup(func() { remoteGrace = saved })
	tree := loadTestTree(t, `
tree:
  id: root
  par:
    - id: sleeper
      args: [sleep, "100"]
    - id: stubborn
      args: [sh, -c, "trap '' INT; echo ready; sleep 100"]
    - id: here
      args: [sleep, "100"]
`)
	tree.SetTerminalHelper(&ptyTerminalHelper{})
	recorder := &testRecorder{}
	tree.SetOutputRecorder(recorder)
	addr := startTestRunner(t)
	tree.Lock("TestTerminateRemote")
	for _, id := range []string{"sleeper", "stubborn", "here"} {
		n := tree.NodeById[id]
		if id != "here" {
			n.Calculated.Runner = addr
		}
		if err := n.Start(true); err != nil {
			t.Fatal(err)
		}
	}
	tree.UnLock()
	for _, id := range []string{"sleeper", "stubborn", "here"} {
		waitStarted(t, tree.NodeById[id])
	}
	t.Cleanup(func() { killNode(t, tree.NodeById["here"]) })
	recorder.check(t, "stubborn 1", "ready\r\n") // the trap is set

	if n := tree.TerminateRemote(10 * time.Second); n != 2 {
		t.Errorf("%d processes were terminated", n)
	}
	tree.RLock("TestTerminateRemote")
	events := eventTexts(tree, 0)
	statuses := StatusName(tree.NodeById["sleeper"].Status) + " " + StatusName(tree.NodeById["stubborn"].Status) +
		" " + StatusName(tree.NodeById["here"].Status)
	tree.RUnlock()
	if statuses != "Failed Failed Running" {
		t.Errorf("the statuses are %s", statuses)
	}
	var signals []string
	for _, e := range events {
		if strings.Contains(e, " signal ") || strings.Contains(e, " exited ") {
			signals = append(signals, e)
		}
	}
	for _, want := range []string{
		"sleeper signal INFO sending SIGINT",
		"sleeper exited WARN exited: signal: interrupt after ",
		"stubborn signal INFO sending SIGINT",
		"stubborn signal INFO sending SIGTERM",
		"stubborn exited WARN exited: signal: terminated after ",
	} {
		if !slices.ContainsFunc(signals, func(e string) bool { return strings.HasPrefix(e, want) }) {
			t.Errorf("%q is not in the events:\n%s", want, strings.Join(signals, "\n"))
		}
	}
	for _, e := range signals {
		if strings.HasPrefix(e, "here ") || strings.HasPrefix(e, "sleeper signal INFO sending SIGTERM") {
			t.Errorf("unexpected event: %s", e)
		}
	}
	// nothing is running remotely
	if n := tree.TerminateRemote(time.Second); n != 0 {
		t.Errorf("%d processes are running", n)
	}
}
