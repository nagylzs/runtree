package gtkui

import (
	"testing"

	"github.com/nagylzs/runtree/internal/rt"
)

// The lists show the nodes in their status, and the Blocked list the nodes that wait for a resource or a lock
func TestListShows(t *testing.T) {
	for _, tc := range []struct {
		name    string
		node    rt.Node
		mode    NavMode
		onlyRun bool
		want    bool
	}{
		{"waiting", rt.Node{Type: rt.TypeRun, Status: rt.StatusWaiting}, NavModeWaiting, true, true},
		{"waiting, not failed", rt.Node{Type: rt.TypeRun, Status: rt.StatusWaiting}, NavModeFailed, true, false},
		{"a seq with run nodes only", rt.Node{Type: rt.TypeSeq, Status: rt.StatusRunning}, NavModeRunning, true, false},
		{"a seq", rt.Node{Type: rt.TypeSeq, Status: rt.StatusRunning}, NavModeRunning, false, true},
		{"blocked", rt.Node{Type: rt.TypeRun, Status: rt.StatusWaiting, Blocked: true}, NavModeBlocked, true, true},
		{"not blocked", rt.Node{Type: rt.TypeRun, Status: rt.StatusWaiting}, NavModeBlocked, true, false},
		{"blocked and frozen", rt.Node{Type: rt.TypeRun, Status: rt.StatusFrozen, Blocked: true}, NavModeBlocked,
			true, true},
		{"blocked but finished", rt.Node{Type: rt.TypeRun, Status: rt.StatusSuccess, Blocked: true}, NavModeBlocked,
			true, false},
		{"blocked but running", rt.Node{Type: rt.TypeRun, Status: rt.StatusRunning, Blocked: true}, NavModeBlocked,
			true, false},
		{"a running par that waits for a lock", rt.Node{Type: rt.TypePar, Status: rt.StatusRunning, Blocked: true},
			NavModeBlocked, false, true},
		{"a blocked waiting node is waiting too", rt.Node{Type: rt.TypeRun, Status: rt.StatusWaiting, Blocked: true},
			NavModeWaiting, true, true},
	} {
		if got := listShows(tc.mode, &tc.node, tc.onlyRun); got != tc.want {
			t.Errorf("%s: %v", tc.name, got)
		}
	}
	if NavModeName(NavModeBlocked) != "Blocked" || NavModeName(NavModeTree) != "Tree" ||
		NavModeName(NavModePaused) != "Paused" {
		t.Error("the names of the modes")
	}
}

// The rows fill the visible width of the navigator (it does not scroll sideways over empty rows); before the
// navigator is shown they have a default width
func TestRowWidth(t *testing.T) {
	for _, tc := range []struct {
		visible, x float64
		want       int
	}{
		{440, 0, 440},
		{440, 3 * levelIndent, 380},
		{0, 0, navigatorRowWidth},
		{0, 2 * levelIndent, navigatorRowWidth},
		{30, 2 * levelIndent, 1}, // deeper than the navigator is wide: place widens it to its minimum width
	} {
		if got := rowWidth(tc.visible, tc.x); got != tc.want {
			t.Errorf("rowWidth(%v, %v) = %d, want %d", tc.visible, tc.x, got, tc.want)
		}
	}
}
