// Package app is what runtree (the GUI) and runtree-headless share: loading the tree for a run, the lock file, the log
// database, and stopping the processes when they exit.
package app

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/user"
	"path/filepath"
	"sync"
	"time"

	"github.com/nagylzs/runtree/internal/lockfile"
	"github.com/nagylzs/runtree/internal/logdb"
	"github.com/nagylzs/runtree/internal/rt"
	"github.com/nagylzs/runtree/internal/runner"
	"github.com/nagylzs/runtree/internal/version"
)

// ExitLocked is the exit code when the lock file is locked by another runtree (EX_TEMPFAIL: try again later)
const ExitLocked = 75

// LoadTree loads a tree file for a run (fileArg is the file as it was given). The warnings are printed to stderr, and
// returned too. maxDepth is the --max-depth option, defaultRunner is --runner.
func LoadTree(fileArg string, defaultRunner string, maxDepth uint) (*rt.Tree, string, rt.ParseErrors, error) {
	fullFilePath, err := filepath.Abs(fileArg)
	if err != nil {
		return nil, "", nil, fmt.Errorf("could open input file %v: %w", fileArg, err)
	}
	sources := rt.NewSources()
	sources.DefaultRunner = defaultRunner
	_, filename, err := rt.AddTree(fullFilePath, sources)
	if err != nil {
		return nil, fullFilePath, nil, err
	}
	tree, err := rt.ParseToDom(sources, filename, maxDepth)
	// the warnings are printed before the errors (see PrintLoadError)
	warnings := sources.Warnings()
	if len(warnings) > 0 {
		_, _ = fmt.Fprintln(os.Stderr, warnings.Error())
	}
	return tree, fullFilePath, warnings, err
}

// PrintCheck prints the result of --check: the tree file (as it was given) is OK, with the numbers of the nodes and
// the warnings
func PrintCheck(fileArg string, tree *rt.Tree, warnings rt.ParseErrors) {
	_, _ = fmt.Fprintf(os.Stderr, "%s: OK%s%s\n", fileArg, plural(", %d node", len(tree.Nodes)),
		plural(", %d warning", len(warnings)))
}

// plural formats a count with format (e.g. ", %d warning"), with an "s" if count > 1. It is empty when count is 0.
func plural(format string, count int) string {
	if count == 0 {
		return ""
	}
	res := fmt.Sprintf(format, count)
	if count > 1 {
		res += "s"
	}
	return res
}

// PrintLoadError prints the errors of loading the tree (rt.ParseErrors, *rt.ParseError) and of its lock file
// (*lockfile.LockedError) to stderr, and returns the exit code: 1, or ExitLocked. It returns 0 for other errors, and
// does not print them.
func PrintLoadError(err error) int {
	var parseErrs rt.ParseErrors
	var parseErr *rt.ParseError
	var lockedErr *lockfile.LockedError
	switch {
	case errors.As(err, &parseErrs):
		// file:line:col: message lines, and their details on separate lines
		_, _ = fmt.Fprintln(os.Stderr, parseErrs.Error())
		if len(parseErrs) > 1 {
			_, _ = fmt.Fprintf(os.Stderr, "%d errors\n", len(parseErrs))
		}
		return 1
	case errors.As(err, &lockedErr):
		_, _ = fmt.Fprintln(os.Stderr, "the tree cannot be run now: "+lockedErr.Error())
		return ExitLocked
	case errors.As(err, &parseErr):
		_, _ = fmt.Fprintln(os.Stderr, parseErr.Error())
		return 1
	}
	return 0
}

// LockTree acquires the lock file of --lock for the run of the tree
func LockTree(path, treeFile string) (*lockfile.Lock, error) {
	userName, host := CurrentUser()
	holder := fmt.Sprintf("%s@%s since %s, tree %s", userName, host, time.Now().Format("2006-01-02 15:04:05 -0700"),
		treeFile)
	l, err := lockfile.Acquire(path, holder)
	if err != nil {
		return nil, err
	}
	slog.Info("holding the lock", "file", path)
	return l, nil
}

// CurrentUser returns the name of the user and the host
func CurrentUser() (string, string) {
	host, _ := os.Hostname()
	name := ""
	if u, err := user.Current(); err == nil {
		name = u.Username
	}
	return name, host
}

// OpenLogDB starts a run in the log database (--log-db), with the nodes of the tree. The caller makes the tree save
// its events (DB.Handler) and its terminal output (LogDBRecorder) there.
func OpenLogDB(path string, treeFile string, tree *rt.Tree) (*logdb.DB, error) {
	userName, host := CurrentUser()
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
	slog.Info("saving the log", "file", path, "run", db.RunID())
	return db, nil
}

// CloseLogDB closes the log database, it logs the error
func CloseLogDB(db *logdb.DB) {
	if err := db.Close(); err != nil {
		slog.Error("could not close the log database", "error", err)
	}
}

// LogDBRecorder saves the terminal output of the nodes in the log database
func LogDBRecorder(db *logdb.DB) rt.OutputRecorder {
	return logDBRecorder{db}
}

type logDBRecorder struct {
	db *logdb.DB
}

func (r logDBRecorder) NodeOutput(n *rt.Node, start int) io.Writer {
	return r.db.Output(n.Id, start)
}

// StopRunning stops the scheduler, and terminates the running processes when runtree exits, so that they do not
// outlive runtree: the local runner terminates its own ones (as rtrunner does when runtree disconnects), the ones on
// ssh and TCP runners get the signals through their connections. It waits until they exit, and their exit is logged.
func StopRunning(tree *rt.Tree) {
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
