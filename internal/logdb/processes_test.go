package logdb

import (
	"bytes"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

const writerEnv = "LOGDB_TEST_WRITER"

// When the test binary is started with writerEnv, it writes a run into that database, like a runtree
func TestMain(m *testing.M) {
	if path := os.Getenv(writerEnv); path != "" {
		if err := writeRun(path); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

const (
	runEvents = 2000
	runChunks = 2000 // of outputLine
)

var outputLine = bytes.Repeat([]byte("x"), 500)

// writeRun writes a run with runEvents events and runChunks output chunks, in half a second (several batches)
func writeRun(path string) error {
	d, err := Open(path, RunInfo{TreeFile: "/trees/upgrade.yaml", Host: "box", User: "admin", Pid: os.Getpid()})
	if err != nil {
		return err
	}
	if err := d.AddNodes([]NodeInfo{{ID: "n1", Type: "Run", Label: "host7", Path: "host7"}}); err != nil {
		return err
	}
	logger := slog.New(d.Handler()).With("node", "n1")
	out := d.Output("n1", 1)
	for i := 0; i < runEvents; i++ {
		logger.Info(fmt.Sprintf("event %d", i), "event", "test")
		_, _ = out.Write(outputLine)
		if i%10 == 0 {
			time.Sleep(2 * time.Millisecond)
		}
	}
	return d.Close()
}

// Several runtree processes can use the same log database at the same time, each of them saves its own run
func TestSeveralProcesses(t *testing.T) {
	path := filepath.Join(t.TempDir(), "log.db")
	var cmds []*exec.Cmd
	var outputs []*bytes.Buffer
	for i := 0; i < 3; i++ {
		cmd := exec.Command(os.Args[0], "-test.run=^$")
		cmd.Env = append(os.Environ(), writerEnv+"="+path)
		var out bytes.Buffer
		cmd.Stdout, cmd.Stderr = &out, &out
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		cmds, outputs = append(cmds, cmd), append(outputs, &out)
	}
	if err := writeRun(path); err != nil { // and this process too
		t.Error(err)
	}
	for i, cmd := range cmds {
		if err := cmd.Wait(); err != nil || outputs[i].Len() > 0 {
			t.Errorf("writer %d: %v %s", i, err, outputs[i])
		}
	}

	r := openTestReader(t, path)
	runs, err := r.Runs()
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 4 {
		t.Fatalf("%d runs", len(runs))
	}
	// they ran at the same time
	var lastStarted, firstFinished time.Time
	for _, run := range runs {
		if lastStarted.IsZero() || run.Started.After(lastStarted) {
			lastStarted = run.Started
		}
		if firstFinished.IsZero() || run.Finished.Before(firstFinished) {
			firstFinished = run.Finished
		}
	}
	if !lastStarted.Before(firstFinished) {
		t.Errorf("the runs did not overlap: the last one started at %s, the first one finished at %s", lastStarted,
			firstFinished)
	}
	for _, run := range runs {
		if run.Events != runEvents || run.Output != int64(runChunks*len(outputLine)) || run.Dropped != 0 ||
			run.Finished.IsZero() {
			t.Errorf("run %d: %d events, %d bytes of output, %d dropped, finished %v", run.ID, run.Events, run.Output,
				run.Dropped, run.Finished)
		}
	}
}
