package main

import (
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/nagylzs/runtree/internal/app"
	"github.com/nagylzs/runtree/internal/logdb"
	"github.com/nagylzs/runtree/internal/rt"
)

// The exit codes with --exit-on-finish, like the ones of runtree-headless
const (
	exitFailed     = 2 // the root node is Failed
	exitUnfinished = 3 // the window was closed before the root node finished
)

// finishExitCode is the exit code with --exit-on-finish, from the status of the root node when runtree exits
func finishExitCode(root rt.Status) int {
	switch {
	case !rt.StatusFinished(root):
		return exitUnfinished
	case rt.StatusHasError(root):
		return exitFailed
	}
	return 0
}

// handleTerm handles SIGTERM (it ends runtree without the close confirmation of the GUI): it terminates the processes
// of the local runner, and closes the log database (when db is not nil), so that the end of the run is saved too.
func handleTerm(tree *rt.Tree, db *logdb.DB) {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGTERM)
	go func() {
		<-ch
		slog.Warn("terminated, exiting")
		app.StopRunning(tree)
		if db != nil {
			app.CloseLogDB(db)
		}
		os.Exit(128 + int(syscall.SIGTERM))
	}()
}
