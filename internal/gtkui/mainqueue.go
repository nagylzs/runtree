package gtkui

/*
#cgo pkg-config: glib-2.0
#include <glib.h>

guint runtreeWatchMainQueue(int fd);
*/
import "C"

import (
	"sync"
	"syscall"
)

// Goroutines must not call GLib, not even glib.IdleAdd or glib.TimeoutAdd: they pass the function to runOnMain, and
// the main gtk loop runs it. On macOS 27 a pthread_rwlock that is contended while signals arrive hangs for good (Go
// sends signals to its threads), and all the sources of GLib share one (source_destroy_lock in gmain.c), so the GUI
// froze when goroutines added sources while the main loop added its own. runOnMain only writes to a pipe, that the
// main loop watches.

// mainQueue holds the functions for the main gtk loop, RunApplication creates it
var mainQueue *mainLoopQueue

// runOnMain runs f in the main gtk loop, when it is idle (like glib.IdleAdd). It can be called from any goroutine,
// and from the main gtk loop too (then f runs later).
func runOnMain(f func()) {
	mainQueue.add(f)
}

// mainLoopQueue is a queue of functions for the main gtk loop. add wakes up the main loop with a byte on a pipe, the
// main loop watches the read end of the pipe (watch), and runs the functions (run).
type mainLoopQueue struct {
	lock  sync.Mutex
	funcs []func()
	r, w  int // the ends of the pipe
}

func newMainLoopQueue() (*mainLoopQueue, error) {
	var fds [2]int
	if err := syscall.Pipe(fds[:]); err != nil {
		return nil, err
	}
	for _, fd := range fds {
		syscall.CloseOnExec(fd)
		if err := syscall.SetNonblock(fd, true); err != nil {
			syscall.Close(fds[0])
			syscall.Close(fds[1])
			return nil, err
		}
	}
	return &mainLoopQueue{r: fds[0], w: fds[1]}, nil
}

// watch adds the source to the default main context that runs the functions of mainQueue. It must be called from the
// main gtk loop (or before it runs), once. The functions that were added before are run too.
func (q *mainLoopQueue) watch() {
	C.runtreeWatchMainQueue(C.int(q.r))
}

func (q *mainLoopQueue) add(f func()) {
	q.lock.Lock()
	wake := len(q.funcs) == 0
	q.funcs = append(q.funcs, f)
	q.lock.Unlock()
	if !wake {
		// the byte of the first function is not read yet, run takes this one too
		return
	}
	for {
		// when the pipe is full (EAGAIN), the main loop has not read the earlier bytes yet, so it wakes up anyway
		if _, err := syscall.Write(q.w, []byte{0}); err != syscall.EINTR {
			return
		}
	}
}

// run is called from the main gtk loop when the pipe is readable. It reads the bytes first, and takes the functions
// after that: a function that is added after the read wakes up the main loop again.
func (q *mainLoopQueue) run() {
	var buf [64]byte
	for {
		n, err := syscall.Read(q.r, buf[:])
		if err == syscall.EINTR {
			continue
		}
		if err != nil || n < len(buf) {
			break
		}
	}
	q.lock.Lock()
	funcs := q.funcs
	q.funcs = nil
	q.lock.Unlock()
	for _, f := range funcs {
		f()
	}
}

//export runtreeMainQueueReady
func runtreeMainQueueReady(fd C.gint, condition C.GIOCondition, data C.gpointer) C.gboolean {
	mainQueue.run()
	return 1 // G_SOURCE_CONTINUE
}
