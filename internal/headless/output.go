package headless

import (
	"bytes"
	"fmt"
	"io"
	"maps"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/nagylzs/runtree/internal/rt"
)

// maxLabel is the maximum length of the labels of the nodes in the output and in the events (in runes)
const maxLabel = 30

// flushAfter is the time after which a line that has not ended yet is written (e.g. a prompt that waits for input)
const flushAfter = 2 * time.Second

// The output modes (--output): all the lines, the lines of the nodes that fail (when they fail), or nothing
const (
	OutputAll    = "all"
	OutputFailed = "failed"
	OutputNone   = "none"
)

// maxHeld is the size of the output of a node that is kept until it fails (--output failed), its end is kept
const maxHeld = 1 << 20

// label is the title of the node (or its command, or its id) for the output and the events, it is shortened to
// maxLabel
func label(n *rt.Node) string {
	l := strings.Join(strings.Fields(n.DisplayLabel()), " ")
	if utf8.RuneCountInString(l) > maxLabel {
		l = string([]rune(l)[:maxLabel-1]) + "…"
	}
	return l
}

// console writes the terminal output of the nodes to w line by line, with the label of the node in front of each
// line, e.g. "web01 apt update | Reading package lists...". It is the OutputRecorder of the tree.
type console struct {
	mu         sync.Mutex // one line at a time
	w          io.Writer
	width      int            // of the labels: they are padded to it
	open       sync.WaitGroup // the outputs that are not closed yet
	flushAfter time.Duration
	mode       string                   // OutputAll ("" too), OutputFailed or OutputNone
	writers    map[*rt.Node]*lineWriter // the output of the last start of the nodes (with OutputFailed)
	// the outputs of the nodes that failed, before the end of their output (with OutputFailed): the exit status may
	// arrive before the end of the output, their lines are written at the end of it, or by finishAll
	pending map[*lineWriter]struct{}
}

// newConsole returns the console for the run nodes of the tree, in an output mode (OutputAll...)
func newConsole(w io.Writer, tree *rt.Tree, mode string) *console {
	c := &console{w: w, flushAfter: flushAfter, mode: mode, writers: make(map[*rt.Node]*lineWriter),
		pending: make(map[*lineWriter]struct{})}
	for _, n := range tree.Nodes {
		if n.Type == rt.TypeRun {
			c.width = max(c.width, utf8.RuneCountInString(label(n)))
		}
	}
	return c
}

func (c *console) NodeOutput(n *rt.Node, _ int) io.Writer {
	c.open.Add(1)
	l := label(n)
	w := &lineWriter{c: c, prefix: l + strings.Repeat(" ", c.width-utf8.RuneCountInString(l)) + " | "}
	if c.mode == OutputFailed {
		c.mu.Lock()
		c.writers[n] = w
		c.mu.Unlock()
	}
	return w
}

// finished is called when a run node finishes: with OutputFailed, its output is written when it failed (at the end of
// the output, which may arrive later than the exit status), or dropped. It does not lock the tree.
func (c *console) finished(n *rt.Node, failed bool) {
	c.mu.Lock()
	w := c.writers[n]
	delete(c.writers, n)
	c.mu.Unlock()
	if w != nil {
		w.decide(failed)
	}
}

// finishAll writes the output of the nodes that failed, and drops the others, for the nodes that did not finish yet,
// and writes the output of the failed nodes whose output did not end (with OutputFailed, at the end of the run)
func (c *console) finishAll(failed func(n *rt.Node) bool) {
	c.mu.Lock()
	writers := c.writers
	c.writers = make(map[*rt.Node]*lineWriter)
	c.mu.Unlock()
	for n, w := range writers {
		w.decide(failed(n))
	}
	c.mu.Lock()
	pending := slices.Collect(maps.Keys(c.pending))
	c.mu.Unlock()
	for _, w := range pending {
		w.writeHeldNow()
	}
}

// wait waits until the outputs are closed (the client of a node closes it after the end of the output), at most for
// timeout. It tells if they are closed.
func (c *console) wait(timeout time.Duration) bool {
	done := make(chan struct{})
	go func() {
		c.open.Wait()
		close(done)
	}()
	select {
	case <-done:
		return true
	case <-time.After(timeout):
		return false
	}
}

func (c *console) writeLine(prefix string, line []byte) {
	c.writeLines(prefix, [][]byte{line})
}

// writeLines writes the lines together, the lines of other nodes do not get between them
func (c *console) writeLines(prefix string, lines [][]byte) {
	var buf []byte
	for _, line := range lines {
		buf = append(buf, prefix...)
		buf = append(buf, line...)
		if bytes.IndexByte(line, 0x1b) >= 0 {
			buf = append(buf, "\x1b[0m"...) // the colors and styles of the line end with it
		}
		buf = append(buf, '\n')
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	_, _ = c.w.Write(buf)
}

// lineWriter is the output of a start of a node. The lines of a terminal end with \r\n; a \r alone ends a line too
// (e.g. a progress bar that redraws its line), then an empty line is not written.
type lineWriter struct {
	c       *console
	prefix  string
	mu      sync.Mutex
	line    []byte // the end of the output that is not a whole line yet
	cr      bool   // the last byte was \r: a \n after it ends the same line
	flushed bool   // the line is written already by flush, its end does not make an empty line
	timer   *time.Timer
	closed  bool

	// with OutputFailed: the lines are held until the node finishes, then they are dropped (drop), or written at the
	// end of the output (written: the later lines are written at once)
	held                   [][]byte
	heldSize, dropped      int // the size of held, and the number of lines that were dropped from its beginning
	decided, drop, written bool
}

func (w *lineWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return len(p), nil
	}
	for _, b := range p {
		if w.cr {
			w.cr = false
			if b == '\n' {
				w.emit(true)
				continue
			}
			w.emit(false)
		}
		switch b {
		case '\n':
			w.emit(true)
		case '\r':
			w.cr = true
		default:
			w.line = append(w.line, b)
		}
	}
	switch {
	case len(w.line) == 0 && !w.cr:
		if w.timer != nil {
			w.timer.Stop()
		}
	case w.timer == nil:
		w.timer = time.AfterFunc(w.c.flushAfter, w.flush)
	default:
		w.timer.Reset(w.c.flushAfter)
	}
	return len(p), nil
}

// emit writes the line (an empty one only when allowEmpty)
func (w *lineWriter) emit(allowEmpty bool) {
	if len(w.line) > 0 || (allowEmpty && !w.flushed) {
		w.out(w.line)
	}
	w.line, w.flushed = w.line[:0], false
}

// out writes a line, or holds it until the node finishes (OutputFailed), or drops it (OutputNone)
func (w *lineWriter) out(line []byte) {
	switch {
	case w.c.mode == OutputNone || w.drop:
	case w.c.mode == OutputFailed && !w.written:
		w.held = append(w.held, bytes.Clone(line))
		w.heldSize += len(line)
		for w.heldSize > maxHeld && len(w.held) > 1 {
			w.heldSize -= len(w.held[0])
			w.held = w.held[1:]
			w.dropped++
		}
	default:
		w.c.writeLine(w.prefix, line)
	}
}

// decide drops the lines when the node did not fail (OutputFailed). When it failed, the held lines are written at the
// end of the output (Close), together, or by finishAll when the output does not end.
func (w *lineWriter) decide(failed bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.decided {
		return
	}
	w.decided, w.drop = true, !failed
	switch {
	case !failed:
		w.held, w.heldSize = nil, 0
	case w.closed:
		w.writeHeld()
	default:
		w.c.mu.Lock()
		w.c.pending[w] = struct{}{}
		w.c.mu.Unlock()
	}
}

// writeHeldNow writes the held lines of a failed node whose output did not end, the later lines will be written at once
func (w *lineWriter) writeHeldNow() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.writeHeld()
}

// writeHeld writes the held lines of a failed node (once), w.mu is held
func (w *lineWriter) writeHeld() {
	if w.written {
		return
	}
	w.written = true
	lines := w.held
	if w.dropped > 0 {
		lines = append([][]byte{fmt.Appendf(nil, "(%d earlier lines are not shown)", w.dropped)}, lines...)
	}
	if len(lines) > 0 {
		w.c.writeLines(w.prefix, lines)
	}
	w.held, w.heldSize = nil, 0
	w.c.mu.Lock()
	delete(w.c.pending, w)
	w.c.mu.Unlock()
}

// flush writes the line that has not ended yet (the rest of it will be on the next line). A \r at its end stays: a \n
// after it ends the line that is written already.
func (w *lineWriter) flush() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.closed && len(w.line) > 0 {
		w.emit(false)
		w.flushed = true
	}
}

// Close writes the line that has not ended yet, it is called by the client of the node at the end of its output
func (w *lineWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return nil
	}
	if w.timer != nil {
		w.timer.Stop()
	}
	w.cr = false
	w.emit(false)
	w.closed = true
	if w.decided && !w.drop {
		w.writeHeld()
	}
	w.c.open.Done()
	return nil
}

// recorders sends the output to several recorders (e.g. the console and the log database)
type recorders []rt.OutputRecorder

func (r recorders) NodeOutput(n *rt.Node, start int) io.Writer {
	var ws multiWriter
	for _, rec := range r {
		if w := rec.NodeOutput(n, start); w != nil {
			ws = append(ws, w)
		}
	}
	return ws
}

// multiWriter writes to all the writers (their errors are ignored), and closes the ones that are io.Closers
type multiWriter []io.Writer

func (m multiWriter) Write(p []byte) (int, error) {
	for _, w := range m {
		_, _ = w.Write(p)
	}
	return len(p), nil
}

func (m multiWriter) Close() error {
	for _, w := range m {
		if c, ok := w.(io.Closer); ok {
			_ = c.Close()
		}
	}
	return nil
}
