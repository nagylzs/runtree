package gtkui

import (
	"math"
	"strings"
	"testing"
	"unicode"

	"github.com/diamondburned/gotk4/pkg/cairo"
	"github.com/nagylzs/runtree/internal/signal"
)

// The Signal menu shows the name of the signal on its button (e.g. SIGABRT, not SIGABORT), with a drawn icon instead of
// an emoji, and the button has a color
func TestSignalButtons(t *testing.T) {
	for name, b := range signalButtons {
		if _, ok := signal.Signals[name]; !ok {
			t.Errorf("%s is not a signal", name)
		}
		if b.label != name && !strings.HasSuffix(b.label, " "+name) {
			t.Errorf("the label of %s is %q", name, b.label)
		}
		for _, r := range b.label {
			if r > unicode.MaxASCII {
				t.Errorf("the label of %s is not ASCII, some systems have no font for it (draw an icon): %q",
					name, b.label)
				break
			}
		}
		if !strings.Contains(mainCss, "."+b.class+" {") {
			t.Errorf("main.css has no rule for %s (%s)", b.class, name)
		}
	}
}

// The icons of the signals are drawn in the middle of the area, with their holes (the eyes of the skull, the highlight
// of the bomb) cut out
func TestDrawSignalIcons(t *testing.T) {
	skull := drawnIcon(iconSkull)
	for _, p := range [][2]float64{{8, 3}, {3, 7}, {13, 7}, {8, 13}} {
		if !skull(p[0], p[1]) {
			t.Errorf("the skull is not drawn at %v", p)
		}
	}
	for _, p := range [][2]float64{{5.6, 7.5}, {10.4, 7.5}, {8, 10.5}, {7, 13.5}, {1, 1}} {
		if skull(p[0], p[1]) {
			t.Errorf("the skull is drawn at %v (an eye, the nose, a gap between the teeth, or outside)", p)
		}
	}
	bomb := drawnIcon(iconBomb)
	for _, p := range [][2]float64{{6.3, 11}, {9.6, 6.9}, {13, 3}, {15.5, 3}} {
		if !bomb(p[0], p[1]) {
			t.Errorf("the bomb is not drawn at %v", p)
		}
	}
	if a := 1.25 * math.Pi; bomb(6.3+2.95*math.Cos(a), 10.2+2.95*math.Sin(a)) { // the middle of the highlight
		t.Errorf("the highlight of the bomb is not cut out")
	}
	none := drawnIcon(iconNone)
	for y := 0.0; y < opIconSize; y++ {
		for x := 0.0; x < opIconSize; x++ {
			if none(x, y) {
				t.Fatalf("iconNone is drawn at %v,%v", x, y)
			}
		}
	}
}

// drawnIcon draws an icon in 64x64 pixels, and returns a function that tells if it is drawn at a point of the icon (in
// the 16x16 coordinates of drawOpIcon)
func drawnIcon(icon opIcon) func(x, y float64) bool {
	const size = 64
	s := float64(size) / opIconSize
	surface := cairo.CreateImageSurface(cairo.FormatARGB32, size, size)
	cr := cairo.Create(surface)
	cr.SetSourceRGBA(1, 1, 1, 1)
	drawOpIcon(cr, icon, size, size)
	surface.Flush()
	data, stride := surface.Data(), surface.Stride()
	return func(x, y float64) bool { return data[int(y*s)*stride+int(x*s)*4+3] > 0 } // ARGB32 is BGRA in memory
}
