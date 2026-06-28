package rt

import (
	"context"
	"fmt"
	"log/slog"

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

	// Once we start a node that was manually waiting, we do not want it to have manual status anymore
	if n.WantManualStatus && n.ManualStatus == StatusWaiting {
		n.WantManualStatus = false
	}

	n.calcClientArgs()
	logger := slog.With("node", n.Id)
	ctx := context.Background()

	c, err := runner.NewClient(ctx, n.Calculated.Runner, logger, n.onError)
	if err != nil {
		n.StartError = err.Error()
		logger.Error("failed to start", "error", n.StartError)
		n.updateStatusesUp(true)
		return fmt.Errorf("failed to start: %w", err)
	}
	err = c.StartPty(ctx, *n.ClientArgs, n.procStateChanged)
	if err != nil {
		n.StartError = err.Error()
		logger.Error("failed to start", "error", n.StartError)
		return fmt.Errorf("failed to start: %w", err)
	}

	n.Client = c
	err = n.Tree.terminalHelper.AfterPtyStarted(n)
	if err != nil {
		logger.Error("failed to start", "error", n.StartError)
		c.Close()
		return fmt.Errorf("failed to start: %w", err)
	}
	n.IncProc(true)
	return nil
}

func (n *Node) procStateChanged(state runner.ProcState) {
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

func (n *Node) onError(e runner.Error) {
	n.RLockTree("Node.onError")
	defer n.RUnlockTree()
	th := n.Tree.terminalHelper
	if th != nil {
		if th.OnClientError != nil {
			th.OnClientError(true, n, e)
		}
	}
}
