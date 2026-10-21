package headless

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// captured collects what the filter writes
type captured struct {
	bytes.Buffer
	closed bool
}

func (c *captured) Close() error {
	c.closed = true
	return nil
}

// The output of the pseudo console of Windows becomes lines, other output goes through unchanged
func TestConptyFilter(t *testing.T) {
	const start = "\x1b[?9001h\x1b[?1004h\x1b[?25l\x1b[2J\x1b[m\x1b[H\x1b]0;C:\\WINDOWS\\system32\\cmd.exe\x07\x1b[?25h"
	const end = "\x1b[?25h\x1b[?9001l\x1b[?1004l"
	for _, tc := range []struct {
		name   string
		on     bool
		writes []string
		want   string
	}{
		{"cmd", false, []string{start + "hello\r\n" + end}, "hello\r\n"},
		{"known to be Windows", true, []string{"\x1b[?25lhello\r\n\x1b]0;title\x1b\\"}, "hello\r\n"},
		{"blank lines are cursor moves", false, []string{start + "one\r\n\x1b[4;1Hfour\r\n"},
			"one\r\n\r\n\r\nfour\r\n"},
		{"a move without the line end", false, []string{start + "one\x1b[2;1Htwo"}, "one\r\ntwo"},
		{"a move up starts the line again", false, []string{start + "50%\x1b[1A\x1b[1;1H60%"}, "50%\r60%"},
		{"a move right is spaces", false, []string{start + "New File\x1b[8C7\x1b[K\r\n"}, "New File        7\r\n"},
		{"colors are kept", false, []string{start + "\x1b[32mgreen\x1b[0m\r\n"}, "\x1b[32mgreen\x1b[0m\r\n"},
		{"a line of only colors is dropped", false, []string{start + "\x1b[m\r\nx\r\n"}, "\r\nx\r\n"},
		{"split between writes", false, []string{"\x1b[?90", "01h\x1b]0;ti", "tle\x07a\x1b[3", "2mb\x1b", "[0m\r", "\n"},
			"a\x1b[32mb\x1b[0m\r\n"},
		{"not ConPTY", false, []string{"\x1b[?25lhello\x1b[K\r\n"}, "\x1b[?25lhello\x1b[K\r\n"},
		{"not ConPTY, split", false, []string{"\x1b[?9", "0x"}, "\x1b[?90x"},
		{"plain", false, []string{"a\r\n", "b"}, "a\r\nb"},
	} {
		c := &captured{}
		f := newConptyFilter(c, tc.on, 80, 24)
		for _, w := range tc.writes {
			if n, err := f.Write([]byte(w)); n != len(w) || err != nil {
				t.Fatalf("%s: Write returned %d, %v", tc.name, n, err)
			}
		}
		if got := c.String(); got != tc.want {
			t.Errorf("%s: got %q, want %q", tc.name, got, tc.want)
		}
		if err := f.Close(); err != nil || !c.closed {
			t.Errorf("%s: not closed: %v", tc.name, err)
		}
	}

	// the start of the output that is not known yet is written when it is closed
	c := &captured{}
	f := newConptyFilter(c, false, 80, 24)
	_, _ = f.Write([]byte("\x1b[?9"))
	_ = f.Close()
	if c.String() != "\x1b[?9" {
		t.Errorf("at close: %q", c.String())
	}
}

// The screen of the pseudo console: colors across moves, scrolling, repaints, overwrites, the width
func TestConptyScreen(t *testing.T) {
	const on = "\x1b[?9001h"
	for _, tc := range []struct {
		name          string
		width, height int
		in, want      string
	}{
		{"a color before a move", 80, 24, on + "\x1b[m\x1b[38;5;9m\x1b[Hred\r\n", "\x1b[38;5;9mred\r\n"},
		{"the color of the next line before its newline", 80, 24,
			on + "plain\x1b[38;5;12m\x1b[48;5;11m\r\nblue\x1b[m\r\n",
			"plain\x1b[38;5;12m\x1b[48;5;11m\r\n\x1b[38;5;12m\x1b[48;5;11mblue\x1b[m\r\n"},
		{"a progress pane after a scroll", 80, 5,
			on + "l1\r\nl2\r\nl3\r\nl4\r\nl5\r\nl6\r\n\x1b[2;1HWorking\x1b[3;1H[ooo   ]\x1b[5;1H" +
				"\x1b[2;1Hl4\x1b[3;1Hl5\x1b[5;1Hafter\r\n",
			"l1\r\nl2\r\nl3\r\nl4\r\nl5\r\nl6\r\nafter\r\n"},
		{"a countdown overwrites the line", 80, 24, on + "Waiting 3 ...\x1b[1;9H2\b1\b0\r\nafter\r\n",
			"Waiting 3 ...\r\nafter\r\n"},
		{"a long line wraps", 10, 24, on + "0123456789abc\x1b[3;1Hnext\r\n", "0123456789abc\r\nnext\r\n"},
		{"a redraw of the line", 80, 24, on + "50%\r60%\r\n", "50%\r60%\r\n"},
		{"a blank line after a wrapped line", 10, 24, on + "0123456789abc\x1b[4;1Hnext", "0123456789abc\r\n\r\nnext"},
		{"text at a column after the end", 80, 24, on + "a\x1b[1;5Hb", "a   b"},
		{"cls erases the rows with newlines, then clears", 80, 6,
			on + "a\r\n\x1b[H" + strings.Repeat("\x1b[K\r\n", 6) + "\x1b[3;1H\x1b[3J\x1b[Hc\r\n", "a\r\nc\r\n"},
		{"a blank line at the beginning, by a move", 80, 24, on + "\x1b[2;1Htable\r\n", "\r\ntable\r\n"},
		{"the blank lines at the end are dropped", 80, 24, on + "x\r\n\r\n\r\n", "x\r\n"},
		{"wide characters take two columns", 80, 24, on + "漢字\x1b[1;6Hx", "漢字 x"},
		{"a wide character wraps", 5, 24, on + "漢字漢\x1b[3;1Hnext", "漢字漢\r\nnext"},
		{"a combining mark", 80, 24, on + "e\u0301\x1b[1;3Hx", "e\u0301 x"},
		// ConPTY repaints "50%" as "99%" at the column after two wide emoji
		{"a wide emoji of the BMP", 80, 24, on + "✅✅ 50%\x1b[1;6H99% done", "✅✅ 50% done"},
		{"a wide emoji", 80, 24, on + "🚀🚀 50%\x1b[1;6H99% done", "🚀🚀 50% done"},
		{"a narrow symbol", 80, 24, on + "⚠ x\x1b[1;5Hy", "⚠ x y"},
	} {
		c := &captured{}
		f := newConptyFilter(c, false, tc.width, tc.height)
		if _, err := f.Write([]byte(tc.in)); err != nil {
			t.Fatal(err)
		}
		if got := c.String(); got != tc.want {
			t.Errorf("%s:\n got %q\nwant %q", tc.name, got, tc.want)
		}
	}
}

// The output of real programs on Windows 11 (testdata/conpty, captured with the local runner of runtree-headless, a
// pseudo console of 120 x 40): whole, and in the chunks as they arrived. The lines are compared without their colors
// and spaces at the end; colors are checked separately.
func TestConptyCaptures(t *testing.T) {
	var chunks map[string]struct {
		Chunks []int `json:"chunks"`
	}
	data, err := os.ReadFile("testdata/conpty/chunks.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &chunks); err != nil {
		t.Fatal(err)
	}
	sgr := regexp.MustCompile("\x1b\\[[0-9;]*m")
	lines := func(out string) []string {
		var res []string
		for _, l := range strings.Split(strings.TrimSuffix(sgr.ReplaceAllString(out, ""), "\r\n"), "\r\n") {
			res = append(res, strings.TrimRight(l, " "))
		}
		return res
	}
	steps := []string{"step 1", "step 2", "step 3"}
	var forty []string
	for i := 1; i <= 40; i++ {
		forty = append(forty, fmt.Sprintf("line %d", i))
	}
	for _, tc := range []struct {
		file   string
		want   []string // nil: only the last line is checked (lastLine)
		last   string
		colors []string // in the output, in this order
	}{
		{file: "1a-powershell-steps.bin", want: steps,
			colors: []string{"\x1b[38;5;10mstep 1", "\x1b[38;5;10mstep 2", "\x1b[38;5;10mstep 3"}},
		{file: "1b-powershell-colors.bin", want: []string{"red", "green then plain", "blue-on-yellow", "plain"},
			colors: []string{"\x1b[38;5;9mred", "\x1b[38;5;10mgreen", "\x1b[38;5;12m\x1b[48;5;11mblue-on-yellow"}},
		{file: "1c-cmd-ansi.bin", want: []string{"red plain", "green bg", "bold blue end"},
			colors: []string{"\x1b[31mred", "\x1b[42mgreen bg", "\x1b[34m\x1b[1mbold blue"}},
		{file: "2-scroll-progress.bin", want: append(forty, "after 1", "after 2")},
		{file: "3-progress.bin", last: "done"}, // the progress pane may be a few lines before it
		{file: "4-cmd-timeout.bin", want: []string{"before", "", "Waiting for 3 seconds, press a key to continue ...",
			"after"}},
	} {
		input, err := os.ReadFile("testdata/conpty/" + tc.file)
		if err != nil {
			t.Fatal(err)
		}
		sizes := chunks[tc.file].Chunks
		for _, split := range []bool{false, true} {
			c := &captured{}
			f := newConptyFilter(c, false, 120, 40)
			if split {
				rest := input
				for _, n := range sizes {
					_, _ = f.Write(rest[:n])
					rest = rest[n:]
				}
				if len(rest) > 0 {
					t.Fatalf("%s: the chunks are not the whole capture", tc.file)
				}
			} else {
				_, _ = f.Write(input)
			}
			out := c.String()
			got := lines(out)
			switch {
			case tc.want != nil && !slices.Equal(got, tc.want):
				t.Errorf("%s (split %v): the lines are\n%q\nwant\n%q", tc.file, split, got, tc.want)
			case tc.want == nil && (len(got) == 0 || got[len(got)-1] != tc.last || len(got) > 4):
				t.Errorf("%s (split %v): the lines are %q", tc.file, split, got)
			}
			rest := out
			for _, c := range tc.colors {
				i := strings.Index(rest, c)
				if i < 0 {
					t.Errorf("%s (split %v): %q is not in %q", tc.file, split, c, out)
					break
				}
				rest = rest[i+len(c):]
			}
		}
	}
}
