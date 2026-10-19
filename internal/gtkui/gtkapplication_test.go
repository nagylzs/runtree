package gtkui

import (
	"os"
	"testing"
)

// runtree uses the cairo renderer, unless GSK_RENDERER is set
func TestDefaultRenderer(t *testing.T) {
	t.Setenv("GSK_RENDERER", "ngl")
	setDefaultRenderer()
	if r := os.Getenv("GSK_RENDERER"); r != "ngl" {
		t.Errorf("GSK_RENDERER=ngl is changed to %q", r)
	}
	t.Setenv("GSK_RENDERER", "")
	setDefaultRenderer()
	if r := os.Getenv("GSK_RENDERER"); r != DefaultRenderer {
		t.Errorf("the renderer is %q, want %q", r, DefaultRenderer)
	}
}
