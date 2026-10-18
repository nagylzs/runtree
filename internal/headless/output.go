package headless

import (
	"bytes"
	"io"
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
}

// newConsole returns the console for the run nodes of the tree
func newConsole(w io.Writer, tree *rt.Tree) *console {
	c := &console{w: w, flushAfter: flushAfter}
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
	return &lineWriter{c: c, prefix: l + strings.Repeat(" ", c.width-utf8.RuneCountInString(l)) + " | "}
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
	buf := make([]byte, 0, len(prefix)+len(line)+5)
	buf = append(buf, prefix...)
	buf = append(buf, line...)
	if bytes.IndexByte(line, 0x1b) >= 0 {
		buf = append(buf, "\x1b[0m"...) // the colors and styles of the line end with it
	}
	buf = append(buf, '\n')
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
		w.c.writeLine(w.prefix, w.line)
	}
	w.line, w.flushed = w.line[:0], false
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
