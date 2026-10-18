package main

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/user"

	"github.com/nagylzs/runtree/internal/config"
	"github.com/nagylzs/runtree/internal/logdb"
	"github.com/nagylzs/runtree/internal/logging"
	"github.com/nagylzs/runtree/internal/rt"
	"github.com/nagylzs/runtree/internal/version"
)

// openLogDB starts a run in the log database, and makes the tree save its events and the terminal output there
func openLogDB(path string, treeFile string, tree *rt.Tree) (*logdb.DB, error) {
	host, _ := os.Hostname()
	userName := ""
	if u, err := user.Current(); err == nil {
		userName = u.Username
	}
	db, err := logdb.Open(path, logdb.RunInfo{TreeFile: treeFile, Host: host, User: userName, Pid: os.Getpid(),
		Version: version.LocalVersion()})
	if err != nil {
		return nil, err
	}
	var nodes []logdb.NodeInfo
	var walk func(n *rt.Node)
	walk = func(n *rt.Node) {
		parent := ""
		if n.Parent != nil {
			parent = n.Parent.Id
		}
		nodes = append(nodes, logdb.NodeInfo{ID: n.Id, ParentID: parent, Type: rt.TypeName(n.Type),
			Label: n.DisplayLabel(), Path: n.Path()})
		for _, c := range n.Nodes {
			walk(c)
		}
	}
	walk(tree.Root)
	if err := db.AddNodes(nodes); err != nil {
		_ = db.Close()
		return nil, err
	}
	tree.SetLogHandler(logging.Multi(slog.Default().Handler(), db.Handler()))
	tree.SetOutputRecorder(outputRecorder{db})
	slog.Info("saving the log", "file", path, "run", db.RunID())
	return db, nil
}

type outputRecorder struct {
	db *logdb.DB
}

func (r outputRecorder) NodeOutput(n *rt.Node, start int) io.Writer {
	return r.db.Output(n.Id, start)
}

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
