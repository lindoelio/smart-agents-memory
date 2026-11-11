package sqlite

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/lindoelio/smart-agents-memory/internal/memory"
	"github.com/lindoelio/smart-agents-memory/internal/vault"
)

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
