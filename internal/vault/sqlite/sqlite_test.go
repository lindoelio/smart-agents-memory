package sqlite

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/lindoelio/smart-agents-memory/internal/memory"
	"github.com/lindoelio/smart-agents-memory/internal/vault"
)

func TestPinnedColumnIsAddedToAnExistingDatabase(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	path := filepath.Join(dir, "memory.db")

	oldSchema := `
CREATE TABLE entries (
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
);`
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(oldSchema); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO entries (title, type, body) VALUES ('An old lesson', 'lesson', 'Old body.')`); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	opened, err := vault.Open(vault.Config{
		Backend:        "sqlite",
		SQLitePath:     path,
		ProjectionRoot: dir,
	})
	if err != nil {
		t.Fatal(err)
	}
	entries, err := opened.Entries(ctx)
	if err != nil {
		t.Fatalf("the old database did not open after the migration: %v", err)
	}
	if len(entries) != 1 || entries[0].Pinned {
		t.Fatalf("the old entry is %+v", entries)
	}

	if _, err := opened.Create(ctx, memory.Entry{
		Title: "A pinned lesson", Type: memory.TypeLesson, Body: "The rule.", Pinned: true,
		Project: "my-repo", Worktree: "feature",
		Category: "testing_standard", UsageScenarios: []string{"before a release"},
	}); err != nil {
		t.Fatal(err)
	}
	entries, err = opened.Entries(ctx)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, e := range entries {
		if e.Title == "A pinned lesson" {
			found = e.Pinned && e.Project == "my-repo" && e.Worktree == "feature" &&
				e.Category == "testing_standard" && len(e.UsageScenarios) == 1
		}
	}
	if !found {
		t.Fatal("the pinned, scope and taxonomy columns did not survive the round trip")
	}
	store := opened.(*Store)
	var index string
	if err := store.db.QueryRowContext(ctx, `SELECT name FROM sqlite_master WHERE type='index' AND name='entries_project'`).Scan(&index); err != nil {
		t.Fatalf("the project index was not created: %v", err)
	}
}

func TestWorktreeNotesAndNames(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	opened, err := vault.Open(vault.Config{
		Backend:        "sqlite",
		SQLitePath:     filepath.Join(dir, "memory.db"),
		ProjectionRoot: dir,
	})
	if err != nil {
		t.Fatal(err)
	}
	store := opened.(*Store)
	if err := store.Ensure(ctx); err != nil {
		t.Fatal(err)
	}
	if err := store.putProjectNote(ctx, "demo", "repo-wide"); err != nil {
		t.Fatal(err)
	}
	if err := store.putProjectNote(ctx, "demo@feature", "branch-local"); err != nil {
		t.Fatal(err)
	}
	note, ok, err := store.WorktreeNote(ctx, "demo", "feature")
	if err != nil || !ok || note != "branch-local" {
		t.Fatalf("the worktree note is %q %v %v", note, ok, err)
	}
	names, err := store.ProjectNoteNames(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != 2 || names[0] != "demo" || names[1] != "demo@feature" {
		t.Fatalf("the note names are %v", names)
	}
	if err := store.RemoveProjectNote(ctx, "demo@feature"); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := store.WorktreeNote(ctx, "demo", "feature"); err != nil || ok {
		t.Fatalf("the worktree note survived: %v %v", ok, err)
	}
}

func TestRenameRemoveProjectNoteAndLiteralSearch(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	opened, err := vault.Open(vault.Config{
		Backend:        "sqlite",
		SQLitePath:     filepath.Join(dir, "memory.db"),
		ProjectionRoot: dir,
	})
	if err != nil {
		t.Fatal(err)
	}
	store := opened.(*Store)
	if err := store.Ensure(ctx); err != nil {
		t.Fatal(err)
	}

	created, err := store.Create(ctx, memory.Entry{
		Title: "100% done", Type: memory.TypeLesson, Body: "a_b stays literal",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Create(ctx, memory.Entry{
		Title: "other", Type: memory.TypeLesson, Body: "nothing special",
	}); err != nil {
		t.Fatal(err)
	}

	renamed, err := store.Rename(ctx, created, "Almost done")
	if err != nil {
		t.Fatal(err)
	}
	if renamed.Title != "Almost done" || renamed.Path != created.Path {
		t.Fatalf("the rename is %+v", renamed)
	}

	hits, err := store.Search(ctx, "%", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 0 {
		t.Fatalf("a literal percent matched %d rows", len(hits))
	}
	hits, err = store.Search(ctx, "_", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || hits[0].Title != "Almost done" {
		t.Fatalf("the underscore search is %+v", hits)
	}

	if err := store.putProjectNote(ctx, "dizer", "The client lives in apps/client."); err != nil {
		t.Fatal(err)
	}
	note, ok, err := store.ProjectNote(ctx, "dizer")
	if err != nil || !ok || note != "The client lives in apps/client." {
		t.Fatalf("the project note is %q %v %v", note, ok, err)
	}

	if err := store.Remove(ctx, renamed); err != nil {
		t.Fatal(err)
	}
	entries, err := store.Entries(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Title != "other" {
		t.Fatalf("the corpus is %+v", entries)
	}
}
