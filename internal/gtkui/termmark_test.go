package gtkui

import (
	"math"
	"testing"

	"github.com/diamondburned/gotk4/pkg/cairo"
)

// The terminal mark is drawn as large as it fits, in the middle of the area (also when the area is wider or taller
// than the mark), and it is an outline: the screen is empty apart from the prompt.
func TestDrawTermMark(t *testing.T) {
	for _, size := range [][2]int{{20, 16}, {64, 26}, {16, 40}, {160, 130}} {
		w, h := size[0], size[1]
		surface := cairo.CreateImageSurface(cairo.FormatARGB32, w, h)
		cr := cairo.Create(surface)
		cr.SetSourceRGBA(1, 1, 1, 1)
		drawTermMark(cr, float64(w), float64(h))
		surface.Flush()
		data, stride := surface.Data(), surface.Stride()
		drawn := func(x, y int) bool { return data[y*stride+x*4+3] > 0 } // ARGB32 is BGRA in memory

		// the bounds of the drawn pixels
		minX, minY, maxX, maxY := w, h, -1, -1
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				if drawn(x, y) {
					minX, minY = min(minX, x), min(minY, y)
					maxX, maxY = max(maxX, x), max(maxY, y)
				}
			}
		}
		if maxX < 0 {
			t.Errorf("%dx%d: nothing is drawn", w, h)
			continue
		}
		s := math.Min(float64(w)/termMarkWidth, float64(h)/termMarkHeight)
		wantW, wantH := termMarkWidth*s, termMarkHeight*s
		gotW, gotH := float64(maxX-minX+1), float64(maxY-minY+1)
		if math.Abs(gotW-wantW) > 1.5 || math.Abs(gotH-wantH) > 1.5 {
			t.Errorf("%dx%d: the mark is %.0fx%.0f, want %.1fx%.1f", w, h, gotW, gotH, wantW, wantH)
		}
		left, right, top, bottom := minX, w-1-maxX, minY, h-1-maxY
		if abs(left-right) > 1 || abs(top-bottom) > 1 {
			t.Errorf("%dx%d: not in the middle: left %d, right %d, top %d, bottom %d", w, h, left, right, top,
				bottom)
		}
		// inside the screen, right of the prompt line, there is nothing
		x0, y0 := float64(w)/2-wantW/2, float64(h)/2-wantH/2
		if x, y := int(x0+13*s), int(y0+4.5*s); drawn(x, y) {
			t.Errorf("%dx%d: the screen is not empty at %d,%d", w, h, x, y)
		}
		// the prompt is drawn: the tip of ">"
		if x, y := int(x0+6.5*s), int(y0+6.5*s); s >= 2 && !drawn(x, y) {
			t.Errorf("%dx%d: the prompt is not drawn at %d,%d", w, h, x, y)
		}
	}
}

func abs(i int) int {
	if i < 0 {
		return -i
	}
	return i
}
