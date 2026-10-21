package headless

import (
	"bytes"
	"io"
	"strconv"
	"strings"
	"unicode/utf8"
)

// conptyStart is how the output of the pseudo console of Windows (ConPTY) starts: it turns on its own input mode
const conptyStart = "\x1b[?9001h"

// maxPen is the number of color sequences that the pen keeps (since the last reset)
const maxPen = 16

// conptyFilter turns the output of a pseudo console of Windows into lines for the console (a log). ConPTY draws a
// screen instead of writing lines: it moves the cursor (also for blank lines), repaints rows, scrolls, and draws
// progress panes over the text. The filter follows the cursor on a screen of the size of the pseudo console, and it
// knows which rows have text in the log already (used):
//   - the log line is on a row of the screen; a move to a later unused row ends it, and the unused rows that it skips
//     are blank lines; a newline on the bottom row scrolls the screen;
//   - text on a used row (other than the log line's) is a repaint (e.g. after a scroll, or under a progress pane): it
//     is dropped; text on an unused row starts a new log line;
//   - a move to the first column of the log line's row starts the line again (like \r); a move to the left within
//     the line (or a backspace) overwrites it: the text is dropped until it is longer than before (e.g. a countdown);
//   - a move to the right is spaces, the erases are dropped, a clear screen makes every row unused;
//   - the blank lines are written when text follows: not before a clear screen, nor at the end;
//   - the wide characters (East Asian, emoji) take two columns;
//   - the colors (ESC[...m) are kept until a reset, and they are written again at the beginning of every line, but a
//     line that has only colors is dropped;
//   - the private modes (ESC[?...h/l) and the window title (ESC]0;...BEL) are dropped.
//
// It is on from the start (a Windows runner), or when the output starts with conptyStart; other output goes through
// unchanged.
type conptyFilter struct {
	w      io.WriteCloser
	on     bool
	known  bool   // the start of the output decided on (with !on: it is not ConPTY)
	start  []byte // the beginning of the output, until it is known
	seq    []byte // the escape sequence that is being read
	inSeq  bool
	osc    bool // seq is an OSC (ESC ]), it ends with BEL or ESC \
	out    []byte
	width  int
	height int

	row, col int    // the cursor (1-based)
	logRow   int    // the row of the log line
	used     []bool // the rows that have text in the log (1-based, used[0] is not a row)
	text     bool   // the log line has text
	blanks   int    // the blank lines before the log line: they are written when text follows (not before a clear)
	maxCol   int    // the last column of the log line that has text: the columns before it are overwritten
	restart  bool   // the cursor went to the first column of the log line: text starts it again (a redraw, like \r)
	utf8     []byte // the bytes of a UTF-8 character that is being read
	spaces   int    // the spaces at the end of the log line: they are written before the next text (not at its end)
	pen      [][]byte
}

// newConptyFilter returns the filter in front of w, for a pseudo console of width x height (0: 80 x 24); on: the
// output is ConPTY's (the node runs on Windows)
func newConptyFilter(w io.WriteCloser, on bool, width, height int) *conptyFilter {
	if width <= 0 {
		width = 80
	}
	if height <= 0 {
		height = 24
	}
	return &conptyFilter{w: w, on: on, known: on, width: width, height: height, row: 1, col: 1, logRow: 1,
		used: make([]bool, height+1)}
}

func (f *conptyFilter) Write(p []byte) (int, error) {
	n := len(p)
	if !f.known {
		f.start = append(f.start, p...)
		if len(f.start) < len(conptyStart) && strings.HasPrefix(conptyStart, string(f.start)) {
			return n, nil // not known yet
		}
		f.known, f.on = true, bytes.HasPrefix(f.start, []byte(conptyStart))
		p, f.start = f.start, nil
		if !f.on {
			_, err := f.w.Write(p)
			return n, err
		}
	} else if !f.on {
		return f.w.Write(p)
	}
	f.out = f.out[:0]
	for _, b := range p {
		f.filter(b)
	}
	if len(f.out) > 0 {
		if _, err := f.w.Write(f.out); err != nil {
			return n, err
		}
	}
	return n, nil
}

// Close writes the start of the output that was not known yet, and closes the writer
func (f *conptyFilter) Close() error {
	if !f.known && len(f.start) > 0 {
		_, _ = f.w.Write(f.start)
	}
	return f.w.Close()
}

func (f *conptyFilter) filter(b byte) {
	if f.inSeq {
		f.sequence(b)
		return
	}
	switch {
	case b == 0x1b:
		f.inSeq, f.osc, f.seq = true, false, append(f.seq[:0], b)
	case b == '\n':
		f.lineFeed()
	case b == '\r':
		f.toFirstColumn()
	case b == '\b':
		f.col = max(1, f.col-1)
	case b < 0x20 || b == 0x7f: // other control characters (e.g. a bell)
	case b < 0x80:
		f.character([]byte{b}, 1)
	case b >= 0xc0: // the first byte of a UTF-8 character
		f.utf8 = append(f.utf8[:0], b)
	case len(f.utf8) > 0: // a continuation byte
		f.utf8 = append(f.utf8, b)
		if utf8.FullRune(f.utf8) {
			r, _ := utf8.DecodeRune(f.utf8)
			f.character(f.utf8, runeWidth(r))
			f.utf8 = f.utf8[:0]
		}
	}
}

// wideEmoji are the emoji of the Basic Multilingual Plane that are shown wide (Emoji_Presentation in Unicode's emoji
// data), e.g. ✅ (U+2705): pairs of the first and the last code point of a range
var wideEmoji = []rune{
	0x231a, 0x231b, 0x23e9, 0x23ec, 0x23f0, 0x23f0, 0x23f3, 0x23f3, 0x25fd, 0x25fe, 0x2614, 0x2615, 0x2648, 0x2653,
	0x267f, 0x267f, 0x2693, 0x2693, 0x26a1, 0x26a1, 0x26aa, 0x26ab, 0x26bd, 0x26be, 0x26c4, 0x26c5, 0x26ce, 0x26ce,
	0x26d4, 0x26d4, 0x26ea, 0x26ea, 0x26f2, 0x26f3, 0x26f5, 0x26f5, 0x26fa, 0x26fa, 0x26fd, 0x26fd, 0x2705, 0x2705,
	0x270a, 0x270b, 0x2728, 0x2728, 0x274c, 0x274c, 0x274e, 0x274e, 0x2753, 0x2755, 0x2757, 0x2757, 0x2795, 0x2797,
	0x27b0, 0x27b0, 0x27bf, 0x27bf, 0x2b1b, 0x2b1c, 0x2b50, 0x2b50, 0x2b55, 0x2b55,
}

// runeWidth is the number of columns of a character on the screen: 2 for the wide ones (East Asian, emoji), 0 for
// the combining marks
func runeWidth(r rune) int {
	switch {
	case r >= 0x300 && r <= 0x36f, r >= 0x200b && r <= 0x200f, r == 0xfe0f:
		return 0
	case r >= 0x1100 && r <= 0x115f, r >= 0x2e80 && r <= 0x303e, r >= 0x3041 && r <= 0x33ff,
		r >= 0x3400 && r <= 0x4dbf, r >= 0x4e00 && r <= 0x9fff, r >= 0xa000 && r <= 0xa4cf,
		r >= 0xac00 && r <= 0xd7a3, r >= 0xf900 && r <= 0xfaff, r >= 0xfe30 && r <= 0xfe4f,
		r >= 0xff00 && r <= 0xff60, r >= 0xffe0 && r <= 0xffe6,
		r >= 0x1f000 && r <= 0x1faff, // emoji, symbols, regional indicators
		r >= 0x20000 && r <= 0x3fffd:
		return 2
	case r >= 0x231a && r <= 0x2b55:
		for i := 0; i < len(wideEmoji); i += 2 {
			if r >= wideEmoji[i] && r <= wideEmoji[i+1] {
				return 2
			}
		}
	}
	return 1
}

// character writes a character (its UTF-8 bytes) of a width at the cursor
func (f *conptyFilter) character(c []byte, width int) {
	if width == 0 { // a combining mark: on the character before it
		if f.row == f.logRow && f.text {
			f.out = append(f.out, c...)
		}
		return
	}
	if f.col+width-1 > f.width { // autowrap: the line goes on on the next row
		onLog := f.row == f.logRow
		f.col = 1
		f.down()
		if onLog {
			f.logRow, f.maxCol = f.row, 0
		}
	}
	space := len(c) == 1 && c[0] == ' '
	if f.row != f.logRow {
		if f.used[f.row] || space {
			f.col += width // a repaint, or blank
			return
		}
		// text on an unused row: a new log line
		if f.text {
			f.endLine()
		}
		f.logRow, f.maxCol, f.restart = f.row, 0, false
		f.spacesTo(f.col)
	}
	if f.restart {
		f.out = append(f.out, '\r')
		f.text, f.maxCol, f.restart, f.spaces = false, 0, false, 0
	}
	if f.col > f.maxCol { // not an overwrite
		if space {
			f.spaces++
		} else {
			f.addText(c)
		}
		f.maxCol = f.col + width - 1
		f.used[f.row] = true
	}
	f.col += width
}

// addText writes a character to the log line, after the blank lines before it, the colors of the pen at its
// beginning, and the spaces before it
func (f *conptyFilter) addText(c []byte) {
	if !f.text {
		for ; f.blanks > 0; f.blanks-- {
			f.out = append(f.out, '\r', '\n')
		}
		for _, s := range f.pen {
			f.out = append(f.out, s...)
		}
		f.text = true
	}
	f.flushSpaces()
	f.out = append(f.out, c...)
}

// flushSpaces writes the spaces that a text follows
func (f *conptyFilter) flushSpaces() {
	for ; f.spaces > 0; f.spaces-- {
		f.out = append(f.out, ' ')
	}
}

// endLine ends the log line, without the spaces at its end; an empty line is held (see blanks)
func (f *conptyFilter) endLine() {
	if f.text {
		f.out = append(f.out, '\r', '\n')
	} else {
		f.blanks++
	}
	f.text, f.maxCol, f.restart, f.spaces = false, 0, false, 0
}

// down moves the cursor to the next row, the bottom row scrolls the screen
func (f *conptyFilter) down() {
	if f.row < f.height {
		f.row++
		return
	}
	// scroll: the rows move up, the log line too (unless the cursor is on it: then it is the new bottom row)
	copy(f.used[1:], f.used[2:])
	f.used[f.height] = false
	if f.logRow != f.row {
		f.logRow = max(1, f.logRow-1)
	}
}

// lineFeed moves the cursor down: on the log line, it ends the line, and the next row is the log line
func (f *conptyFilter) lineFeed() {
	if f.row != f.logRow {
		f.down()
		return
	}
	f.endLine()
	f.down()
	f.logRow = f.row
}

// toFirstColumn moves the cursor to the first column: on the log line, text after it starts the line again (a redraw),
// but a newline after it just ends the line (\r\n)
func (f *conptyFilter) toFirstColumn() {
	f.col = 1
	if f.row == f.logRow && f.text {
		f.restart = true
	}
}

// moveTo moves the cursor to a row and a column (1-based)
func (f *conptyFilter) moveTo(row, col int) {
	row, col = min(max(row, 1), f.height), min(max(col, 1), f.width)
	switch {
	case row == f.logRow:
		f.row = row
		if col == 1 {
			f.toFirstColumn()
		} else {
			f.spacesTo(col)
		}
	case row > f.logRow && !f.used[row]:
		// a new line below: the log line ends, the unused rows between are blank lines
		f.endLine()
		for r := f.logRow + 1; r < row; r++ {
			if !f.used[r] {
				f.endLine()
			}
		}
		f.row, f.logRow, f.maxCol, f.restart = row, row, 0, false
		f.col = 1
		f.spacesTo(col)
	default: // a used row (a repaint), or an unused row above: the text decides
		f.row, f.col = row, col
	}
}

// spacesTo moves the cursor to a column of the log line: after its end, the gap is spaces
func (f *conptyFilter) spacesTo(col int) {
	if col > f.maxCol+1 && f.row == f.logRow {
		f.spaces += col - 1 - f.maxCol
		f.maxCol = col - 1
	}
	f.col = col
}

// sequence reads a byte of an escape sequence, and handles it when it ends
func (f *conptyFilter) sequence(b byte) {
	f.seq = append(f.seq, b)
	switch {
	case f.osc:
		// OSC: until BEL or ESC \ (the title), dropped
		if b == 0x07 || (b == '\\' && len(f.seq) > 2 && f.seq[len(f.seq)-2] == 0x1b) {
			f.inSeq = false
		}
		return
	case len(f.seq) == 2:
		switch b {
		case '[':
			return // CSI
		case ']':
			f.osc = true
			return
		case '(', ')':
			return // a character set: one more byte
		}
		f.inSeq = false // ESC 7, ESC 8, ESC =...: dropped
		return
	case f.seq[1] == '(' || f.seq[1] == ')':
		f.inSeq = false
		return
	}
	if b < 0x40 || b > 0x7e {
		if len(f.seq) > 64 {
			f.inSeq = false // not a sequence that it knows
		}
		return // parameters and intermediates
	}
	f.inSeq = false
	f.csi(string(f.seq[2:len(f.seq)-1]), b)
}

// csi handles ESC [ params final
func (f *conptyFilter) csi(params string, final byte) {
	if strings.HasPrefix(params, "?") {
		return // a private mode
	}
	n := func(i, def int) int {
		parts := strings.Split(params, ";")
		if i < len(parts) {
			if v, err := strconv.Atoi(parts[i]); err == nil && v > 0 {
				return v
			}
		}
		return def
	}
	switch final {
	case 'm': // colors
		f.color(params)
	case 'H', 'f': // cursor position: row;column
		f.moveTo(n(0, 1), n(1, 1))
	case 'A': // up
		f.moveTo(f.row-n(0, 1), f.col)
	case 'B': // down
		f.moveTo(f.row+n(0, 1), f.col)
	case 'C': // right
		f.moveTo(f.row, f.col+n(0, 1))
	case 'D': // left
		f.moveTo(f.row, f.col-n(0, 1))
	case 'G': // column
		f.moveTo(f.row, n(0, 1))
	case 'd': // row
		f.moveTo(n(0, 1), f.col)
	case 'J':
		if params == "2" || params == "3" { // clear screen: the text is gone, every row is unused
			if f.text {
				f.endLine()
			}
			clear(f.used)
			f.blanks = 0 // e.g. cls erases the rows with newlines first
		}
	}
	// K, X (erases) and the rest are dropped
}

// color handles a color sequence: the pen keeps it until a reset
func (f *conptyFilter) color(params string) {
	if params == "" || params == "0" {
		f.pen = f.pen[:0]
	} else {
		f.pen = append(f.pen, bytes.Clone(f.seq))
		if len(f.pen) > maxPen {
			f.pen = f.pen[1:]
		}
	}
	if f.text && f.row == f.logRow {
		f.flushSpaces() // a colored space keeps its place before the color changes
		f.out = append(f.out, f.seq...)
	}
}
