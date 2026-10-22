package gtkui

import (
	"strings"
	"testing"

	"github.com/nagylzs/runtree/internal/rt"
)

// The view switcher counts the nodes of the lists like the lists show them: with "Run nodes only", a failed par is
// not counted (its failed run node is)
func TestNavModeCounts(t *testing.T) {
	nodes := []*rt.Node{
		{Type: rt.TypePar, Status: rt.StatusFailed},
		{Type: rt.TypeRun, Status: rt.StatusFailed},
		{Type: rt.TypeRun, Status: rt.StatusFailed},
		{Type: rt.TypeRun, Status: rt.StatusPaused},
		{Type: rt.TypeRun, Status: rt.StatusRunning},
		{Type: rt.TypeRun, Status: rt.StatusWaiting, Blocked: true},
	}
	counts := navModeCounts(nodes, true)
	for mode, want := range map[NavMode]int{NavModeTree: 6, NavModeFailed: 2, NavModePaused: 1, NavModeRunning: 1,
		NavModeWaiting: 1, NavModeBlocked: 1, NavModeSuccess: 0} {
		if counts[mode] != want {
			t.Errorf("run nodes only: %s: %d, want %d", NavModeName(mode), counts[mode], want)
		}
	}
	if got := navModeCounts(nodes, false)[NavModeFailed]; got != 3 {
		t.Errorf("all nodes: Failed: %d, want 3", got)
	}
}

// The buttons of the failed and the paused nodes tell what they show, and they have the color of the status
func TestAttentionButtons(t *testing.T) {
	for _, tc := range []struct {
		mode    NavMode
		count   int
		onlyRun bool
		want    string
	}{
		{NavModeFailed, 3, true, "3 failed run nodes: show them"},
		{NavModeFailed, 1, true, "1 failed run node: show it"},
		{NavModePaused, 1, false, "1 paused node: show it"},
		{NavModePaused, 2, false, "2 paused nodes: show them"},
	} {
		if got := attentionTooltip(tc.mode, tc.count, tc.onlyRun); got != tc.want {
			t.Errorf("attentionTooltip(%s, %d, %v) = %q, want %q", NavModeName(tc.mode), tc.count, tc.onlyRun, got,
				tc.want)
		}
	}
	for _, mode := range attentionModes {
		if class := ".status-" + strings.ToLower(NavModeName(mode)) + " {"; !strings.Contains(mainCss, class) {
			t.Errorf("main.css has no %s", class)
		}
	}
	if !strings.Contains(mainCss, "button.attention {") {
		t.Error("main.css has no rule for the attention buttons")
	}

	pause := drawnIcon(iconPause)
	for _, p := range [][2]float64{{5, 8}, {11, 8}} {
		if !pause(p[0], p[1]) {
			t.Errorf("the pause icon is not drawn at %v", p)
		}
	}
	if pause(8, 8) {
		t.Error("the pause icon has no gap between its bars")
	}
}
