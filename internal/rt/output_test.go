package rt

import (
	"bytes"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"
	"time"
)

// testRecorder records the terminal output of the nodes, by "id start"
type testRecorder struct {
	mu     sync.Mutex
	output map[string]*bytes.Buffer
}

type recorderWriter struct {
	r   *testRecorder
	key string
}

func (r *testRecorder) NodeOutput(n *Node, start int) io.Writer {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.output == nil {
		r.output = make(map[string]*bytes.Buffer)
	}
	key := fmt.Sprintf("%s %d", n.Id, start)
	r.output[key] = &bytes.Buffer{}
	return recorderWriter{r, key}
}

func (w recorderWriter) Write(p []byte) (int, error) {
	w.r.mu.Lock()
	defer w.r.mu.Unlock()
	return w.r.output[w.key].Write(p)
}

func (r *testRecorder) get(key string) (string, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	b, ok := r.output[key]
	if !ok {
		return "", false
	}
	return b.String(), true
}

const outputTree = `
tree:
  id: root
  seq:
    - id: hello
      args: [sh, -c, "echo hello; echo world"]
`

// waitFinished updates the statuses (like the scheduler) until the node is finished
func waitFinished(t *testing.T, n *Node) {
	t.Helper()
	for deadline := time.Now().Add(5 * time.Second); ; time.Sleep(10 * time.Millisecond) {
		n.LockTree("waitFinished")
		n.Tree.Root.updateStatusesDown(true)
		done := StatusFinished(n.Status)
		n.UnLockTree()
		if done {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s did not finish", n.Id)
		}
	}
}

func TestOutputRecorder(t *testing.T) {
	tree := loadTestTree(t, outputTree)
	tree.SetTerminalHelper(&ptyTerminalHelper{})
	recorder := &testRecorder{}
	tree.SetOutputRecorder(recorder)
	hello := tree.NodeById["hello"]
	hello.Calculated.Runner = startTestRunner(t)

	for start := 1; start <= 2; start++ { // the second one is a re-run
		tree.Lock("TestOutputRecorder")
		_, err := hello.PerformOp(OpRun, true, false, true, nil)
		tree.UnLock()
		if err != nil {
			t.Fatal(err)
		}
		waitFinished(t, hello)
		if hello.Starts != start {
			t.Errorf("starts: %d, want %d", hello.Starts, start)
		}
		// the output may arrive a bit later than the exit
		key := fmt.Sprintf("hello %d", start)
		var out string
		for deadline := time.Now().Add(2 * time.Second); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
			if out, _ = recorder.get(key); strings.Contains(out, "world") {
				break
			}
		}
		if out != "hello\r\nworld\r\n" {
			t.Errorf("the output of %s is %q", key, out)
		}
	}
	// the start is in the events
	var starts []string
	for _, e := range tree.EventLog().Since(0) {
		if e.Event == EventStart || e.Event == EventExited {
			s, _ := attrValue(e, "start")
			starts = append(starts, e.Event+" "+s)
		}
	}
	if got := strings.Join(starts, ", "); got != "start 1, exited 1, start 2, exited 2" {
		t.Errorf("the starts in the events: %s", got)
	}
}
