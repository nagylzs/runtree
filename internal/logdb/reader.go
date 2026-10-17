package logdb

import (
	"database/sql"
	"errors"
	"fmt"
	"io"
	"time"
)

// Reader reads a log database (it can be used while runtree writes it)
type Reader struct {
	db *sql.DB
}

func OpenReader(path string) (*Reader, error) {
	db, err := sql.Open("sqlite", dsn(path, true))
	if err != nil {
		return nil, err
	}
	var version int
	if err := db.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if version == 0 {
		_ = db.Close()
		return nil, fmt.Errorf("%s is not a runtree log database", path)
	}
	if version > schemaVersion {
		_ = db.Close()
		return nil, fmt.Errorf("%s is from a newer runtree (schema version %d)", path, version)
	}
	return &Reader{db: db}, nil
}

func (r *Reader) Close() error {
	return r.db.Close()
}

func parseTime(s sql.NullString) time.Time {
	if !s.Valid {
		return time.Time{}
	}
	t, _ := time.Parse(timeFormat, s.String)
	return t
}

type Run struct {
	ID       int64
	Started  time.Time
	Finished time.Time // zero while it runs (or when runtree was killed)
	RunInfo
	Dropped int64
	Events  int64
	Output  int64 // bytes
}

// Runs returns the runs, the oldest first
func (r *Reader) Runs() ([]Run, error) {
	rows, err := r.db.Query(`SELECT id, started, finished, tree_file, host, user, pid, version, dropped,
			(SELECT count(*) FROM events WHERE run_id = runs.id),
			(SELECT coalesce(sum(length(data)), 0) FROM output WHERE run_id = runs.id)
		FROM runs ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []Run
	for rows.Next() {
		var run Run
		var started, finished sql.NullString
		if err := rows.Scan(&run.ID, &started, &finished, &run.TreeFile, &run.Host, &run.User, &run.Pid,
			&run.Version, &run.Dropped, &run.Events, &run.Output); err != nil {
			return nil, err
		}
		run.Started, run.Finished = parseTime(started), parseTime(finished)
		result = append(result, run)
	}
	return result, rows.Err()
}

// LastRun returns the id of the last run
func (r *Reader) LastRun() (int64, error) {
	var id sql.NullInt64
	if err := r.db.QueryRow(`SELECT max(id) FROM runs`).Scan(&id); err != nil {
		return 0, err
	}
	if !id.Valid {
		return 0, errors.New("there are no runs in the log database")
	}
	return id.Int64, nil
}

// CheckRun returns an error if there is no such run
func (r *Reader) CheckRun(runID int64) error {
	var n int
	if err := r.db.QueryRow(`SELECT count(*) FROM runs WHERE id = ?`, runID).Scan(&n); err != nil {
		return err
	}
	if n == 0 {
		return fmt.Errorf("there is no run %d in the log database", runID)
	}
	return nil
}

type Event struct {
	Time    time.Time
	Level   string
	NodeID  string // empty for the events of the tree
	Path    string // of the node
	Event   string
	Message string
	Attrs   string // JSON object
}

// Events returns the events of a run, in time order
func (r *Reader) Events(runID int64) ([]Event, error) {
	rows, err := r.db.Query(`SELECT e.time, e.level, coalesce(e.node_id, ''), coalesce(n.path, ''),
			coalesce(e.event, ''), e.message, e.attrs
		FROM events e LEFT JOIN nodes n ON n.run_id = e.run_id AND n.node_id = e.node_id
		WHERE e.run_id = ? ORDER BY e.id`, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []Event
	for rows.Next() {
		var e Event
		var t sql.NullString
		if err := rows.Scan(&t, &e.Level, &e.NodeID, &e.Path, &e.Event, &e.Message, &e.Attrs); err != nil {
			return nil, err
		}
		e.Time = parseTime(t)
		result = append(result, e)
	}
	return result, rows.Err()
}

// FindNodes returns the nodes of a run whose id, label or path is name. When there is no such node, then the nodes
// whose path contains name.
func (r *Reader) FindNodes(runID int64, name string) ([]NodeInfo, error) {
	nodes, err := r.queryNodes(`SELECT node_id, coalesce(parent_id, ''), type, label, path FROM nodes
		WHERE run_id = ? AND (node_id = ? OR label = ? OR path = ?) ORDER BY rowid`, runID, name, name, name)
	if err != nil || len(nodes) > 0 {
		return nodes, err
	}
	return r.queryNodes(`SELECT node_id, coalesce(parent_id, ''), type, label, path FROM nodes
		WHERE run_id = ? AND path LIKE ? ESCAPE '\' ORDER BY rowid`, runID, "%"+escapeLike(name)+"%")
}

func (r *Reader) queryNodes(query string, args ...any) ([]NodeInfo, error) {
	rows, err := r.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []NodeInfo
	for rows.Next() {
		var n NodeInfo
		if err := rows.Scan(&n.ID, &n.ParentID, &n.Type, &n.Label, &n.Path); err != nil {
			return nil, err
		}
		result = append(result, n)
	}
	return result, rows.Err()
}

// Starts returns the starts of a node in a run that have output, in order
func (r *Reader) Starts(runID int64, nodeID string) ([]int, error) {
	rows, err := r.db.Query(`SELECT DISTINCT start FROM output WHERE run_id = ? AND node_id = ? ORDER BY start`,
		runID, nodeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []int
	for rows.Next() {
		var start int
		if err := rows.Scan(&start); err != nil {
			return nil, err
		}
		result = append(result, start)
	}
	return result, rows.Err()
}

// Output writes the terminal output of a start of a node to w
func (r *Reader) Output(runID int64, nodeID string, start int, w io.Writer) error {
	rows, err := r.db.Query(`SELECT data FROM output WHERE run_id = ? AND node_id = ? AND start = ? ORDER BY id`,
		runID, nodeID, start)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var data []byte
		if err := rows.Scan(&data); err != nil {
			return err
		}
		if _, err := w.Write(data); err != nil {
			return err
		}
	}
	return rows.Err()
}
