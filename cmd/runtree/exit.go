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
