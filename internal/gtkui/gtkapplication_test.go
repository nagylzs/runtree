package gtkui

import (
	"os"
	"runtime"
	"testing"
)

// runtree uses the cairo renderer, unless GSK_RENDERER is set, or on macOS
func TestDefaultRenderer(t *testing.T) {
	t.Setenv("GSK_RENDERER", "ngl")
	setDefaultRenderer()
	if r := os.Getenv("GSK_RENDERER"); r != "ngl" {
		t.Errorf("GSK_RENDERER=ngl is changed to %q", r)
	}
	t.Setenv("GSK_RENDERER", "")
	setDefaultRenderer()
	if r, want := os.Getenv("GSK_RENDERER"), rendererFor(runtime.GOOS); r != want {
		t.Errorf("the renderer is %q, want %q", r, want)
	}
	if rendererFor("linux") != "cairo" || rendererFor("darwin") != "" {
		t.Errorf("the renderers: linux %q, darwin %q", rendererFor("linux"), rendererFor("darwin"))
	}
}
