package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/nagylzs/runtree/internal/config"
	"github.com/nagylzs/runtree/internal/logdb"
)

// showLog prints from the log database for --show-runs, --show-events and --show-output
func showLog(args config.CLIArgs) error {
	if args.LogDB == "" {
		return errors.New("--show-runs, --show-events and --show-output need --log-db FILE")
	}
	r, err := logdb.OpenReader(args.LogDB)
	if err != nil {
		return err
	}
	defer r.Close()
	if args.ShowRuns {
		return logdb.PrintRuns(os.Stdout, r)
	}
	runID := args.Run
	if runID == 0 {
		if runID, err = r.LastRun(); err != nil {
			return err
		}
	}
	if args.ShowEvents {
		return logdb.PrintEvents(os.Stdout, r, runID)
	}
	node, err := logdb.FindNode(r, runID, args.ShowOutput)
	if err != nil {
		return err
	}
	start, starts, err := logdb.PrintOutput(os.Stdout, r, runID, node, args.Start)
	if err == nil && starts > 1 {
		_, _ = fmt.Fprintf(os.Stderr, "(start %d of %d of %s, see --start)\n", start, starts, node.Path)
	}
	return err
}
