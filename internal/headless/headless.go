// Package headless runs a tree without the GUI (runtree-headless), e.g. in CI or from cron: nobody watches it, so it
// is unattended (see rt.Tree.SetUnattended). The terminal output of the nodes goes to stdout, every line with the label
// of its node; the events of the tree go to stderr. The exit code tells how the run ended.
package headless

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"syscall"
	"time"

	"github.com/nagylzs/runtree/internal/app"
	"github.com/nagylzs/runtree/internal/config"
	"github.com/nagylzs/runtree/internal/logdb"
	"github.com/nagylzs/runtree/internal/logging"
	"github.com/nagylzs/runtree/internal/rt"
	"github.com/nagylzs/runtree/internal/runner"
)

// The exit codes of runtree-headless. 1 and 75 (app.ExitLocked) are the same as for runtree, a signal is 128 + its
// number (130 for SIGINT, 143 for SIGTERM).
const (
	ExitSuccess = 0
	ExitError   = 1 // the tree has errors, or a command line error
	ExitFailed  = 2 // the root node failed
	ExitStopped = 3 // the run stopped before the root finished: nothing runs, and nothing can be started
)

// outputWait is how long the output of the nodes may arrive after the run ended (the client of a node closes its
// output a bit after the exit of its process)
const outputWait = 10 * time.Second

// winSize is the size of the terminals of the processes
var winSize = runner.WinSize{Rows: 40, Cols: 120}

// Run runs the tree of fileArg, and returns the exit code. The output of the nodes goes to stdout, the events and the
// summary at the end go to stderr. A signal on sigs stops the run: the processes are terminated; a second one exits
// at once.
func Run(args config.HeadlessCLIArgs, fileArg string, stdout, stderr io.Writer, sigs <-chan os.Signal) int {
	runner.SetBinaryDir(args.RtrunnerDir)
	runner.SetSSHShare(!args.NoSSHShare)
	tree, fullFilePath, warnings, err := app.LoadTree(fileArg, args.Runner, args.MaxDepth)
	if err != nil {
		return loadError(err)
	}
	if args.Check {
		app.PrintCheck(fileArg, tree, warnings)
		return ExitSuccess
	}
	release, err := app.LockTree(args.Lock, tree, fullFilePath)
	if err != nil {
		return loadError(err)
	}
	defer release()

	level := slog.LevelInfo
	if args.Debug {
		level = slog.LevelDebug
	}
	var events slog.Handler = newTextHandler(stderr, level)
	if args.LogFormat == "json" {
		events = logging.NewHandler(stderr, level, "json")
	}
	out := newConsole(stdout, tree, args.Output)
	var output rt.OutputRecorder = out
	if args.LogDB != "" {
		var db *logdb.DB
		db, err = app.OpenLogDB(args.LogDB, fullFilePath, tree)
		if err != nil {
			slog.Error("could not open the log database: " + err.Error())
			return ExitError
		}
		defer app.CloseLogDB(db)
		events = logging.Multi(events, db.Handler())
		output = recorders{out, app.LogDBRecorder(db)}
	}
	tree.SetLogHandler(events)
	tree.SetOutputRecorder(output)
	tree.SetTerminalHelper(terminal{out})
	tree.SetUnattended()
	if len(args.Thaw) > 0 {
		tree.Lock("--thaw")
		err := tree.ThawGates(true, args.Thaw)
		tree.UnLock()
		if err != nil {
			_, _ = fmt.Fprintln(stderr, "--thaw: "+err.Error())
			return ExitError
		}
	}

	began := time.Now()
	go tree.Run()
	var sig os.Signal
	select {
	case <-tree.Ended():
	case sig = <-sigs:
		_, _ = fmt.Fprintf(stderr, "%s: terminating the processes (again to exit at once)\n", signalName(sig))
		go func() {
			sig := <-sigs
			_, _ = fmt.Fprintf(stderr, "%s again: exiting\n", signalName(sig))
			os.Exit(signalExit(sig))
		}()
	}
	app.StopRunning(tree)
	if !out.wait(outputWait) {
		slog.Warn("the end of the output of some nodes is missing")
	}
	elapsed := time.Since(began)

	tree.RLock("headless summary")
	defer tree.RUnlock()
	out.finishAll(func(n *rt.Node) bool { return n.Status == rt.StatusFailed })
	if sig != nil {
		_, _ = fmt.Fprintf(stderr, "The run was interrupted after %s (%s).\n", formatElapsed(elapsed), signalName(sig))
		printFailed(stderr, tree)
		return signalExit(sig)
	}
	end := tree.End()
	code := ExitSuccess
	switch {
	case end.Stopped:
		code = ExitStopped
		_, _ = fmt.Fprintf(stderr, "The run stopped after %s: nothing runs, and nothing can be started.\n",
			formatElapsed(elapsed))
		_, _ = fmt.Fprintln(stderr, "Waiting for:")
		for _, h := range end.Holds {
			_, _ = fmt.Fprintf(stderr, "  %s: %s\n", h.Node.DisplayLabel(), h.Reason)
		}
	default:
		if end.Status != rt.StatusSuccess {
			code = ExitFailed
		}
		_, _ = fmt.Fprintf(stderr, "The run ended after %s: %s.\n", formatElapsed(elapsed), rt.StatusName(end.Status))
	}
	printFailed(stderr, tree)
	return code
}

// loadError prints the error of loading or locking the tree, and returns the exit code
func loadError(err error) int {
	if code := app.PrintLoadError(err); code != 0 {
		return code
	}
	slog.Error(err.Error())
	return ExitError
}

func signalName(sig os.Signal) string {
	switch sig {
	case os.Interrupt:
		return "SIGINT"
	case syscall.SIGTERM:
		return "SIGTERM"
	}
	return sig.String()
}

func signalExit(sig os.Signal) int {
	if s, ok := sig.(syscall.Signal); ok {
		return 128 + int(s)
	}
	return ExitError
}

// printFailed lists the failed run nodes, in the order of the tree, with their exit codes (or errors). It must be
// called with the tree locked.
func printFailed(w io.Writer, tree *rt.Tree) {
	var failed []*rt.Node
	var walk func(n *rt.Node)
	walk = func(n *rt.Node) {
		if n.Type == rt.TypeRun && n.Status == rt.StatusFailed {
			failed = append(failed, n)
		}
		for _, c := range n.Nodes {
			walk(c)
		}
	}
	walk(tree.Root)
	if len(failed) == 0 {
		return
	}
	_, _ = fmt.Fprintln(w, "Failed:")
	for _, n := range failed {
		_, _ = fmt.Fprintf(w, "  %s: %s\n", n.DisplayLabel(), failure(n))
	}
}

// failure tells why a failed run node failed
func failure(n *rt.Node) string {
	if n.StartError != "" {
		return "could not start: " + n.StartError
	}
	if n.Client == nil {
		return "failed"
	}
	why := "failed"
	ps := n.Client.LastState()
	if ps.Exited && ps.ExitCode >= 0 {
		why = "exit code " + rt.ExitCodeText(ps.ExitCode)
	} else if ps.Error != "" {
		why = ps.Error
	}
	if n.TimedOut {
		return fmt.Sprintf("timed out after %s, %s", rt.DurationText(n.Timeout), why)
	}
	return why
}

// formatElapsed formats a duration for humans, in seconds (or with a decimal under 10 seconds)
func formatElapsed(d time.Duration) string {
	if d < 10*time.Second {
		return d.Round(100 * time.Millisecond).String()
	}
	return d.Round(time.Second).String()
}

// terminal is the TerminalHelper of the tree: there are no terminals
// terminal is the TerminalHelper of the tree: there are no terminals, it tells the console when a node finishes
type terminal struct {
	out *console
}

func (terminal) GetWinSize() runner.WinSize                  { return winSize }
func (terminal) AfterPtyStarted(*rt.Node)                    {}
func (terminal) ProcStateChanged(*rt.Node, runner.ProcState) {}
func (t terminal) OnNodeChanged(n *rt.Node, old rt.Status, _ bool) {
	if n.Type == rt.TypeRun && rt.StatusActive(old) && !rt.StatusActive(n.Status) {
		t.out.finished(n, n.Status == rt.StatusFailed)
	}
}
func (terminal) OnClientError(bool, *rt.Node, runner.Error) {}
func (terminal) OnInternalError(message string)             { slog.Error("internal error: " + message) }
