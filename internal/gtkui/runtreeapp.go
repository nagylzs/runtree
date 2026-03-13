package gtkui

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"sync"
	"time"

	"github.com/diamondburned/gotk4/pkg/core/glib"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
	vte "github.com/nagylzs/gotk4-vte"
	"github.com/nagylzs/runtree/internal/rt"
	"github.com/nagylzs/runtree/internal/runner"
)

// RunTreeApp is the main application code that builds the GUI and runs the deps
type RunTreeApp struct {
	overlay *gtk.Overlay

	bash           string
	socat          string
	terminals      map[string]*vte.Terminal
	currentTerm    *vte.Terminal  // current terminal displayed on the rightSide side of the main pane
	lastWinSize    runner.WinSize // last known terminal window size
	pane           *MainPaned
	lblSelectANode *gtk.Label
	selected       *rt.Node    // this stores the official currently selected node
	lock           *sync.Mutex // guards all internals, used internally only
	window         *gtk.ApplicationWindow
}

func NewRunTreeApp(runTree *rt.Tree) (*RunTreeApp, error) {
	var err error
	var bash, socat string

	bash, err = exec.LookPath("bash")
	if err != nil {
		return nil, fmt.Errorf("could not find bash, please install bash: %w", err.Error())
	}
	socat, err = exec.LookPath("socat")
	if err != nil {
		return nil, fmt.Errorf("could not find socat, please install socat: %w", err.Error())
	}

	terminals := make(map[string]*vte.Terminal)

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

	go func() {
		for {
			time.Sleep(1 * time.Second)
			RunTree.PrintWho()
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

func (ra *RunTreeApp) AfterPtyStarted(n *rt.Node) error {
	term, err := vte.TerminalNew()
	if err != nil {
		return err
	}

	// TODO: set shortcuts, at least Ctrl+Shift+C and Ctrl+Shift+V to copy/paste

	// Send size changes to pty master.
	term.ConnectStateFlagsChanged(func(state gtk.StateFlags) {
		// if the terminal is not mapped (displayed) then we won't send terminal size changes.
		if !term.Mapped() {
			return
		}
		cols, rows := term.GetColumnCount(), term.GetRowCount()
		width, height := uint16(term.Width()), uint16(term.Height())
		size := runner.WinSize{
			Cols:   cols,
			Rows:   rows,
			Width:  width,
			Height: height,
		}
		// slog.Info("TermSizeChange", "size", size)

		// Save the last known size so that new terminals can start with this size.
		ra.lock.Lock()
		ra.lastWinSize = size
		ra.lock.Unlock()

		c := n.Client
		go func() {
			// TODO: use some kind of timeout here?
			// If we cannot send the size then it is maybe because the process already exited, but the pty
			// was closed on the server before the "process state changed" message arrived to the client
			// Since these happen on different computers in parallel, it is not clear how could we distinguish
			// between a real write error and a "write error after remote process exited". We just hope that
			// if there is a write error, then the process exits (soon), and the server will send the correct
			// exit code.

			ls := c.LastState()
			if ls.Alive() {
				err := c.SendSize(context.TODO(), size, true)
				if err != nil {
					slog.Error(fmt.Sprintf("could not send terminal resize: %s", err.Error()))
				}
			}
		}()
	})

	ra.lock.Lock()
	ra.terminals[n.Id] = term
	ra.lock.Unlock()

	wd, _ := os.Getwd()
	cmd := []string{ra.bash, "-c", ra.socat + " -,raw,echo=0 UNIX-CONNECT:" + n.Client.PtyPath}
	err = term.SpawnAsyncSimple(wd, cmd, os.Environ())

	return err
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
		println(fmt.Sprintf("%s selected", node.Id))
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
	var term *vte.Terminal = nil
	if node != nil {
		term = ra.terminals[node.Id]
	}
	if term != nil {
		ra.pane.SetTerminalWidget(term)
	} else {
		if node == nil {
			ra.lblSelectANode.SetText("Please select a node")
		} else {
			ra.lblSelectANode.SetText("No terminal was started for this node (yet)")
		}
		ra.pane.SetTerminalWidget(ra.lblSelectANode)
	}
	ra.currentTerm = term
	if term != nil && grabFocus {
		glib.IdleAdd(func() {
			term.GrabFocus()
		})
	}

	// Display tools for the selected node
	ra.pane.toolBox.SetNode(node)
}
