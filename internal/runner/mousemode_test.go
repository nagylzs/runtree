package runner

import (
	"strings"
	"testing"
)

// The mouse modes are followed in the output, also when a sequence is split between writes
func TestMouseModeTracker(t *testing.T) {
	for _, tc := range []struct {
		name   string
		writes []string
		want   bool
	}{
		{"nothing", []string{"hello\r\n"}, false},
		{"normal tracking", []string{"\x1b[?1000h"}, true},
		{"with SGR coordinates", []string{"\x1b[?1000;1006h"}, true},
		{"any event (vim mouse=a)", []string{"\x1b[?1003h"}, true},
		{"X10", []string{"\x1b[?9h"}, true},
		{"only SGR coordinates", []string{"\x1b[?1006h"}, false},
		{"other private modes", []string{"\x1b[?1049h\x1b[?25l\x1b[?2004h"}, false},
		{"turned off", []string{"\x1b[?1002h text \x1b[?1002l"}, false},
		{"one of two off", []string{"\x1b[?1000h\x1b[?1002h\x1b[?1000l"}, true},
		{"both off in one", []string{"\x1b[?1000;1002h", "\x1b[?1002;1000l"}, false},
		{"split", []string{"ab\x1b", "[", "?10", "00", "h"}, true},
		{"split, then off", []string{"\x1b[?1000h", "\x1b[?10", "00l"}, false},
		{"reset", []string{"\x1b[?1000h", "\x1bc"}, false},
		{"not private", []string{"\x1b[1000h"}, false},
		{"ESC in the parameters", []string{"\x1b[?10\x1b[?1000h"}, true},
		{"too long", []string{"\x1b[?" + strings.Repeat("1;", maxModeParams/2) + "1000h"}, false},
	} {
		var m mouseModeTracker
		for _, w := range tc.writes {
			if n, err := m.Write([]byte(w)); n != len(w) || err != nil {
				t.Fatalf("%s: Write returned %d, %v", tc.name, n, err)
			}
		}
		if got := m.on(); got != tc.want {
			t.Errorf("%s: mouse tracking is %v, want %v", tc.name, got, tc.want)
		}
	}
}
