package gtkui

import (
	"math"

	"github.com/diamondburned/gotk4/pkg/cairo"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
)

// newTermMark creates the mark of a run node that has a terminal, shown before its status in the navigator. It is
// drawn, so it does not depend on fonts or icon themes: its size comes from the CSS (.term-mark in main.css, in em, so
// it follows the size of the text), and its color is the color of the text.
func newTermMark() *gtk.DrawingArea {
	da := gtk.NewDrawingArea()
	da.AddCSSClass("term-mark")
	da.SetDrawFunc(func(da *gtk.DrawingArea, cr *cairo.Context, width, height int) {
		c := da.Color()
		cr.SetSourceRGBA(float64(c.Red()), float64(c.Green()), float64(c.Blue()), float64(c.Alpha()))
		drawTermMark(cr, float64(width), float64(height))
	})
	return da
}

// The size of the terminal mark in drawTermMark, before it is scaled
const termMarkWidth, termMarkHeight = 16.0, 13.0

// drawTermMark draws a screen with a prompt (">_") with the source of cr, as large as it fits into width x height, in
// the middle
func drawTermMark(cr *cairo.Context, width, height float64) {
	s := math.Min(width/termMarkWidth, height/termMarkHeight)
	cr.Translate((width-termMarkWidth*s)/2, (height-termMarkHeight*s)/2)
	cr.Scale(s, s)
	cr.SetLineWidth(1.5)
	cr.SetLineCap(cairo.LineCapRound)
	cr.SetLineJoin(cairo.LineJoinRound)

	// the screen: a rectangle with rounded corners, the line inside the size
	const lw, r = 0.75, 2.0
	x0, y0, x1, y1 := lw, lw, termMarkWidth-lw, termMarkHeight-lw
	cr.NewSubPath()
	cr.Arc(x1-r, y0+r, r, -math.Pi/2, 0)
	cr.Arc(x1-r, y1-r, r, 0, math.Pi/2)
	cr.Arc(x0+r, y1-r, r, math.Pi/2, math.Pi)
	cr.Arc(x0+r, y0+r, r, math.Pi, 3*math.Pi/2)
	cr.ClosePath()
	cr.Stroke()

	// the prompt
	cr.MoveTo(4, 4)
	cr.LineTo(6.5, 6.5)
	cr.LineTo(4, 9)
	cr.MoveTo(8, 9)
	cr.LineTo(11.5, 9)
	cr.Stroke()
}
