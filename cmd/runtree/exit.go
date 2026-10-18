package main

import (
	"log/slog"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"github.com/nagylzs/runtree/internal/logdb"
	"github.com/nagylzs/runtree/internal/rt"
	"github.com/nagylzs/runtree/internal/runner"
)

// stopRunning stops the scheduler, and terminates the running processes when runtree exits, so that they do not
// outlive runtree: the local runner terminates its own ones (as rtrunner does when runtree disconnects), the ones on
// ssh and TCP runners get the signals through their connections. It waits until they exit, and their exit is logged.
func stopRunning(tree *rt.Tree) {
	tree.StopScheduler()
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		if n := runner.LocalProcesses(); n > 0 {
			slog.Warn("terminating the processes of the local runner", "processes", n)
		}
		if !runner.StopLocal(runner.StopLocalTimeout) {
			slog.Error("some processes of the local runner did not exit", "processes", runner.LocalProcesses())
		}
	}()
	go func() {
		defer wg.Done()
		tree.TerminateRemote(runner.StopLocalTimeout)
	}()
	wg.Wait()
}

// handleTerm handles SIGTERM (it ends runtree without the close confirmation of the GUI): it terminates the processes
// of the local runner, and closes the log database (when db is not nil), so that the end of the run is saved too.
func handleTerm(tree *rt.Tree, db *logdb.DB) {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGTERM)
	go func() {
		<-ch
		slog.Warn("terminated, exiting")
		stopRunning(tree)
		if db != nil {
			if err := db.Close(); err != nil {
				slog.Error("could not close the log database", "error", err)
			}
		}
		os.Exit(128 + int(syscall.SIGTERM))
	}()
}
