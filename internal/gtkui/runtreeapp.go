package gtkui

import (
	"fmt"
	"log/slog"
	"os/exec"
	"sync"
	"time"

	"github.com/diamondburned/gotk4/pkg/core/glib"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
	"github.com/nagylzs/runtree/internal/rt"
	"github.com/nagylzs/runtree/internal/runner"
)

// RunTreeApp is the main application code that builds the GUI and runs the deps
type RunTreeApp struct {
	overlay *gtk.Overlay

	bash           string
	socat          string
	terminals      map[string]*nodeTerminal // by node id
	currentTerm    *nodeTerminal            // current terminal displayed on the rightSide side of the main pane
	lastWinSize    runner.WinSize           // last known terminal window size
	pane           *MainPaned
	lblSelectANode *gtk.Label
	selected       *rt.Node    // this stores the official currently selected node
	lock           *sync.Mutex // guards all internals, used internally only
	window         *gtk.ApplicationWindow
	closeDialog    *gtk.Window    // close confirmation dialog, when shown; main gtk loop only
	closeConfirmed bool           // the user confirmed closing the main window; main gtk loop only
	warnings       rt.ParseErrors // the warnings of the tree files, shown in the main window
}

func NewRunTreeApp(runTree *rt.Tree) (*RunTreeApp, error) {
	var err error
	var bash, socat string

	bash, err = exec.LookPath("bash")
	if err != nil {
		return nil, fmt.Errorf("could not find bash, please install bash: %w", err)
	}
	socat, err = exec.LookPath("socat")
	if err != nil {
		return nil, fmt.Errorf("could not find socat, please install socat: %w", err)
	}

	terminals := make(map[string]*nodeTerminal)

	app := &RunTreeApp{
		bash:      bash,
		socat:     socat,
		terminals: terminals,
		lock:      &sync.Mutex{},
		lastWinSize: runner.WinSize{
			Rows:   50,
			Cols:   160,
			Width:  0,
			Height: 0,
		},
	}
	runTree.SetTerminalHelper(app)
	return app, nil
}

// Start background processes
func (ra *RunTreeApp) Start() {
	// Start the runtree deps, it will search for runable nodes and start them.
	go RunTree.Run()
	// Update the GUI periodically
	go ra.runGuiUpdater()
	glib.TimeoutAdd(terminalSizeCheckMs, ra.checkTerminalSize)

	go func() {
		for {
			time.Sleep(1 * time.Second)
			RunTree.CheckLock(5 * time.Second)
		}
	}()
}

func (ra *RunTreeApp) runGuiUpdater() {
	start := time.Now()
	c := time.Second * 0
	var cc int64 = 0
	for {
		started := time.Now()
		glib.IdleAdd(ra.pane.UpdateGUI)
		elapsed := time.Since(started)
		c += elapsed
		cc += 1
		if elapsed < time.Second {
			time.Sleep(time.Second)
		}
		if time.Since(start) > 10*time.Second {
			slog.Debug(fmt.Sprintf("%v Avg GUI time=%v, count=%v", time.Now(), time.Duration(int64(c)/cc), cc))
			start, c, cc = time.Now(), 0, 0
		}
	}
}

func (ra *RunTreeApp) GetWinSize() runner.WinSize {
	ra.lock.Lock()
	defer ra.lock.Unlock()
	return ra.lastWinSize
}

// AfterPtyStarted is called with the tree locked, from the scheduler goroutine or from the main gtk loop.
// The terminal is created later, in the main gtk loop: gtk must not be called from other goroutines, and the main
// gtk loop cannot be waited for here, because it may be waiting for the tree lock.
func (ra *RunTreeApp) AfterPtyStarted(n *rt.Node) {
	client, size := n.Client, n.ClientArgs.InitialPtySize
	glib.IdleAdd(func() {
		err := ra.createTerminal(n, client, size)
		if err != nil {
			n.TerminalFailed(err)
		}
	})
}

//// getNodeTerm returns the terminal emulator for the given node. It is thread safe.
//func (ra *RunTreeApp) getNodeTerm(node *rt.Node) *vte.Terminal {
//	if node == nil {
//		return nil
//	}
//	ra.lock.Lock()
//	defer ra.lock.Unlock()
//	return ra.terminals[node.Id]
//}

func (ra *RunTreeApp) ProcStateChanged(n *rt.Node, state runner.ProcState) {
	ra.pane.navigator.UpdateNode(n, "ProcStateChanged")
	// Is the node the selected one?
	ra.lock.Lock()
	sel := ra.selected
	ra.lock.Unlock()
	if sel != n {
		return
	}
	// Yes, re-display
	glib.IdleAdd(func() {
		ra.displaySelectedNode(false)
	})
}

// OnTreeNodeSelected is called from the main gtk loop, when the user selects a gui node.
// also called from OnNodeChanged when a process is started
func (ra *RunTreeApp) OnTreeNodeSelected(node *rt.Node) {
	if node != nil {
		slog.Debug("node selected", "node", node.Id)
	}
	ra.lock.Lock()
	ra.selected = node
	ra.lock.Unlock()

	ra.displaySelectedNode(true)

}

func (ra *RunTreeApp) OnNodeChanged(n *rt.Node, oldStatus rt.Status, oldExpanded bool) {
	ra.pane.navigator.UpdateNode(n, "OnNodeChanged")
}

func (ra *RunTreeApp) OnClientError(locked bool, node *rt.Node, e runner.Error) {
	glib.IdleAdd(func() {
		ShowToast(e.Message)
	})
}

func (ra *RunTreeApp) OnInternalError(message string) {
	glib.IdleAdd(func() {
		ShowToast(message)
	})
}

func (ra *RunTreeApp) Selected() *rt.Node {
	ra.lock.Lock()
	defer ra.lock.Unlock()
	return ra.selected
}

func (ra *RunTreeApp) displaySelectedNode(grabFocus bool) {
	ra.lock.Lock()
	node := ra.selected
	ra.lock.Unlock()

	// Display terminal for the selected node
	var nt *nodeTerminal = nil
	if node != nil {
		ra.lock.Lock()
		nt = ra.terminals[node.Id]
		ra.lock.Unlock()
	}
	if nt != nil {
		ra.pane.SetTerminalWidget(nt.term)
	} else {
		if node == nil {
			ra.lblSelectANode.SetText("Please select a node")
		} else {
			ra.lblSelectANode.SetText("No terminal was started for this node (yet)")
		}
		ra.pane.SetTerminalWidget(ra.lblSelectANode)
	}
	ra.currentTerm = nt
	if nt != nil && grabFocus {
		glib.IdleAdd(func() {
			nt.term.GrabFocus()
		})
	}

	// Display tools for the selected node
	ra.pane.toolBox.SetNode(node)
}
