package rt

import (
	"strings"
	"testing"

	"github.com/nagylzs/runtree/internal/runner"
)

// Without a runner property, the scheduler starts the processes on the local runner, in runtree
func TestLocalRunnerIsTheDefault(t *testing.T) {
	tree := loadTestTree(t, outputTree)
	tree.SetTerminalHelper(&ptyTerminalHelper{})
	recorder := &testRecorder{}
	tree.SetOutputRecorder(recorder)
	hello := tree.NodeById["hello"]
	if hello.Calculated.Runner != runner.LocalAddress {
		t.Fatalf("the runner is %q", hello.Calculated.Runner)
	}
	tree.schedule()
	waitFinished(t, hello)
	tree.RLock("TestLocalRunnerIsTheDefault")
	status, starts := hello.Status, hello.Starts
	tree.RUnlock()
	if status != StatusSuccess || starts != 1 {
		t.Errorf("the status is %s after %d starts", StatusName(status), starts)
	}
	recorder.check(t, "hello 1", "hello\r\nworld\r\n")
	// the local runner is not named in the message
	if e := tree.EventLog().Since(0)[0]; e.Event != EventStart ||
		!strings.HasPrefix(e.Message, `starting: sh -c "echo hello; echo world"`) {
		t.Errorf("the start event is %s %q", e.Event, e.Message)
	}
}

// A stopped scheduler does not start nodes
func TestStopScheduler(t *testing.T) {
	tree := loadTestTree(t, outputTree)
	tree.SetTerminalHelper(&ptyTerminalHelper{})
	tree.StopScheduler()
	tree.schedule() // it would start hello, see TestLocalRunnerIsTheDefault
	hello := tree.NodeById["hello"]
	if hello.Starts != 0 || hello.Status != StatusWaiting {
		t.Errorf("the status is %s after %d starts", StatusName(hello.Status), hello.Starts)
	}
}
