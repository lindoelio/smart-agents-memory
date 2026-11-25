package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lindoelio/smart-agents-memory/internal/memory"
	"github.com/lindoelio/smart-agents-memory/internal/vault"
)

// openStore opens a SQLite gateway in a temporary directory and creates the
// schema.
func openStore(t *testing.T) *Store {
	t.Helper()
	dir := t.TempDir()
	opened, err := vault.Open(vault.Config{
		Backend:        "sqlite",
		SQLitePath:     filepath.Join(dir, "memory.db"),
		ProjectionRoot: dir,
	})
	if err != nil {
		t.Fatal(err)
	}
	store, ok := opened.(*Store)
	if !ok {
		t.Fatalf("the sqlite gateway is %T", opened)
	}
	if err := store.Ensure(context.Background()); err != nil {
		t.Fatal(err)
	}
	return store
}

func TestKindRootAndIndexPath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "memory.db")
	opened, err := vault.Open(vault.Config{Backend: "sqlite", SQLitePath: path})
	if err != nil {
		t.Fatal(err)
	}
	store := opened.(*Store)
	if store.Kind() != "sqlite" {
		t.Fatalf("the kind is %q", store.Kind())
	}
	if store.Root() != path {
		t.Fatalf("the root is %q", store.Root())
	}
	// Without a projection root, the index lives next to the database.
	if store.IndexPath() != filepath.Join(dir, "INDEX.md") {
		t.Fatalf("the index path is %q", store.IndexPath())
	}
	// Opening a missing database must not create the file.
	if err := store.Ready(context.Background()); err == nil {
		t.Fatal("a missing database is ready")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("opening the gateway created the database")
	}
}

func TestOpenWithoutAPathIsAnError(t *testing.T) {
	if _, err := vault.Open(vault.Config{Backend: "sqlite"}); err == nil ||
		!strings.Contains(err.Error(), "needs a database path") {
		t.Fatalf("the error is %v", err)
	}
}

func TestOpenReportsAConnectionFailure(t *testing.T) {
	// A directory is not a database, and the schema cannot be applied.
	dir := t.TempDir()
	if _, err := vault.Open(vault.Config{Backend: "sqlite", SQLitePath: dir}); err == nil {
		t.Fatal("a directory opened as a database")
	}
}

func TestEnsureReportsACanceledContext(t *testing.T) {
	store := &Store{path: filepath.Join(t.TempDir(), "memory.db")}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := store.Ensure(ctx); err == nil {
		t.Fatal("a canceled Ensure succeeded")
	}
}

func TestCloseIsIdempotent(t *testing.T) {
	var unopened Store
	if err := unopened.Close(); err != nil {
		t.Fatalf("closing an unopened store failed: %v", err)
	}
	store := openStore(t)
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("the second close failed: %v", err)
	}
}

func TestReadyReportsADirectory(t *testing.T) {
	store := &Store{path: t.TempDir()}
	if err := store.Ready(context.Background()); err == nil {
		t.Fatal("a directory is a ready database")
	}
}

func TestReadyDBRejectsACanceledContext(t *testing.T) {
	store := openStore(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := store.readyDB(ctx); err == nil {
		t.Fatal("readyDB accepted a canceled context")
	}
}

func TestQueriesReportAClosedDatabase(t *testing.T) {
	ctx := context.Background()
	store := openStore(t)
	created, err := store.Create(ctx, memory.Entry{Title: "A lesson", Type: memory.TypeLesson, Body: "Body."})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	checks := map[string]func() error{
		"entries": func() error { _, err := store.Entries(ctx); return err },
		"create": func() error {
			_, err := store.Create(ctx, memory.Entry{Title: "x", Type: memory.TypeLesson})
			return err
		},
		"update":             func() error { return store.Update(ctx, created) },
		"search":             func() error { _, err := store.Search(ctx, "a", 10); return err },
		"append log":         func() error { return store.AppendLog(ctx, "line") },
		"log lines":          func() error { _, err := store.LogLines(ctx, 10); return err },
		"remove":             func() error { return store.Remove(ctx, created) },
		"project note":       func() error { _, _, err := store.ProjectNote(ctx, "demo"); return err },
		"project note names": func() error { _, err := store.ProjectNoteNames(ctx); return err },
		"remove note":        func() error { return store.RemoveProjectNote(ctx, "demo") },
		"put project note":   func() error { return store.putProjectNote(ctx, "demo", "body") },
	}
	for name, check := range checks {
		if err := check(); err == nil {
			t.Errorf("%s did not report the closed database", name)
		}
	}
}

func TestAppendLogAndLogLines(t *testing.T) {
	ctx := context.Background()
	store := openStore(t)
	for _, line := range []string{"first", "second", "third"} {
		if err := store.AppendLog(ctx, line); err != nil {
			t.Fatal(err)
		}
	}
	lines, err := store.LogLines(ctx, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 3 {
		t.Fatalf("the log has %v", lines)
	}
	if !strings.HasSuffix(lines[0], "| third") || !strings.HasSuffix(lines[2], "| first") {
		t.Fatalf("the log is not newest first: %v", lines)
	}
	limited, err := store.LogLines(ctx, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(limited) != 2 || limited[0] != lines[0] {
		t.Fatalf("the limited log is %v", limited)
	}
}

func TestWriteIndex(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	store := &Store{projectionRoot: dir}
	corpus := []memory.Entry{
		{Title: "Beta", Type: memory.TypeLesson},
		{Title: "Alpha", Type: memory.TypeLesson},
		{Title: "A decision", Type: memory.TypeProject},
	}
	path, err := store.WriteIndex(ctx, corpus)
	if err != nil {
		t.Fatal(err)
	}
	if path != filepath.Join(dir, "INDEX.md") {
		t.Fatalf("the index path is %q", path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "- Alpha") || !strings.Contains(string(data), "## project") {
		t.Fatalf("the index is:\n%s", data)
	}
	if strings.Index(string(data), "Alpha") > strings.Index(string(data), "Beta") {
		t.Fatalf("the index is not sorted:\n%s", data)
	}

	// A file where the projection root belongs is an error.
	file := filepath.Join(t.TempDir(), "root-file")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := (&Store{projectionRoot: file}).WriteIndex(ctx, corpus); err == nil {
		t.Fatal("the index was written into a file")
	}

	// A folder where INDEX.md belongs is an error.
	blocked := t.TempDir()
	if err := os.MkdirAll(filepath.Join(blocked, "INDEX.md"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := (&Store{projectionRoot: blocked}).WriteIndex(ctx, corpus); err == nil {
		t.Fatal("the index was written over a folder")
	}
}

func TestCreateRejectsAnUnknownType(t *testing.T) {
	store := openStore(t)
	if _, err := store.Create(context.Background(), memory.Entry{
		Title: "x", Type: memory.Type("nonsense"),
	}); err == nil {
		t.Fatal("an unknown type was stored")
	}
}

func TestUpdateRenameAndRemoveReportAMissingEntry(t *testing.T) {
	ctx := context.Background()
	store := openStore(t)
	missing := memory.Entry{Title: "Never stored", Type: memory.TypeLesson}
	if err := store.Update(ctx, missing); err == nil {
		t.Fatal("a missing entry was updated")
	}
	if _, err := store.Rename(ctx, missing, "Another"); err == nil {
		t.Fatal("a missing entry was renamed")
	}
	if err := store.Remove(ctx, missing); err == nil {
		t.Fatal("a missing entry was removed")
	}
}

func TestIDOfResolvesByPathThenTitle(t *testing.T) {
	ctx := context.Background()
	store := openStore(t)
	created, err := store.Create(ctx, memory.Entry{Title: "A lesson", Type: memory.TypeLesson, Body: "Body."})
	if err != nil {
		t.Fatal(err)
	}
	byPath, err := store.idOf(ctx, created)
	if err != nil {
		t.Fatal(err)
	}
	byTitle, err := store.idOf(ctx, memory.Entry{Title: "A lesson", Type: memory.TypeLesson})
	if err != nil {
		t.Fatal(err)
	}
	if byPath != byTitle {
		t.Fatalf("the path resolved to %d and the title to %d", byPath, byTitle)
	}

	// A broken path falls back to the title.
	recovered, err := store.idOf(ctx, memory.Entry{Title: "A lesson", Path: "not-a-row"})
	if err != nil || recovered != byTitle {
		t.Fatalf("the broken path resolved to %d (%v)", recovered, err)
	}

	// An unknown title is an error that names the entry.
	_, err = store.idOf(ctx, memory.Entry{Title: "Never stored"})
	if err == nil || !strings.Contains(err.Error(), "Never stored") {
		t.Fatalf("the error is %v", err)
	}
}

func TestEntriesSortAnUnknownTypeLast(t *testing.T) {
	ctx := context.Background()
	store := openStore(t)
	if err := store.connect(); err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.ExecContext(ctx,
		`INSERT INTO entries (title, type, body) VALUES ('Zed', 'unknown-type', 'Body.')`); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Create(ctx, memory.Entry{Title: "A lesson", Type: memory.TypeLesson, Body: "Body."}); err != nil {
		t.Fatal(err)
	}
	entries, err := store.Entries(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 || entries[len(entries)-1].Title != "Zed" {
		t.Fatalf("the unknown type is not last: %+v", entries)
	}
	if typeOrder(memory.Type("unknown-type")) != len(memory.Types) {
		t.Fatal("typeOrder of an unknown type is not the last position")
	}
}

func TestSearchMatchesTheTitleAndFallsBackToTheBody(t *testing.T) {
	ctx := context.Background()
	store := openStore(t)
	if _, err := store.Create(ctx, memory.Entry{
		Title: "A pipefail lesson", Type: memory.TypeLesson,
		Body: "First line.\nThe pipeline aborts.\nLast line.",
	}); err != nil {
		t.Fatal(err)
	}

	hits, err := store.Search(ctx, "pipefail", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || hits[0].Title != "A pipefail lesson" {
		t.Fatalf("the title search is %+v", hits)
	}
	if hits[0].Text != "First line.\nThe pipeline aborts.\nLast line." {
		t.Fatalf("a title-only match returned %q", hits[0].Text)
	}

	hits, err = store.Search(ctx, "pipeline", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || hits[0].Text != "The pipeline aborts." {
		t.Fatalf("the body search is %+v", hits)
	}

	hits, err = store.Search(ctx, "absent", 10)
	if err != nil || len(hits) != 0 {
		t.Fatalf("an absent query returned %+v (%v)", hits, err)
	}
}

func TestProjectNoteMissingIsNotAnError(t *testing.T) {
	ctx := context.Background()
	store := openStore(t)
	if note, ok, err := store.ProjectNote(ctx, "absent"); err != nil || ok || note != "" {
		t.Fatalf("a missing project note returned %q %v %v", note, ok, err)
	}
	if err := store.RemoveProjectNote(ctx, "absent"); err != nil {
		t.Fatalf("removing a missing note failed: %v", err)
	}
	if names, err := store.ProjectNoteNames(ctx); err != nil || len(names) != 0 {
		t.Fatalf("the empty note list is %v %v", names, err)
	}
}

func TestEnsureAppliesTheMigrationTwice(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	path := filepath.Join(dir, "memory.db")
	store := &Store{path: path, projectionRoot: dir}
	if err := store.Ensure(ctx); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	again := &Store{path: path, projectionRoot: dir}
	if err := again.Ensure(ctx); err != nil {
		t.Fatalf("the second Ensure failed: %v", err)
	}
	if err := again.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestEncodeAndDecodeHelpers(t *testing.T) {
	if got := encodeList(nil); got != "" {
		t.Fatalf("an empty list is %q", got)
	}
	if got := encodeList([]string{"a", "b"}); got != `["a","b"]` {
		t.Fatalf("the encoded list is %q", got)
	}
	if got := decodeList(""); got != nil {
		t.Fatalf("an empty list decoded to %v", got)
	}
	if got := decodeList("not json"); got != nil {
		t.Fatalf("a broken list decoded to %v", got)
	}
	if got := decodeList(`["a","b"]`); len(got) != 2 || got[0] != "a" {
		t.Fatalf("the decoded list is %v", got)
	}

	if got := encodeMap(nil); got != "" {
		t.Fatalf("an empty map is %q", got)
	}
	if got := encodeMap(map[string]string{"k": "v"}); got != `{"k":"v"}` {
		t.Fatalf("the encoded map is %q", got)
	}
	if got := decodeMap(""); got != nil {
		t.Fatalf("an empty map decoded to %v", got)
	}
	if got := decodeMap("not json"); got != nil {
		t.Fatalf("a broken map decoded to %v", got)
	}
	if got := decodeMap(`{"k":"v"}`); got["k"] != "v" {
		t.Fatalf("the decoded map is %v", got)
	}
	if _, err := json.Marshal(map[string]string{"k": "v"}); err != nil {
		t.Fatal(err)
	}
}

func TestEscapeLikeAndSnippet(t *testing.T) {
	if got := escapeLike(`50%_a\b`); got != `50\%\_a\\b` {
		t.Fatalf("the escaped pattern is %q", got)
	}
	if got := snippet("one\ntwo NEEDLE here\nthree", "needle"); got != "two NEEDLE here" {
		t.Fatalf("the snippet is %q", got)
	}
	if got := snippet("  only a body  ", "absent"); got != "only a body" {
		t.Fatalf("the fallback snippet is %q", got)
	}
}

func TestExtraMapRoundTripThroughTheColumns(t *testing.T) {
	ctx := context.Background()
	store := openStore(t)
	created, err := store.Create(ctx, memory.Entry{
		Title: "Extra keys", Type: memory.TypeLesson, Body: "Body.",
		Keywords: []string{"one", "two"},
		Extra:    map[string]string{"alwaysApply": "true"},
	})
	if err != nil {
		t.Fatal(err)
	}
	entries, err := store.Entries(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Extra["alwaysApply"] != "true" || len(entries[0].Keywords) != 2 {
		t.Fatalf("the columns did not round trip: %+v", entries)
	}
	created.Body = "Changed."
	created.Extra = map[string]string{"custom": "kept"}
	if err := store.Update(ctx, created); err != nil {
		t.Fatal(err)
	}
	entries, err = store.Entries(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if entries[0].Body != "Changed." || entries[0].Extra["custom"] != "kept" {
		t.Fatalf("the update lost the columns: %+v", entries[0])
	}
}

func TestEveryCommandReportsAnUnusableStore(t *testing.T) {
	ctx := context.Background()

	t.Run("missing", func(t *testing.T) {
		store := &Store{path: filepath.Join(t.TempDir(), "absent.db"), projectionRoot: t.TempDir()}
		checkUnusable(t, store)
	})

	t.Run("not a database", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "memory.db")
		if err := os.WriteFile(path, []byte("this is not a database"), 0o644); err != nil {
			t.Fatal(err)
		}
		store := &Store{path: path, projectionRoot: dir}
		checkUnusable(t, store)
	})

	_ = ctx
}

// checkUnusable asserts that every command reports an unusable store instead
// of panicking or returning a nil error.
func checkUnusable(t *testing.T, store *Store) {
	t.Helper()
	ctx := context.Background()
	entry := memory.Entry{Title: "x", Type: memory.TypeLesson, Body: "Body."}
	checks := map[string]func() error{
		"entries":            func() error { _, err := store.Entries(ctx); return err },
		"create":             func() error { _, err := store.Create(ctx, entry); return err },
		"update":             func() error { return store.Update(ctx, entry) },
		"search":             func() error { _, err := store.Search(ctx, "x", 10); return err },
		"append log":         func() error { return store.AppendLog(ctx, "line") },
		"log lines":          func() error { _, err := store.LogLines(ctx, 10); return err },
		"remove":             func() error { return store.Remove(ctx, entry) },
		"project note":       func() error { _, _, err := store.ProjectNote(ctx, "demo"); return err },
		"project note names": func() error { _, err := store.ProjectNoteNames(ctx); return err },
		"remove note":        func() error { return store.RemoveProjectNote(ctx, "demo") },
		"put project note":   func() error { return store.putProjectNote(ctx, "demo", "body") },
	}
	for name, check := range checks {
		if err := check(); err == nil {
			t.Errorf("%s did not report the unusable store", name)
		}
	}
}

func TestConnectReportsAMkdirFailure(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "file")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	store := &Store{path: filepath.Join(file, "memory.db")}
	if err := store.Ensure(context.Background()); err == nil {
		t.Fatal("the database was created under a file")
	}
}

func TestEnsureColumnReportsAClosedDatabase(t *testing.T) {
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "memory.db"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(schema); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if err := ensureColumn(db, "pinned", "INTEGER NOT NULL DEFAULT 0"); err == nil {
		t.Fatal("a closed database accepted a column")
	}
	if err := ensureColumns(db); err == nil {
		t.Fatal("a closed database accepted the migration")
	}
}
