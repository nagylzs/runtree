package rt

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"time"

	"github.com/nagylzs/runtree/internal/runner"
)

// Start calculates command arguments, environment runner etc. from variables and starts the command on the runner.
// Connecting to the runner and starting the process happens in the background (see connect), without the tree lock:
// it can take a while (e.g. an SSH login). Meanwhile the node is starting: its status is running, without a client.
// A failure to connect or start is a start error of the node, logged as a start-failed event.
func (n *Node) Start(locked bool) error {
	if !locked {
		panic("must be called in locked state")
	}

	if n.Type != TypeRun {
		panic("Node.Start: cannot start a non-run node")
	}

	defer n.updateStatusesUp(true)

	// The status of a started node comes from its process (or its start error), not from a previous manual status
	// (e.g. manually waiting, or success when it is run again).
	n.WantManualStatus = false

	n.calcClientArgs()
	n.Starts++
	logger := n.log()
	on := ""
	if n.Calculated.Runner != runner.LocalAddress {
		on = " on " + n.Calculated.Runner
	}
	logger.Info(fmt.Sprintf("starting%s: %s", on, n.commandLine(true)),
		attrEvent, EventStart, "start", n.Starts, "runner", n.Calculated.Runner, "args", n.Calculated.Args,
		"cwd", n.Calculated.CWD)

	var output io.Writer
	if r := n.Tree.outputRecorder; r != nil {
		output = r.NodeOutput(n, n.Starts)
	}
	n.Client = nil // the client of the previous start, its process has exited
	n.StartError = ""
	n.TimedOut = false
	n.starting = true // running from now on, see calcRunStatus
	go n.connect(n.Calculated.Runner, *n.ClientArgs, logger, output)
	return nil
}

// connect connects to the runner and starts the process of the node, see Start. It is called in unlocked state.
func (n *Node) connect(address string, args runner.CmdArgs, logger *slog.Logger, output io.Writer) {
	defer n.Tree.recoverPanic("start of " + n.Id)
	ctx := context.Background()
	c, err := runner.NewClient(ctx, address, logger, n.onError)
	if err == nil {
		if output != nil {
			c.SetOutput(output)
		}
		if n.Tree.unattended {
			c.SetNoTerminal()
		}
		if err = c.StartPty(ctx, args, n.procStateChanged); err != nil {
			c.Close()
		}
	}
	// the client closes the output at its end, but it was not started
	if cl, ok := output.(io.Closer); ok && err != nil {
		_ = cl.Close()
	}

	n.LockTree("Node.connect")
	defer n.UnLockTree()
	n.starting = false
	if err != nil {
		n.startFailed(err.Error())
	} else {
		n.Client = c
		n.Tree.terminalHelper.AfterPtyStarted(n)
	}
	n.updateStatusesUp(true)
}

// Starting tells if the node is connecting to its runner to start its process, see Start
func (n *Node) Starting(locked bool) bool {
	if !locked {
		panic("must be called in locked state")
	}
	return n.starting
}

// TerminalFailed must be called by the TerminalHelper, in unlocked state, when it could not connect the pty of a
// started node to a terminal. It makes the node fail, and closes the client, so the runner terminates the process.
func (n *Node) TerminalFailed(err error) {
	n.LockTree("Node.TerminalFailed")
	n.startFailed(fmt.Sprintf("could not connect terminal: %s", err.Error()))
	c := n.Client
	n.UnLockTree()
	// Closing the client calls procStateChanged, that locks the tree and updates the status.
	if c != nil {
		c.Close()
	}
}

// startFailed sets the start error, and logs it
func (n *Node) startFailed(msg string) {
	n.StartError = msg
	n.log().Error("failed to start: "+msg, attrEvent, EventStartFailed, "start", n.Starts, "error", msg)
}

func (n *Node) procStateChanged(state runner.ProcState) {
	defer n.Tree.recoverPanic("process state change of " + n.Id)
	n.LockTree("Node.procStateChanged")
	defer n.UnLockTree()
	n.updateStatusesUp(true)
	if n.Tree.terminalHelper != nil {
		n.Tree.terminalHelper.ProcStateChanged(n, state)
	}
}

// CalcClientArgs calculates and set ClientArgs
func (n *Node) calcClientArgs() {
	cEnvs := make([]string, 0)
	for k, v := range n.Calculated.Envs {
		cEnvs = append(cEnvs, fmt.Sprintf("%s=%s", k, v))
	}
	args := n.Calculated.Args
	n.ClientArgs = &runner.CmdArgs{
		Name:           args[0],
		Args:           args,
		Env:            cEnvs,
		InheritSysEnvs: n.SystemEnvs,
		Cwd:            n.Calculated.CWD,
		InitialPtySize: n.Tree.terminalHelper.GetWinSize(), // TODO: looks nasty
	}
}

// IncProc increases Node.NProc for the node, and all of its parents. updateStatus calls it when a run node starts
// running, and DecProc when it stops.
func (n *Node) IncProc(locked bool) {
	if !locked {
		panic("node must be locked")
	}
	for n != nil {
		n.NProc += 1
		n = n.Parent
	}
}

// DecProc decreases Node.NProc for the node, and all of its parents.
func (n *Node) DecProc(locked bool) {
	if !locked {
		panic("node must be locked")
	}
	for n != nil {
		n.NProc -= 1
		n = n.Parent
	}
}

// logExit logs the exit of the process of the node. It is called by updateStatus when a running node stops running,
// before the status change is logged. (The process state may be seen by the scheduler before procStateChanged.)
func (n *Node) logExit(state runner.ProcState) {
	msg := "exited"
	if state.Exited && state.ExitCode >= 0 {
		msg = "exited with code " + ExitCodeText(state.ExitCode)
	} else if state.Error != "" {
		msg = "exited: " + state.Error // killed by a signal, or the connection to the runner is lost
	}
	var elapsed time.Duration
	if !n.Started.IsZero() {
		elapsed = time.Since(n.Started)
		msg += " after " + formatDuration(elapsed)
	}
	level := slog.LevelInfo
	if !n.ExitedSuccessfully(state) {
		level = slog.LevelWarn
	}
	attrs := []any{attrEvent, EventExited, "start", n.Starts, "pid", state.Pid, "exit_code", state.ExitCode,
		"elapsed", elapsed, "user_time", state.UserTime, "system_time", state.SystemTime}
	if state.Error != "" {
		attrs = append(attrs, "error", state.Error)
	}
	n.log().Log(context.Background(), level, msg, attrs...)
}

// SendSignal sends a signal (e.g. "SIGINT") to the process of a run node, when it is running. It tells if the signal
// was sent.
func (n *Node) SendSignal(locked bool, name string) (bool, error) {
	if !locked {
		panic("must be called in locked state")
	}
	c := n.Client
	if c == nil {
		return false, nil
	}
	if ps := c.LastState(); !ps.Alive() {
		return false, nil
	}
	n.log().Info("sending "+name, attrEvent, EventSignal, "signal", name)
	if err := c.SendSignal(context.Background(), name, true); err != nil {
		n.log().Warn(fmt.Sprintf("could not send %s: %s", name, err), attrEvent, EventSignal, "signal", name,
			"error", err)
		return true, err
	}
	return true, nil
}

func (n *Node) onError(e runner.Error) {
	defer n.Tree.recoverPanic("runner error of " + n.Id)
	level := slog.LevelError
	if e.Code == runner.ErrorSignalNotSupported {
		level = slog.LevelWarn // e.g. SIGTERM on Windows: a timeout or the exit of runtree sends SIGKILL at once then
	}
	n.log().Log(context.Background(), level, e.Message, attrEvent, EventRunnerError, "code", e.Code)
	n.RLockTree("Node.onError")
	defer n.RUnlockTree()
	th := n.Tree.terminalHelper
	if th != nil {
		th.OnClientError(true, n, e)
	}
}
