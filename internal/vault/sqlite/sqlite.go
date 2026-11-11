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
CREATE TABLE IF NOT EXISTS project_notes (
	name       TEXT PRIMARY KEY,
	body       TEXT NOT NULL DEFAULT '',
	updated_at INTEGER NOT NULL DEFAULT 0
);
`

// Store is a corpus kept in a SQLite database.
type Store struct {
	db             *sql.DB
	path           string
	projectionRoot string
}

func init() {
	vault.Register("sqlite", func(cfg vault.Config) (vault.Vault, error) {
		path := cfg.SQLitePath
		if path == "" {
			return nil, errors.New("the sqlite gateway needs a database path")
		}
		root := cfg.ProjectionRoot
		if root == "" {
			root = filepath.Dir(path)
		}
		store := &Store{path: path, projectionRoot: root}
		// Opening a missing file must not create it. Install does that,
		// and the other commands report that the store is not there yet.
		if _, err := os.Stat(path); err == nil {
			if err := store.connect(); err != nil {
				return nil, err
			}
		}
		return store, nil
	})
}

// connect opens the database and applies the schema. It creates the file.
func (s *Store) connect() error {
	if s.db != nil {
		return nil
	}
	if dir := filepath.Dir(s.path); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	db, err := sql.Open("sqlite", s.path)
	if err != nil {
		return err
	}
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return err
	}
	s.db = db
	return nil
}

func (s *Store) readyDB(ctx context.Context) (*sql.DB, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := s.Ready(ctx); err != nil {
		return nil, err
	}
	if s.db == nil {
		if err := s.connect(); err != nil {
			return nil, err
		}
	}
	return s.db, nil
}

// Kind is the registered name of the gateway.
func (s *Store) Kind() string { return "sqlite" }

// Root is where the store lives.
func (s *Store) Root() string { return s.path }

// Ready reports whether the database file exists.
func (s *Store) Ready(_ context.Context) error {
	info, err := os.Stat(s.path)
	if err != nil || info.IsDir() {
		return fmt.Errorf("no memory store at %s — run `smart-agents-memory install` to create one", s.path)
	}
	return nil
}

// Ensure creates the database and its schema.
func (s *Store) Ensure(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return s.connect()
}

// Close releases the database.
func (s *Store) Close() error {
	if s.db == nil {
		return nil
	}
	return s.db.Close()
}

// Entries returns every entry in the store.
func (s *Store) Entries(ctx context.Context) ([]memory.Entry, error) {
	db, err := s.readyDB(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := db.QueryContext(ctx, `SELECT id, title, type, date, body, tier, source, native,
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
func (s *Store) Create(ctx context.Context, e memory.Entry) (memory.Entry, error) {
	if !e.Type.Valid() {
		return e, fmt.Errorf("invalid entry type %q", e.Type)
	}
	db, err := s.readyDB(ctx)
	if err != nil {
		return e, err
	}
	now := time.Now()
	res, err := db.ExecContext(ctx, `INSERT INTO entries
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
func (s *Store) Update(ctx context.Context, e memory.Entry) error {
	db, err := s.readyDB(ctx)
	if err != nil {
		return err
	}
	id, err := s.idOf(ctx, e)
	if err != nil {
		return err
	}
	_, err = db.ExecContext(ctx, `UPDATE entries SET title=?, type=?, date=?, body=?, tier=?,
		source=?, native=?, native_type=?, native_file=?, reason=?, description=?,
		keywords=?, extra=?, updated_at=? WHERE id=?`,
		e.Title, string(e.Type), e.Date, e.Body, string(e.Tier), e.Source, e.Native,
		e.NativeType, e.NativeFile, e.Reason, e.Description,
		encodeList(e.Keywords), encodeMap(e.Extra), time.Now().Unix(), id)
	return err
}

// Search returns the entries that match the query.
//
// % and _ are literals. A search for a percent sign must not match every row.
func (s *Store) Search(ctx context.Context, query string, limit int) ([]vault.Hit, error) {
	if limit <= 0 {
		limit = 30
	}
	db, err := s.readyDB(ctx)
	if err != nil {
		return nil, err
	}
	pattern := "%" + escapeLike(strings.ToLower(query)) + "%"
	rows, err := db.QueryContext(ctx, `SELECT id, title, body FROM entries
		WHERE lower(title) LIKE ? ESCAPE '\' OR lower(body) LIKE ? ESCAPE '\'
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
func (s *Store) AppendLog(ctx context.Context, line string) error {
	db, err := s.readyDB(ctx)
	if err != nil {
		return err
	}
	_, err = db.ExecContext(ctx, `INSERT INTO log (ts, line) VALUES (?, ?)`, time.Now().Format(time.RFC3339), line)
	return err
}

// LogLines returns the lines of the write log, newest first.
func (s *Store) LogLines(ctx context.Context, limit int) ([]string, error) {
	db, err := s.readyDB(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := db.QueryContext(ctx, `SELECT ts, line FROM log ORDER BY id DESC`)
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

// Rename writes an entry under a new title. The row id stays.
func (s *Store) Rename(ctx context.Context, e memory.Entry, title string) (memory.Entry, error) {
	e.Title = title
	if err := s.Update(ctx, e); err != nil {
		return e, err
	}
	return e, nil
}

// Remove deletes an entry from the store.
func (s *Store) Remove(ctx context.Context, e memory.Entry) error {
	db, err := s.readyDB(ctx)
	if err != nil {
		return err
	}
	id, err := s.idOf(ctx, e)
	if err != nil {
		return err
	}
	_, err = db.ExecContext(ctx, `DELETE FROM entries WHERE id = ?`, id)
	return err
}

// ProjectNote returns the note of a project, when the store has one.
func (s *Store) ProjectNote(ctx context.Context, name string) (string, bool, error) {
	db, err := s.readyDB(ctx)
	if err != nil {
		return "", false, err
	}
	var body string
	err = db.QueryRowContext(ctx, `SELECT body FROM project_notes WHERE name = ?`, name).Scan(&body)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return body, true, nil
}

// putProjectNote stores a project note. The command line does not write
// project notes; the file gateways are edited by hand, and the tests of
// this gateway write through here.
func (s *Store) putProjectNote(ctx context.Context, name, body string) error {
	db, err := s.readyDB(ctx)
	if err != nil {
		return err
	}
	_, err = db.ExecContext(ctx, `INSERT INTO project_notes (name, body, updated_at)
		VALUES (?, ?, ?)
		ON CONFLICT(name) DO UPDATE SET body = excluded.body, updated_at = excluded.updated_at`,
		name, body, time.Now().Unix())
	return err
}

// IndexPath is where the plain index of the corpus lives.
func (s *Store) IndexPath() string {
	return filepath.Join(s.projectionRoot, "INDEX.md")
}

// WriteIndex publishes the index next to the local projections.
func (s *Store) WriteIndex(_ context.Context, corpus []memory.Entry) (string, error) {
	var b strings.Builder
	b.WriteString("# Corpus index\n\n")
	b.WriteString(fmt.Sprintf("_Updated at %s. The corpus is the single source of truth; native/ is a projection._\n\n",
		time.Now().Format("2006-01-02 15:04")))
	for _, typ := range memory.Types {
		var subset []memory.Entry
		for _, e := range corpus {
			if e.Type == typ {
				subset = append(subset, e)
			}
		}
		if len(subset) == 0 {
			continue
		}
		b.WriteString("## " + string(typ) + "\n\n")
		sort.SliceStable(subset, func(i, j int) bool {
			return strings.ToLower(subset[i].Title) < strings.ToLower(subset[j].Title)
		})
		for _, e := range subset {
			b.WriteString("- " + e.Title + "\n")
		}
		b.WriteString("\n")
	}
	if err := os.MkdirAll(s.projectionRoot, 0o755); err != nil {
		return "", err
	}
	path := s.IndexPath()
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		return "", err
	}
	return path, nil
}

// idOf resolves the row id of an entry, by path or by title.
func (s *Store) idOf(ctx context.Context, e memory.Entry) (int64, error) {
	if e.Path != "" {
		var id int64
		if _, err := fmt.Sscanf(e.Path, "entries/%d", &id); err == nil {
			return id, nil
		}
	}
	db, err := s.readyDB(ctx)
	if err != nil {
		return 0, err
	}
	var id int64
	err = db.QueryRowContext(ctx, `SELECT id FROM entries WHERE title = ? ORDER BY id LIMIT 1`, e.Title).Scan(&id)
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

// escapeLike makes % and _ and \ literals for a LIKE pattern that uses
// backslash as the escape character.
func escapeLike(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `%`, `\%`)
	s = strings.ReplaceAll(s, `_`, `\_`)
	return s
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
