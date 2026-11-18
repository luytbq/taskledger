// Package registry is the taskledger SQLite store: tasks, parent links, agent
// sessions, and the plugin hello cache.
package registry

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

const (
	Tracked  = "tracked"
	Archived = "archived"
)

var (
	ErrNotFound  = errors.New("task not found")
	ErrDuplicate = errors.New("task id already exists")
	ErrCycle     = errors.New("parent link would create a cycle")
)

// VN is the zone every timestamp written by taskledger uses.
var VN = time.FixedZone("ICT", 7*3600)

func Now() time.Time { return time.Now().In(VN) }

func Stamp(t time.Time) string { return t.In(VN).Format(time.RFC3339) }

type Task struct {
	ID        string  `json:"id"`
	Plugin    string  `json:"plugin"`
	Locator   string  `json:"-"`
	Title     string  `json:"title"`
	Registry  string  `json:"registry"`
	ParentID  *string `json:"parent_id"`
	CreatedAt string  `json:"created_at"`
}

type Session struct {
	Agent     string `json:"agent"`
	SessionID string `json:"session_id"`
	CWD       string `json:"cwd"`
	Title     string `json:"title"`
	StartedAt string `json:"started_at"`
	LastSeen  string `json:"last_seen"`
}

type DB struct{ db *sql.DB }

// migrations[i] moves the schema from user_version i to i+1.
var migrations = []string{
	`CREATE TABLE tasks (
		id          TEXT PRIMARY KEY,
		plugin      TEXT NOT NULL,
		locator     TEXT NOT NULL,
		title       TEXT NOT NULL DEFAULT '',
		parent_id   TEXT REFERENCES tasks(id) ON DELETE SET NULL,
		registry    TEXT NOT NULL DEFAULT 'tracked' CHECK (registry IN ('tracked', 'archived')),
		created_at  TEXT NOT NULL
	);
	CREATE TABLE sessions (
		task_id     TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
		agent       TEXT NOT NULL,
		session_id  TEXT NOT NULL,
		cwd         TEXT NOT NULL,
		title       TEXT NOT NULL DEFAULT '',
		started_at  TEXT NOT NULL,
		last_seen   TEXT NOT NULL,
		PRIMARY KEY (task_id, agent, session_id)
	);
	CREATE TABLE hello_cache (
		plugin      TEXT PRIMARY KEY,
		command     TEXT NOT NULL,
		mtime       INTEGER NOT NULL,
		body        TEXT NOT NULL
	);`,
}

func Open(path string) (*DB, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	// busy_timeout lets concurrent agents wait for the write lock instead of failing.
	dsn := "file:" + path + "?_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	r := &DB{db: db}
	if err := r.migrate(); err != nil {
		db.Close()
		return nil, err
	}
	return r, nil
}

func (r *DB) Close() error { return r.db.Close() }

func (r *DB) migrate() error {
	var v int
	if err := r.db.QueryRow(`PRAGMA user_version`).Scan(&v); err != nil {
		return err
	}
	if v > len(migrations) {
		return fmt.Errorf("registry schema version %d is newer than this tlg (%d)", v, len(migrations))
	}
	for ; v < len(migrations); v++ {
		tx, err := r.db.Begin()
		if err != nil {
			return err
		}
		if _, err := tx.Exec(migrations[v]); err != nil {
			tx.Rollback()
			return fmt.Errorf("migrate registry to v%d: %w", v+1, err)
		}
		if _, err := tx.Exec(fmt.Sprintf(`PRAGMA user_version = %d`, v+1)); err != nil {
			tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

const taskCols = `id, plugin, locator, title, registry, parent_id, created_at`

func scanTask(s interface{ Scan(...any) error }) (Task, error) {
	var t Task
	var parent sql.NullString
	err := s.Scan(&t.ID, &t.Plugin, &t.Locator, &t.Title, &t.Registry, &parent, &t.CreatedAt)
	if parent.Valid {
		p := parent.String
		t.ParentID = &p
	}
	return t, err
}

func (r *DB) AddTask(t Task) error {
	_, err := r.db.Exec(`INSERT INTO tasks (`+taskCols+`) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		t.ID, t.Plugin, t.Locator, t.Title, t.Registry, t.ParentID, t.CreatedAt)
	if err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed") {
		return ErrDuplicate
	}
	return err
}

func (r *DB) GetTask(id string) (Task, error) {
	t, err := scanTask(r.db.QueryRow(`SELECT `+taskCols+` FROM tasks WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return t, ErrNotFound
	}
	return t, err
}

func (r *DB) Exists(id string) (bool, error) {
	var n int
	err := r.db.QueryRow(`SELECT count(*) FROM tasks WHERE id = ?`, id).Scan(&n)
	return n > 0, err
}

// ListTasks returns tasks ordered by id. all=false keeps only tracked tasks;
// plugin="" means every plugin.
func (r *DB) ListTasks(all bool, plugin string) ([]Task, error) {
	q := `SELECT ` + taskCols + ` FROM tasks WHERE 1=1`
	var args []any
	if !all {
		q += ` AND registry = 'tracked'`
	}
	if plugin != "" {
		q += ` AND plugin = ?`
		args = append(args, plugin)
	}
	rows, err := r.db.Query(q+` ORDER BY id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Task{}
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (r *DB) SetTitle(id, title string) error {
	_, err := r.db.Exec(`UPDATE tasks SET title = ? WHERE id = ? AND title <> ?`, title, id, title)
	return err
}

func (r *DB) SetRegistry(id, state string) error {
	res, err := r.db.Exec(`UPDATE tasks SET registry = ? WHERE id = ?`, state, id)
	return affected(res, err)
}

// SetParent links id under parent, or unlinks it when parent is nil. It refuses a
// link that would make id its own ancestor.
func (r *DB) SetParent(id string, parent *string) error {
	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if parent != nil {
		cur := *parent
		for depth := 0; ; depth++ {
			if cur == id {
				return ErrCycle
			}
			var next sql.NullString
			err := tx.QueryRow(`SELECT parent_id FROM tasks WHERE id = ?`, cur).Scan(&next)
			if errors.Is(err, sql.ErrNoRows) {
				if depth == 0 {
					return ErrNotFound
				}
				break
			}
			if err != nil {
				return err
			}
			if !next.Valid {
				break
			}
			cur = next.String
		}
	}
	res, err := tx.Exec(`UPDATE tasks SET parent_id = ? WHERE id = ?`, parent, id)
	if err := affected(res, err); err != nil {
		return err
	}
	return tx.Commit()
}

// Forget deletes the task and its sessions; children lose their parent link.
func (r *DB) Forget(id string) (int, error) {
	tx, err := r.db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	var n int
	if err := tx.QueryRow(`SELECT count(*) FROM sessions WHERE task_id = ?`, id).Scan(&n); err != nil {
		return 0, err
	}
	res, err := tx.Exec(`DELETE FROM tasks WHERE id = ?`, id)
	if err := affected(res, err); err != nil {
		return 0, err
	}
	return n, tx.Commit()
}

func (r *DB) Children(id string, onlyTracked bool) ([]string, error) {
	q := `SELECT id FROM tasks WHERE parent_id = ?`
	if onlyTracked {
		q += ` AND registry = 'tracked'`
	}
	rows, err := r.db.Query(q+` ORDER BY id`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var c string
		if err := rows.Scan(&c); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// TouchSession records that a session worked on a task: it inserts the session
// the first time and only moves last_seen afterwards.
func (r *DB) TouchSession(taskID, agent, sessionID, cwd, title string, now time.Time) (Session, error) {
	ts := Stamp(now)
	_, err := r.db.Exec(`INSERT INTO sessions (task_id, agent, session_id, cwd, title, started_at, last_seen)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (task_id, agent, session_id) DO UPDATE SET
			last_seen = excluded.last_seen,
			title = CASE WHEN excluded.title <> '' THEN excluded.title ELSE sessions.title END`,
		taskID, agent, sessionID, cwd, title, ts, ts)
	if err != nil {
		return Session{}, err
	}
	var s Session
	err = r.db.QueryRow(`SELECT agent, session_id, cwd, title, started_at, last_seen FROM sessions
		WHERE task_id = ? AND agent = ? AND session_id = ?`, taskID, agent, sessionID).
		Scan(&s.Agent, &s.SessionID, &s.CWD, &s.Title, &s.StartedAt, &s.LastSeen)
	return s, err
}

// Sessions returns the task's sessions, most recently seen first. limit <= 0
// means all of them.
func (r *DB) Sessions(taskID string, limit int) ([]Session, error) {
	q := `SELECT agent, session_id, cwd, title, started_at, last_seen FROM sessions
		WHERE task_id = ? ORDER BY last_seen DESC, started_at DESC`
	args := []any{taskID}
	if limit > 0 {
		q += ` LIMIT ?`
		args = append(args, limit)
	}
	rows, err := r.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Session{}
	for rows.Next() {
		var s Session
		if err := rows.Scan(&s.Agent, &s.SessionID, &s.CWD, &s.Title, &s.StartedAt, &s.LastSeen); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func (r *DB) CachedHello(plugin, command string, mtime int64) (string, bool) {
	var body string
	err := r.db.QueryRow(`SELECT body FROM hello_cache WHERE plugin = ? AND command = ? AND mtime = ?`,
		plugin, command, mtime).Scan(&body)
	return body, err == nil
}

func (r *DB) StoreHello(plugin, command string, mtime int64, body string) error {
	_, err := r.db.Exec(`INSERT INTO hello_cache (plugin, command, mtime, body) VALUES (?, ?, ?, ?)
		ON CONFLICT (plugin) DO UPDATE SET command = excluded.command, mtime = excluded.mtime, body = excluded.body`,
		plugin, command, mtime, body)
	return err
}

func affected(res sql.Result, err error) error {
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}
