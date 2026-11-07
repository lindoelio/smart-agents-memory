// Package sqlite is the SQLite gateway: the same corpus in a single
// database file, for people who want the memory to travel with the machine
// instead of living in a vault.
//
// The driver is the pure Go one, so the binary cross-compiles without a C
// toolchain.
package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	"github.com/lindoelio/smart-agents-memory/internal/memory"
	"github.com/lindoelio/smart-agents-memory/internal/vault"
)

const schema = `
CREATE TABLE IF NOT EXISTS entries (
	id           INTEGER PRIMARY KEY AUTOINCREMENT,
	title        TEXT NOT NULL,
	type         TEXT NOT NULL,
	date         TEXT NOT NULL DEFAULT '',
	body         TEXT NOT NULL DEFAULT '',
	tier         TEXT NOT NULL DEFAULT '',
	source       TEXT NOT NULL DEFAULT '',
	native       TEXT NOT NULL DEFAULT '',
	native_type  TEXT NOT NULL DEFAULT '',
	native_file  TEXT NOT NULL DEFAULT '',
	reason       TEXT NOT NULL DEFAULT '',
	description  TEXT NOT NULL DEFAULT '',
	keywords     TEXT NOT NULL DEFAULT '',
	extra        TEXT NOT NULL DEFAULT '',
	updated_at   INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS entries_type ON entries(type);
CREATE INDEX IF NOT EXISTS entries_title ON entries(title);
CREATE TABLE IF NOT EXISTS log (
	id    INTEGER PRIMARY KEY AUTOINCREMENT,
	ts    TEXT NOT NULL,
	line  TEXT NOT NULL
);
`

// Store is a corpus kept in a SQLite database.
type Store struct {
	db   *sql.DB
	path string
}

func init() {
	vault.Register("sqlite", func(cfg vault.Config) (vault.Vault, error) {
		path := cfg.SQLitePath
		if path == "" {
			return nil, errors.New("the sqlite gateway needs a database path")
		}
		if dir := filepath.Dir(path); dir != "" {
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return nil, err
			}
		}
		db, err := sql.Open("sqlite", path)
		if err != nil {
			return nil, err
		}
		if _, err := db.Exec(schema); err != nil {
			db.Close()
			return nil, err
		}
		return &Store{db: db, path: path}, nil
	})
}

// Kind is the registered name of the gateway.
func (s *Store) Kind() string { return "sqlite" }

// Root is where the store lives.
func (s *Store) Root() string { return s.path }

// Close releases the database.
func (s *Store) Close() error { return s.db.Close() }

// Entries returns every entry in the store.
func (s *Store) Entries(_ context.Context) ([]memory.Entry, error) {
	rows, err := s.db.Query(`SELECT id, title, type, date, body, tier, source, native,
		native_type, native_file, reason, description, keywords, extra, updated_at
		FROM entries ORDER BY type, lower(title)`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []memory.Entry
	for rows.Next() {
		var (
			e         memory.Entry
			id        int
			keywords  string
			extra     string
			updatedAt int64
		)
		if err := rows.Scan(&id, &e.Title, &e.Type, &e.Date, &e.Body, &e.Tier, &e.Source,
			&e.Native, &e.NativeType, &e.NativeFile, &e.Reason, &e.Description,
			&keywords, &extra, &updatedAt); err != nil {
			return nil, err
		}
		e.Path = fmt.Sprintf("entries/%d", id)
		e.ModTime = time.Unix(updatedAt, 0)
		e.Keywords = decodeList(keywords)
		e.Extra = decodeMap(extra)
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Type != out[j].Type {
			return typeOrder(out[i].Type) < typeOrder(out[j].Type)
		}
		return strings.ToLower(out[i].Title) < strings.ToLower(out[j].Title)
	})
	return out, nil
}

// Create stores a new entry and returns it as persisted.
func (s *Store) Create(_ context.Context, e memory.Entry) (memory.Entry, error) {
	if !e.Type.Valid() {
		return e, fmt.Errorf("invalid entry type %q", e.Type)
	}
	now := time.Now()
	res, err := s.db.Exec(`INSERT INTO entries
		(title, type, date, body, tier, source, native, native_type, native_file,
		 reason, description, keywords, extra, updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		e.Title, string(e.Type), e.Date, e.Body, string(e.Tier), e.Source, e.Native,
		e.NativeType, e.NativeFile, e.Reason, e.Description,
		encodeList(e.Keywords), encodeMap(e.Extra), now.Unix())
	if err != nil {
		return e, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return e, err
	}
	e.Path = fmt.Sprintf("entries/%d", id)
	e.ModTime = now
	return e, nil
}

// Update overwrites an existing entry.
func (s *Store) Update(_ context.Context, e memory.Entry) error {
	id, err := s.idOf(e)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`UPDATE entries SET title=?, type=?, date=?, body=?, tier=?,
		source=?, native=?, native_type=?, native_file=?, reason=?, description=?,
		keywords=?, extra=?, updated_at=? WHERE id=?`,
		e.Title, string(e.Type), e.Date, e.Body, string(e.Tier), e.Source, e.Native,
		e.NativeType, e.NativeFile, e.Reason, e.Description,
		encodeList(e.Keywords), encodeMap(e.Extra), time.Now().Unix(), id)
	return err
}

// Search returns the entries that match the query.
func (s *Store) Search(_ context.Context, query string, limit int) ([]vault.Hit, error) {
	if limit <= 0 {
		limit = 30
	}
	pattern := "%" + strings.ToLower(query) + "%"
	rows, err := s.db.Query(`SELECT id, title, body FROM entries
		WHERE lower(title) LIKE ? OR lower(body) LIKE ?
		ORDER BY type, lower(title) LIMIT ?`, pattern, pattern, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []vault.Hit
	for rows.Next() {
		var id int
		var title, body string
		if err := rows.Scan(&id, &title, &body); err != nil {
			return nil, err
		}
		out = append(out, vault.Hit{
			Path:  fmt.Sprintf("entries/%d", id),
			Text:  snippet(body, query),
			Title: title,
		})
	}
	return out, rows.Err()
}

// AppendLog appends one line to the write log.
func (s *Store) AppendLog(_ context.Context, line string) error {
	_, err := s.db.Exec(`INSERT INTO log (ts, line) VALUES (?, ?)`, time.Now().Format(time.RFC3339), line)
	return err
}

// LogLines returns the lines of the write log, newest first.
func (s *Store) LogLines(_ context.Context, limit int) ([]string, error) {
	rows, err := s.db.Query(`SELECT ts, line FROM log ORDER BY id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var ts, line string
		if err := rows.Scan(&ts, &line); err != nil {
			return nil, err
		}
		out = append(out, fmt.Sprintf("- %s | %s", ts, line))
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// idOf resolves the row id of an entry, by path or by title.
func (s *Store) idOf(e memory.Entry) (int64, error) {
	if e.Path != "" {
		var id int64
		if _, err := fmt.Sscanf(e.Path, "entries/%d", &id); err == nil {
			return id, nil
		}
	}
	var id int64
	err := s.db.QueryRow(`SELECT id FROM entries WHERE title = ? ORDER BY id LIMIT 1`, e.Title).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("entry %q not found: %w", e.Title, err)
	}
	return id, nil
}

// typeOrder gives every type a fixed position.
func typeOrder(t memory.Type) int {
	for i, known := range memory.Types {
		if t == known {
			return i
		}
	}
	return len(memory.Types)
}

// snippet returns the line of the body that matches the query.
func snippet(body, query string) string {
	needle := strings.ToLower(query)
	for _, line := range strings.Split(body, "\n") {
		if strings.Contains(strings.ToLower(line), needle) {
			return strings.TrimSpace(line)
		}
	}
	return strings.TrimSpace(body)
}

// encodeList renders a list as JSON.
func encodeList(items []string) string {
	if len(items) == 0 {
		return ""
	}
	data, err := json.Marshal(items)
	if err != nil {
		return ""
	}
	return string(data)
}

// decodeList reads a list from JSON.
func decodeList(raw string) []string {
	if raw == "" {
		return nil
	}
	var out []string
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil
	}
	return out
}

// encodeMap renders extra frontmatter keys as JSON.
func encodeMap(m map[string]string) string {
	if len(m) == 0 {
		return ""
	}
	data, err := json.Marshal(m)
	if err != nil {
		return ""
	}
	return string(data)
}

// decodeMap reads extra frontmatter keys from JSON.
func decodeMap(raw string) map[string]string {
	if raw == "" {
		return nil
	}
	out := map[string]string{}
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil
	}
	return out
}
