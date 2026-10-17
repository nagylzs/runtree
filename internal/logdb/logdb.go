// Package logdb saves the log of runtree runs in an SQLite database: the events of the tree, and the terminal output
// of its processes. One database can hold many runs (runtree sessions), see schema.
package logdb

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	_ "modernc.org/sqlite"
)

const schemaVersion = 1

// The times are UTC, in a format that the date and time functions of SQLite understand (e.g.
// datetime(time, 'localtime')).
const schema = `
CREATE TABLE IF NOT EXISTS runs (
	id        INTEGER PRIMARY KEY,
	started   TEXT NOT NULL,
	finished  TEXT,             -- NULL while runtree runs (or when it was killed)
	tree_file TEXT NOT NULL,
	host      TEXT NOT NULL,
	user      TEXT NOT NULL,
	pid       INTEGER NOT NULL,
	version   TEXT NOT NULL,
	dropped   INTEGER NOT NULL DEFAULT 0 -- events that could not be saved (the disk was too slow)
);
CREATE TABLE IF NOT EXISTS nodes (
	run_id    INTEGER NOT NULL REFERENCES runs(id),
	node_id   TEXT NOT NULL,
	parent_id TEXT,             -- NULL for the root
	type      TEXT NOT NULL,
	label     TEXT NOT NULL,    -- the title (or the command)
	path      TEXT NOT NULL,    -- the labels from the root, separated by " › "
	PRIMARY KEY (run_id, node_id)
);
CREATE TABLE IF NOT EXISTS events (
	id        INTEGER PRIMARY KEY,
	run_id    INTEGER NOT NULL REFERENCES runs(id),
	time      TEXT NOT NULL,
	level     TEXT NOT NULL,    -- INFO, WARN, ERROR
	node_id   TEXT,             -- NULL for the events of the tree
	event     TEXT,             -- NULL for other messages (e.g. a lost runner connection)
	message   TEXT NOT NULL,
	attrs     TEXT NOT NULL     -- the other attributes, as a JSON object
);
CREATE INDEX IF NOT EXISTS events_node ON events(run_id, node_id);
CREATE TABLE IF NOT EXISTS output (
	id        INTEGER PRIMARY KEY,
	run_id    INTEGER NOT NULL REFERENCES runs(id),
	node_id   TEXT NOT NULL,
	start     INTEGER NOT NULL, -- which start of the node in the run: 1, then 2 for a re-run...
	time      TEXT NOT NULL,    -- when the first byte of the chunk arrived
	data      BLOB NOT NULL     -- the raw terminal output (with escape sequences)
);
CREATE INDEX IF NOT EXISTS output_node ON output(run_id, node_id, start);
`

const timeFormat = "2006-01-02T15:04:05.000000Z"

func formatTime(t time.Time) string {
	return t.UTC().Format(timeFormat)
}

// RunInfo describes a run (a runtree session)
type RunInfo struct {
	TreeFile string
	Host     string
	User     string
	Pid      int
	Version  string
}

// NodeInfo describes a node of the tree of a run
type NodeInfo struct {
	ID       string
	ParentID string // empty for the root
	Type     string
	Label    string
	Path     string
}

type event struct {
	time    time.Time
	level   string
	node    string // empty: NULL
	event   string // empty: NULL
	message string
	attrs   string
}

type chunk struct {
	node  string
	start int
	time  time.Time
	data  []byte
}

const (
	eventBuffer  = 10000
	outputBuffer = 1000
	batchSize    = 1000                   // items in a transaction at most
	batchDelay   = 100 * time.Millisecond // a transaction waits this much for more items
	maxChunk     = 1 << 20                // consecutive output chunks are merged up to this size
)

// DB saves the log of a run. Its methods can be called from any goroutine, and they do not wait for the disk: the
// records are saved in batches by a goroutine.
type DB struct {
	db        *sql.DB
	runID     int64
	events    chan event
	output    chan chunk
	quit      chan struct{}
	done      chan struct{}
	closeOnce sync.Once
	closeErr  error
	dropped   atomic.Int64
	failed    atomic.Bool // a write failed, it is logged once
}

func dsn(path string, readOnly bool) string {
	q := "_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)"
	if readOnly {
		return "file:" + path + "?mode=ro&" + q
	}
	return "file:" + path + "?_txlock=immediate&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)&" + q
}

// Open opens (or creates) the database, and starts a new run in it.
func Open(path string, run RunInfo) (*DB, error) {
	db, err := sql.Open("sqlite", dsn(path, false))
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if err := initSchema(db); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	res, err := db.Exec(`INSERT INTO runs (started, tree_file, host, user, pid, version) VALUES (?, ?, ?, ?, ?, ?)`,
		formatTime(time.Now()), run.TreeFile, run.Host, run.User, run.Pid, run.Version)
	if err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	d := &DB{
		db:     db,
		events: make(chan event, eventBuffer),
		output: make(chan chunk, outputBuffer),
		quit:   make(chan struct{}),
		done:   make(chan struct{}),
	}
	if d.runID, err = res.LastInsertId(); err != nil {
		_ = db.Close()
		return nil, err
	}
	go d.run()
	return d, nil
}

func initSchema(db *sql.DB) error {
	var version int
	if err := db.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		return err
	}
	if version > schemaVersion {
		return fmt.Errorf("the log database is from a newer runtree (schema version %d)", version)
	}
	if _, err := db.Exec(schema); err != nil {
		return err
	}
	_, err := db.Exec(fmt.Sprintf("PRAGMA user_version = %d", schemaVersion))
	return err
}

// RunID is the id of the run in the database
func (d *DB) RunID() int64 {
	return d.runID
}

// AddNodes saves the nodes of the tree (right away, it is called before the tree runs)
func (d *DB) AddNodes(nodes []NodeInfo) error {
	tx, err := d.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	stmt, err := tx.Prepare(`INSERT INTO nodes (run_id, node_id, parent_id, type, label, path) VALUES (?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	for _, n := range nodes {
		if _, err := stmt.Exec(d.runID, n.ID, nullString(n.ParentID), n.Type, n.Label, n.Path); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func nullString(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func (d *DB) closed() bool {
	select {
	case <-d.quit:
		return true
	default:
		return false
	}
}

// addEvent never blocks (the events are logged while the tree is locked): when the buffer is full, the event is
// dropped (and counted)
func (d *DB) addEvent(e event) {
	if d.closed() {
		return
	}
	select {
	case d.events <- e:
	default:
		if d.dropped.Add(1) == 1 {
			slog.Error("the log database is too slow, events are dropped")
		}
	}
}

// Output returns a writer for the terminal output of a start of a node. The written bytes are copied and saved in
// the background. Write blocks only when the saving is far behind, and it never fails.
func (d *DB) Output(nodeID string, start int) *OutputWriter {
	return &OutputWriter{db: d, node: nodeID, start: start}
}

type OutputWriter struct {
	db    *DB
	node  string
	start int
}

func (w *OutputWriter) Write(p []byte) (int, error) {
	if len(p) == 0 || w.db.closed() {
		return len(p), nil
	}
	c := chunk{node: w.node, start: w.start, time: time.Now(), data: append([]byte(nil), p...)}
	select {
	case w.db.output <- c:
	case <-w.db.quit:
	}
	return len(p), nil
}

// Close saves the remaining records, and the end of the run. Records that come later are not saved. It can be called
// more than once (the other calls wait for the first one, and return its result).
func (d *DB) Close() error {
	d.closeOnce.Do(func() {
		close(d.quit)
		<-d.done
		_, err := d.db.Exec(`UPDATE runs SET finished = ?, dropped = ? WHERE id = ?`, formatTime(time.Now()),
			d.dropped.Load(), d.runID)
		if cerr := d.db.Close(); err == nil {
			err = cerr
		}
		d.closeErr = err
	})
	return d.closeErr
}

// run saves the records in batches, until Close
func (d *DB) run() {
	defer close(d.done)
	for {
		var b batch
		select {
		case e := <-d.events:
			b.addEvent(e)
		case c := <-d.output:
			b.addChunk(c)
		case <-d.quit:
			d.drain(&b)
			d.write(&b)
			return
		}
		timer := time.NewTimer(batchDelay)
	collect:
		for b.size() < batchSize {
			select {
			case e := <-d.events:
				b.addEvent(e)
			case c := <-d.output:
				b.addChunk(c)
			case <-timer.C:
				break collect
			case <-d.quit:
				timer.Stop()
				d.drain(&b)
				d.write(&b)
				return
			}
		}
		timer.Stop()
		d.write(&b)
	}
}

// drain adds the buffered records to the batch
func (d *DB) drain(b *batch) {
	for {
		select {
		case e := <-d.events:
			b.addEvent(e)
		case c := <-d.output:
			b.addChunk(c)
		default:
			return
		}
	}
}

// batch is the records of a transaction. The consecutive output chunks of a start of a node are merged.
type batch struct {
	events []event
	chunks []*chunk
	last   map[string]*chunk // the last chunk of each start of a node
}

func (b *batch) size() int {
	return len(b.events) + len(b.chunks)
}

func (b *batch) addEvent(e event) {
	b.events = append(b.events, e)
}

func (b *batch) addChunk(c chunk) {
	key := fmt.Sprintf("%d %s", c.start, c.node)
	if last := b.last[key]; last != nil && len(last.data)+len(c.data) <= maxChunk {
		last.data = append(last.data, c.data...)
		return
	}
	if b.last == nil {
		b.last = make(map[string]*chunk)
	}
	b.last[key] = &c
	b.chunks = append(b.chunks, &c)
}

func (d *DB) write(b *batch) {
	if b.size() == 0 {
		return
	}
	if err := d.writeTx(b); err != nil && !d.failed.Swap(true) {
		slog.Error("could not save to the log database", "error", err)
	}
}

func (d *DB) writeTx(b *batch) error {
	tx, err := d.db.BeginTx(context.Background(), nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if len(b.events) > 0 {
		stmt, err := tx.Prepare(`INSERT INTO events (run_id, time, level, node_id, event, message, attrs)
			VALUES (?, ?, ?, ?, ?, ?, ?)`)
		if err != nil {
			return err
		}
		for _, e := range b.events {
			if _, err := stmt.Exec(d.runID, formatTime(e.time), e.level, nullString(e.node), nullString(e.event),
				e.message, e.attrs); err != nil {
				return err
			}
		}
	}
	if len(b.chunks) > 0 {
		stmt, err := tx.Prepare(`INSERT INTO output (run_id, node_id, start, time, data) VALUES (?, ?, ?, ?, ?)`)
		if err != nil {
			return err
		}
		for _, c := range b.chunks {
			if _, err := stmt.Exec(d.runID, c.node, c.start, formatTime(c.time), c.data); err != nil {
				return err
			}
		}
	}
	return tx.Commit()
}

// Handler returns a slog handler that saves the records at Info level and above as events of the run. The "node"
// and "event" attributes have their own columns, the others are saved as a JSON object.
func (d *DB) Handler() slog.Handler {
	return &handler{db: d}
}

type handler struct {
	db     *DB
	node   string
	event  string
	attrs  []slog.Attr // from WithAttrs, the keys are prefixed with the groups
	prefix string      // from WithGroup ("a.b.")
}

func (h *handler) Enabled(_ context.Context, level slog.Level) bool {
	return level >= slog.LevelInfo
}

func (h *handler) Handle(_ context.Context, r slog.Record) error {
	h2 := *h
	h2.attrs = append([]slog.Attr(nil), h.attrs...)
	r.Attrs(func(a slog.Attr) bool {
		h2.add(h.prefix, a)
		return true
	})
	h.db.addEvent(event{
		time:    r.Time,
		level:   r.Level.String(),
		node:    h2.node,
		event:   h2.event,
		message: r.Message,
		attrs:   attrsJSON(h2.attrs),
	})
	return nil
}

func (h *handler) WithAttrs(attrs []slog.Attr) slog.Handler {
	h2 := *h
	h2.attrs = append([]slog.Attr(nil), h.attrs...)
	for _, a := range attrs {
		h2.add(h.prefix, a)
	}
	return &h2
}

func (h *handler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	h2 := *h
	h2.prefix += name + "."
	return &h2
}

func (h *handler) add(prefix string, a slog.Attr) {
	v := a.Value.Resolve()
	if prefix == "" && v.Kind() == slog.KindString {
		switch a.Key {
		case "node":
			h.node = v.String()
			return
		case "event":
			h.event = v.String()
			return
		}
	}
	if v.Kind() == slog.KindGroup {
		if a.Key != "" {
			prefix += a.Key + "."
		}
		for _, ga := range v.Group() {
			h.add(prefix, ga)
		}
		return
	}
	if a.Key != "" {
		h.attrs = append(h.attrs, slog.Attr{Key: prefix + a.Key, Value: v})
	}
}

// attrsJSON returns the attributes as a JSON object. Durations are seconds (numbers, so that SQL can add and compare
// them), errors are their messages.
func attrsJSON(attrs []slog.Attr) string {
	m := make(map[string]any, len(attrs))
	for _, a := range attrs {
		v := a.Value
		switch v.Kind() {
		case slog.KindDuration:
			m[a.Key] = v.Duration().Seconds()
		case slog.KindTime:
			m[a.Key] = formatTime(v.Time())
		case slog.KindAny:
			if err, ok := v.Any().(error); ok {
				m[a.Key] = err.Error()
			} else if _, jerr := json.Marshal(v.Any()); jerr == nil {
				m[a.Key] = v.Any()
			} else {
				m[a.Key] = fmt.Sprint(v.Any())
			}
		default:
			m[a.Key] = v.Any()
		}
	}
	data, err := json.Marshal(m)
	if err != nil {
		return "{}"
	}
	return string(data)
}

// escapeLike escapes a text for LIKE ... ESCAPE '\'
func escapeLike(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}
