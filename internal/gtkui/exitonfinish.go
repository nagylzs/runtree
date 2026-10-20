package gtkui

import (
	"fmt"
	"strings"

	"github.com/diamondburned/gotk4/pkg/core/glib"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
	"github.com/nagylzs/runtree/internal/rt"
)

// exitDelay is the number of seconds between the end of the tree and the exit of runtree with --exit-on-finish: the
// people who watch the window see the result, and they can stay
const exitDelay = 10

// exitOnFinish: runtree exits when the root node is finished, see SetExitOnFinish
var exitOnFinish bool

// SetExitOnFinish makes runtree exit when the root node is finished (runtree --exit-on-finish), exitDelay seconds
// after it, unless the user stays. It must be called before RunApplication.
func SetExitOnFinish() {
	exitOnFinish = true
}

// countdown is the state of the exit countdown (without the GUI, see finishBanner)
type countdown struct {
	left     int // the seconds left, while counting
	counting bool
	stay     bool // the user stays: no countdown anymore
}

// tick is called every second, it tells if the root node is finished. It returns the seconds left until the exit (-1:
// no countdown), and if it is time to exit. The countdown stops when the root is started again (e.g. Re-run).
func (c *countdown) tick(finished bool) (left int, exit bool) {
	switch {
	case c.stay || !finished:
		c.counting = false
		return -1, false
	case !c.counting:
		c.counting, c.left = true, exitDelay
	default:
		c.left--
	}
	return c.left, c.left <= 0
}

// finishBanner shows the countdown above the main pane, with a button to stay
type finishBanner struct {
	*gtk.Box
	label       *gtk.Label
	statusClass string
	countdown   countdown
}

// newFinishBanner creates the banner, it is hidden until the root node is finished. It must be called from the main
// gtk loop.
func newFinishBanner() *finishBanner {
	b := &finishBanner{Box: gtk.NewBox(gtk.OrientationHorizontal, 12), label: gtk.NewLabel("")}
	b.label.SetHExpand(true)
	b.label.SetXAlign(0)
	btnStay := gtk.NewButtonWithLabel("Stay open")
	btnStay.SetTooltipText("Do not exit (runtree --exit-on-finish)")
	btnStay.ConnectClicked(func() {
		b.countdown.stay = true
		b.SetVisible(false)
	})
	b.AddCSSClass("finish-banner")
	b.Append(b.label)
	b.Append(btnStay)
	b.SetVisible(false)
	return b
}

// start updates the banner every second, and closes the main window when the countdown ends. It must be called from
// the main gtk loop.
func (b *finishBanner) start() {
	glib.TimeoutAdd(1000, func() bool {
		defer recoverPanic("exit on finish")
		RunTree.RLock("finishBanner")
		status := RunTree.Root.Status
		RunTree.RUnlock()
		left, exit := b.countdown.tick(rt.StatusFinished(status))
		if left < 0 {
			b.SetVisible(false)
			return true
		}
		if class := "status-" + strings.ToLower(rt.StatusName(status)); class != b.statusClass {
			if b.statusClass != "" {
				b.RemoveCSSClass(b.statusClass)
			}
			b.AddCSSClass(class)
			b.statusClass = class
		}
		b.label.SetText(fmt.Sprintf("The tree finished: %s. runtree exits in %d seconds.", rt.StatusName(status),
			left))
		b.SetVisible(true)
		if exit {
			// The root is finished, nothing runs: no confirmation (it would ask when a node failed), the countdown
			// was the chance to stay.
			Application.closeConfirmed = true
			Application.window.Close()
			return false
		}
		return true
	})
}
