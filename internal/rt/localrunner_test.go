package rt

import (
	"net"
	"strings"
	"testing"
	"time"

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
	status, starts, nproc := hello.Status, hello.Starts, tree.Root.NProc
	tree.RUnlock()
	if status != StatusSuccess || starts != 1 || nproc != 0 {
		t.Errorf("the status is %s after %d starts, %d processes", StatusName(status), starts, nproc)
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

// Connecting to the runner does not hold the tree lock. Meanwhile the node is starting: it is running, and it counts
// as a process (for max_proc). When the connection fails, the node fails.
func TestStartInBackground(t *testing.T) {
	tree := loadTestTree(t, outputTree)
	tree.SetTerminalHelper(&ptyTerminalHelper{})
	// a runner that does not answer
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	accepted := make(chan net.Conn, 1)
	go func() {
		if c, err := l.Accept(); err == nil {
			accepted <- c
		}
	}()
	hello := tree.NodeById["hello"]
	hello.Calculated.Runner = l.Addr().String()
	tree.Lock("TestStartInBackground")
	err = hello.Start(true)
	tree.UnLock()
	if err != nil {
		t.Fatal(err)
	}
	var conn net.Conn
	select {
	case conn = <-accepted:
	case <-time.After(5 * time.Second):
		t.Fatal("no connection")
	}
	tree.Lock("TestStartInBackground") // it is not locked while connecting
	starting, status, nproc := hello.Starting(true), hello.Status, tree.Root.NProc
	tree.UnLock()
	if !starting || status != StatusRunning || nproc != 1 {
		t.Errorf("connecting: starting %v, %s, %d processes", starting, StatusName(status), nproc)
	}

	_ = conn.Close() // the version handshake fails
	waitStarted(t, hello)
	tree.RLock("TestStartInBackground")
	status, nproc, startError := hello.Status, tree.Root.NProc, hello.StartError
	tree.RUnlock()
	if status != StatusFailed || nproc != 0 || startError == "" {
		t.Errorf("after the failure: %s, %d processes, start error %q", StatusName(status), nproc, startError)
	}
}
