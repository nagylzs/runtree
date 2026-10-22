package gtkui

import (
	"math"

	"github.com/diamondburned/gotk4/pkg/cairo"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
	"github.com/nagylzs/runtree/internal/rt"
)

// opIcon is an icon of the operation buttons and of some signals. They are drawn (like the terminal mark, see
// termmark.go), so they do not depend on fonts or icon themes (macOS has no Adwaita icons, some systems have no emoji
// fonts): their size comes from the CSS (.op-icon in main.css, in em), and their color is the color of the text, also
// when the button is insensitive.
type opIcon int

const (
	iconNone opIcon = iota // nothing is drawn
	iconRun
	iconRerun
	iconFreeze
	iconThaw
	iconFail
	iconSignal
	iconSkip
	iconSuccess
	iconReset
	iconAll
	iconCopy
	iconMenu
	iconSkull // SIGKILL
	iconBomb  // SIGABRT
	iconPause // the paused nodes (mainpaned.go)
)

// opIconOf returns the icon of an operation; rerun is for Run on a finished node
func opIconOf(op rt.NodeOp, rerun bool) opIcon {
	switch op {
	case rt.OpRun:
		if rerun {
			return iconRerun
		}
		return iconRun
	case rt.OpFreeze:
		return iconFreeze
	case rt.OpThaw:
		return iconThaw
	case rt.OpFail:
		return iconFail
	case rt.OpSignal:
		return iconSignal
	case rt.OpSkip:
		return iconSkip
	case rt.OpSuccess:
		return iconSuccess
	default:
		return iconReset
	}
}

// opIconView shows an opIcon, that can be changed (Run and Re-run)
type opIconView struct {
	*gtk.DrawingArea
	icon opIcon
}

func newOpIconView(icon opIcon) *opIconView {
	v := &opIconView{DrawingArea: gtk.NewDrawingArea(), icon: icon}
	v.AddCSSClass("op-icon")
	v.SetDrawFunc(func(da *gtk.DrawingArea, cr *cairo.Context, width, height int) {
		c := da.Color()
		cr.SetSourceRGBA(float64(c.Red()), float64(c.Green()), float64(c.Blue()), float64(c.Alpha()))
		drawOpIcon(cr, v.icon, float64(width), float64(height))
	})
	return v
}

// setIcon changes the icon, when it is different
func (v *opIconView) setIcon(icon opIcon) {
	if v.icon != icon {
		v.icon = icon
		v.QueueDraw()
	}
}

// opIconSize is the size of the icons in drawOpIcon, before they are scaled
const opIconSize = 16.0

// drawOpIcon draws an icon with the source of cr, as large as it fits into width x height, in the middle
func drawOpIcon(cr *cairo.Context, icon opIcon, width, height float64) {
	s := math.Min(width, height) / opIconSize
	cr.Translate((width-opIconSize*s)/2, (height-opIconSize*s)/2)
	cr.Scale(s, s)
	cr.SetLineWidth(1.6)
	cr.SetLineCap(cairo.LineCapRound)
	cr.SetLineJoin(cairo.LineJoinRound)

	switch icon {
	case iconRun: // a triangle pointing right
		cr.MoveTo(4.5, 2.5)
		cr.LineTo(13, 8)
		cr.LineTo(4.5, 13.5)
		cr.ClosePath()
		cr.Fill()
	case iconRerun: // a circle arrow, clockwise (also SIGHUP: reload)
		cr.Arc(8, 8, 5, -math.Pi/2+0.5, 2*math.Pi-math.Pi/2-0.2)
		cr.Stroke()
		arrowHead(cr, 8+5*math.Cos(-math.Pi/2+0.5), 8+5*math.Sin(-math.Pi/2+0.5), -math.Pi/2+0.5-math.Pi/2)
	case iconFreeze: // a snowflake: three lines, with small branches at their ends
		for i := 0; i < 6; i++ {
			a := float64(i) * math.Pi / 3
			cx, cy := 8+6*math.Cos(a), 8+6*math.Sin(a)
			cr.MoveTo(8, 8)
			cr.LineTo(cx, cy)
			bx, by := 8+3.8*math.Cos(a), 8+3.8*math.Sin(a)
			for _, d := range []float64{-0.7, 0.7} {
				cr.MoveTo(bx, by)
				cr.LineTo(bx+2.2*math.Cos(a+d), by+2.2*math.Sin(a+d))
			}
		}
		cr.Stroke()
	case iconThaw: // a sun
		cr.Arc(8, 8, 2.8, 0, 2*math.Pi)
		cr.Stroke()
		for i := 0; i < 8; i++ {
			a := float64(i) * math.Pi / 4
			cr.MoveTo(8+4.8*math.Cos(a), 8+4.8*math.Sin(a))
			cr.LineTo(8+6.6*math.Cos(a), 8+6.6*math.Sin(a))
		}
		cr.Stroke()
	case iconFail: // an X
		cr.MoveTo(3.5, 3.5)
		cr.LineTo(12.5, 12.5)
		cr.MoveTo(12.5, 3.5)
		cr.LineTo(3.5, 12.5)
		cr.Stroke()
	case iconSignal: // a lightning bolt
		cr.MoveTo(9.5, 1.5)
		cr.LineTo(3.5, 9)
		cr.LineTo(7.5, 9)
		cr.LineTo(6.5, 14.5)
		cr.LineTo(12.5, 7)
		cr.LineTo(8.5, 7)
		cr.ClosePath()
		cr.Fill()
	case iconSkip: // a triangle pointing right, and a bar
		cr.MoveTo(3, 3)
		cr.LineTo(10, 8)
		cr.LineTo(3, 13)
		cr.ClosePath()
		cr.Fill()
		cr.MoveTo(12.5, 3)
		cr.LineTo(12.5, 13)
		cr.Stroke()
	case iconSuccess: // a check mark
		cr.MoveTo(2.5, 8.5)
		cr.LineTo(6.5, 12.5)
		cr.LineTo(13.5, 3.5)
		cr.Stroke()
	case iconReset: // an arrow that turns back (undo)
		cr.MoveTo(4, 6)
		cr.LineTo(10, 6)
		cr.Arc(10, 9.5, 3.5, -math.Pi/2, math.Pi/2)
		cr.LineTo(6, 13)
		cr.Stroke()
		arrowHead(cr, 3.5, 6, math.Pi)
	case iconCopy: // two sheets
		roundedRect(cr, 5.5, 5.5, 8, 8.5, 1.5)
		cr.Stroke()
		cr.MoveTo(3, 10.5)
		cr.LineTo(2.5, 10.5)
		cr.LineTo(2.5, 2.5)
		cr.LineTo(10.5, 2.5)
		cr.LineTo(10.5, 3)
		cr.Stroke()
	case iconMenu: // three lines
		for _, y := range []float64{4, 8, 12} {
			cr.MoveTo(2.5, y)
			cr.LineTo(13.5, y)
		}
		cr.Stroke()
	case iconAll: // a tree: a trunk with three branches
		cr.MoveTo(4, 2.5)
		cr.LineTo(4, 12.5)
		for _, y := range []float64{4.5, 8.5, 12.5} {
			cr.MoveTo(4, y)
			cr.LineTo(7, y)
		}
		cr.Stroke()
		for _, y := range []float64{4.5, 8.5, 12.5} {
			cr.Rectangle(8.5, y-1.5, 5, 3)
		}
		cr.Fill()
	case iconPause: // two bars
		roundedRect(cr, 3.5, 2.5, 3, 11, 0.8)
		roundedRect(cr, 9.5, 2.5, 3, 11, 0.8)
		cr.Fill()
	case iconSkull: // a skull: the cranium and the jaw, with the eyes, the nose and the teeth cut out
		cr.Arc(8, 7, 6, 0.75*math.Pi, 2.25*math.Pi)
		cr.LineTo(11, 12)
		cr.LineTo(11, 14.5)
		cr.LineTo(5, 14.5)
		cr.LineTo(5, 12)
		cr.ClosePath()
		for _, x := range []float64{5.6, 10.4} {
			cr.NewSubPath()
			cr.Arc(x, 7.5, 1.7, 0, 2*math.Pi)
		}
		cr.MoveTo(8, 9.3)
		cr.LineTo(8.9, 11)
		cr.LineTo(7.1, 11)
		cr.ClosePath()
		for _, x := range []float64{6.6, 8.6} {
			cr.Rectangle(x, 12.6, 0.8, 1.9)
		}
		cr.SetFillRule(cairo.FillRuleEvenOdd)
		cr.Fill()
	case iconBomb: // a bomb: a ball with a highlight, a cap, a fuse and a spark
		cr.Arc(6.3, 10.2, 4.8, 0, 2*math.Pi)
		cr.NewSubPath() // the highlight, cut out
		cr.Arc(6.3, 10.2, 3.4, 1.1*math.Pi, 1.4*math.Pi)
		cr.ArcNegative(6.3, 10.2, 2.5, 1.4*math.Pi, 1.1*math.Pi)
		cr.ClosePath()
		cr.SetFillRule(cairo.FillRuleEvenOdd)
		cr.Fill()
		cr.SetLineWidth(3.2)
		cr.SetLineCap(cairo.LineCapButt)
		cr.MoveTo(9, 7.5)
		cr.LineTo(10.1, 6.4)
		cr.Stroke()
		cr.SetLineWidth(1.2)
		cr.SetLineCap(cairo.LineCapRound)
		cr.MoveTo(10.1, 6.4)
		cr.CurveTo(10.6, 5.6, 11, 5, 11.6, 4.6)
		cr.Stroke()
		for i := 0; i < 16; i++ { // the spark: a star with 8 points, the diagonal ones are shorter
			a := float64(i) * math.Pi / 8
			r := 0.9
			switch {
			case i%4 == 0:
				r = 2.9
			case i%2 == 0:
				r = 2
			}
			cr.LineTo(13+r*math.Cos(a), 3+r*math.Sin(a))
		}
		cr.ClosePath()
		cr.Fill()
	}
}

// roundedRect adds a rectangle with rounded corners to the path
func roundedRect(cr *cairo.Context, x, y, w, h, r float64) {
	cr.NewSubPath()
	cr.Arc(x+w-r, y+r, r, -math.Pi/2, 0)
	cr.Arc(x+w-r, y+h-r, r, 0, math.Pi/2)
	cr.Arc(x+r, y+h-r, r, math.Pi/2, math.Pi)
	cr.Arc(x+r, y+r, r, math.Pi, 3*math.Pi/2)
	cr.ClosePath()
}

// arrowHead draws a filled arrow head with its tip at x, y, pointing to the angle a
func arrowHead(cr *cairo.Context, x, y, a float64) {
	const length, spread = 3.5, 0.55
	cr.MoveTo(x, y)
	cr.LineTo(x-length*math.Cos(a-spread), y-length*math.Sin(a-spread))
	cr.LineTo(x-length*math.Cos(a+spread), y-length*math.Sin(a+spread))
	cr.ClosePath()
	cr.Fill()
}
