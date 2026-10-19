package gtkui

import (
	"runtime"
	"sync"
	"testing"
	"time"

	coreglib "github.com/diamondburned/gotk4/pkg/core/glib"
	"github.com/diamondburned/gotk4/pkg/glib/v2"
)

// The functions of runOnMain run in the main loop (here the test goroutine iterates the default main context), each
// once, in the order of each goroutine; the ones that were added before the watch too. Then the main loop is idle:
// the pipe is read, the watch does not wake it up again. (Run it with -race: only the main loop uses ran and last.)
func TestRunOnMain(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	q, err := newMainLoopQueue()
	if err != nil {
		t.Fatal(err)
	}
	mainQueue = q

	const goroutines, calls = 8, 200
	const total = 1 + goroutines*calls
	ran := 0
	last := make([]int, goroutines)
	runOnMain(func() { ran++ }) // before the watch, e.g. a signal before the window is activated
	q.watch()
	var wg sync.WaitGroup
	for g := range goroutines {
		wg.Go(func() {
			for i := 1; i <= calls; i++ {
				runOnMain(func() {
					if last[g] != i-1 {
						t.Errorf("goroutine %d: function %d after %d", g, i, last[g])
					}
					last[g] = i
					ran++
				})
			}
		})
	}

	// the iteration blocks until a source is ready: the timer ends it when the functions do not run
	timer := coreglib.TimeoutAdd(100, func() bool { return true })
	ctx := glib.MainContextDefault()
	for deadline := time.Now().Add(10 * time.Second); ran < total && time.Now().Before(deadline); {
		ctx.Iteration(true)
	}
	coreglib.SourceRemove(timer)
	wg.Wait()
	if ran != total {
		t.Fatalf("%d functions ran, want %d", ran, total)
	}

	ctx.Iteration(false) // the byte of the last add may come after its function ran
	if ctx.Iteration(false) {
		t.Error("the main loop is woken up without functions")
	}
	if ran != total {
		t.Errorf("%d functions ran, want %d", ran, total)
	}
}
