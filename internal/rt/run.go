package rt

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/nagylzs/runtree/internal/runner"
)

// Start calculates command arguments, environment runner etc. from variables and starts the command on the runner
// It can return an error, but only so that the caller can display it to the user.
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
	logger.Info(fmt.Sprintf("starting%s: %s", on, quoteArgs(n.Calculated.Args)),
		attrEvent, EventStart, "start", n.Starts, "runner", n.Calculated.Runner, "args", n.Calculated.Args,
		"cwd", n.Calculated.CWD)
	ctx := context.Background()

	c, err := runner.NewClient(ctx, n.Calculated.Runner, logger, n.onError)
	if err != nil {
		n.startFailed(err.Error())
		n.updateStatusesUp(true)
		return fmt.Errorf("failed to start: %w", err)
	}
	if r := n.Tree.outputRecorder; r != nil {
		if w := r.NodeOutput(n, n.Starts); w != nil {
			c.SetOutput(w)
		}
	}
	err = c.StartPty(ctx, *n.ClientArgs, n.procStateChanged)
	if err != nil {
		n.startFailed(err.Error())
		return fmt.Errorf("failed to start: %w", err)
	}

	n.Client = c
	n.Tree.terminalHelper.AfterPtyStarted(n)
	n.IncProc(true)
	return nil
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
	// process exited, node's old state is alive -> will decrease the number of processes
	if !state.Alive() && StatusActive(n.Status) {
		n.DecProc(true)
	}
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

// IncProc increases Node.NProc for the node, and all of its parents.
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
		msg = fmt.Sprintf("exited with code %d", state.ExitCode)
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
	n.log().Error(e.Message, attrEvent, EventRunnerError, "code", e.Code)
	n.RLockTree("Node.onError")
	defer n.RUnlockTree()
	th := n.Tree.terminalHelper
	if th != nil {
		th.OnClientError(true, n, e)
	}
}
