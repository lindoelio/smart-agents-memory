package vault_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/lindoelio/smart-agents-memory/internal/memory"
	"github.com/lindoelio/smart-agents-memory/internal/vault"

	// The adapters register themselves; the test imports them for the
	// side effect, exactly like the command does.
	_ "github.com/lindoelio/smart-agents-memory/internal/vault/disk"
	_ "github.com/lindoelio/smart-agents-memory/internal/vault/obsidian"
	_ "github.com/lindoelio/smart-agents-memory/internal/vault/sqlite"
)

// fixture is the corpus every backend has to carry without loss.
func fixture() []memory.Entry {
	return []memory.Entry{
		{
			Title:      "CI is not the test loop",
			Type:       memory.TypeUser,
			Date:       "2026-11-07",
			Body:       "CI confirms, it does not find the bug. Test and fix locally; only push green.",
			Tier:       memory.TierHot,
			Source:     memory.DefaultSource,
			Native:     "minimax-user",
			NativeType: "user",
		},
		{
			Title:      "pipefail aborts the script on a failing command substitution",
			Type:       memory.TypeLesson,
			Date:       "2026-11-07",
			Body:       "Under set -euo pipefail, a pipeline that fails inside $(...) aborts the whole script.",
			Tier:       memory.TierCold,
			Source:     memory.DefaultSource,
			Native:     "minimax-agent",
			NativeType: "reference",
		},
		{
			Title:      "A project decision",
			Type:       memory.TypeProject,
			Date:       "2026-11-07",
			Body:       "The Flutter client lives in apps/client, not apps/web.",
			Tier:       memory.TierCold,
			Source:     memory.DefaultSource,
			Native:     "minimax-agent",
			NativeType: "reference",
		},
	}
}

// newBackend builds a gateway of the given kind on a temporary directory.
func newBackend(t *testing.T, kind string) vault.Vault {
	t.Helper()
	root := t.TempDir()
	cfg := vault.Config{Backend: kind, MemoryHome: root}
	switch kind {
	case "sqlite":
		cfg.SQLitePath = filepath.Join(root, "memory.db")
	case "obsidian", "disk":
	default:
		t.Fatalf("unknown backend %q", kind)
	}
	store, err := vault.Open(cfg)
	if err != nil {
		t.Fatalf("cannot open the %s gateway: %v", kind, err)
	}
	return store
}

// write stores the fixture in a gateway and reads it back.
func write(t *testing.T, store vault.Vault) []memory.Entry {
	t.Helper()
	ctx := context.Background()
	for _, e := range fixture() {
		if _, err := store.Create(ctx, e); err != nil {
			t.Fatalf("cannot create %q: %v", e.Title, err)
		}
	}
	entries, err := store.Entries(ctx)
	if err != nil {
		t.Fatalf("cannot read the entries: %v", err)
	}
	return entries
}

// check compares the entries with the fixture.
func check(t *testing.T, kind string, entries []memory.Entry) {
	t.Helper()
	if len(entries) != len(fixture()) {
		t.Fatalf("%s: %d entries, want %d", kind, len(entries), len(fixture()))
	}
	want := map[string]memory.Entry{}
	for _, e := range fixture() {
		want[memory.NormTitle(e.Title)] = e
	}
	for _, e := range entries {
		original, ok := want[memory.NormTitle(e.Title)]
		if !ok {
			t.Fatalf("%s: unexpected entry %q", kind, e.Title)
		}
		if e.Body != original.Body {
			t.Fatalf("%s: the body of %q changed:\n%q\n%q", kind, e.Title, e.Body, original.Body)
		}
		if e.Type != original.Type {
			t.Fatalf("%s: the type of %q is %q, want %q", kind, e.Title, e.Type, original.Type)
		}
		if e.Tier != original.Tier {
			t.Fatalf("%s: the tier of %q is %q, want %q", kind, e.Title, e.Tier, original.Tier)
		}
		if e.Date != original.Date {
			t.Fatalf("%s: the date of %q is %q, want %q", kind, e.Title, e.Date, original.Date)
		}
		if e.Native != original.Native {
			t.Fatalf("%s: the native target of %q is %q, want %q", kind, e.Title, e.Native, original.Native)
		}
		if e.ModTime.IsZero() {
			t.Fatalf("%s: %q has no modification time", kind, e.Title)
		}
	}
}

// TestBackendsCarryTheSameCorpus is the architectural test: every gateway
// carries the same corpus without loss.
func TestBackendsCarryTheSameCorpus(t *testing.T) {
	for _, kind := range []string{"obsidian", "disk", "sqlite"} {
		t.Run(kind, func(t *testing.T) {
			store := newBackend(t, kind)
			entries := write(t, store)
			check(t, kind, entries)
		})
	}
}

// TestTheCorpusTravelsBetweenBackends moves the corpus from one gateway to
// another, and back.
func TestTheCorpusTravelsBetweenBackends(t *testing.T) {
	ctx := context.Background()
	source := newBackend(t, "obsidian")
	entries := write(t, source)

	target := newBackend(t, "sqlite")
	for _, e := range entries {
		e.Path = ""
		if _, err := target.Create(ctx, e); err != nil {
			t.Fatalf("cannot copy %q: %v", e.Title, err)
		}
	}
	moved, err := target.Entries(ctx)
	if err != nil {
		t.Fatalf("cannot read the copied corpus: %v", err)
	}
	check(t, "obsidian -> sqlite", moved)

	back := newBackend(t, "disk")
	for _, e := range moved {
		e.Path = ""
		if _, err := back.Create(ctx, e); err != nil {
			t.Fatalf("cannot copy %q: %v", e.Title, err)
		}
	}
	round, err := back.Entries(ctx)
	if err != nil {
		t.Fatalf("cannot read the copied corpus: %v", err)
	}
	check(t, "sqlite -> disk", round)
}

// TestTheObsidianGatewayFindsNotesAnywhere is the difference between the
// Obsidian gateway and the plain directory one.
func TestTheObsidianGatewayFindsNotesAnywhere(t *testing.T) {
	root := t.TempDir()
	note := `---
title: "A note outside the corpus folder"
type: lesson
date: 2026-11-07
---

## A note outside the corpus folder

It lives in a folder of the vault.
`
	if err := os.MkdirAll(filepath.Join(root, "notes"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "notes", "a-note.md"), []byte(note), 0o644); err != nil {
		t.Fatal(err)
	}

	obsidianStore, err := vault.Open(vault.Config{Backend: "obsidian", MemoryHome: root})
	if err != nil {
		t.Fatal(err)
	}
	entries, err := obsidianStore.Entries(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Title != "A note outside the corpus folder" {
		t.Fatalf("the obsidian gateway did not find the note: %+v", entries)
	}

	diskStore, err := vault.Open(vault.Config{Backend: "disk", MemoryHome: root})
	if err != nil {
		t.Fatal(err)
	}
	entries, err = diskStore.Entries(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("the disk gateway found a note outside corpus/: %+v", entries)
	}
}

// TestTheDiskGatewayIgnoresNotesWithoutAType keeps the project notes and
// the projections out of the corpus.
func TestTheDiskGatewayIgnoresNotesWithoutAType(t *testing.T) {
	root := t.TempDir()
	corpus := filepath.Join(root, "corpus", "lesson")
	if err := os.MkdirAll(corpus, 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(name, content string) {
		if err := os.WriteFile(filepath.Join(corpus, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("a-lesson.md", "---\ntitle: \"A lesson\"\ntype: lesson\ndate: 2026-11-07\n---\n\nBody.\n")
	write("a-project-note.md", "---\ndescription: A project\ntype: project\ndate: 2026-11-07\n---\n\n# A project\n")

	store, err := vault.Open(vault.Config{Backend: "disk", MemoryHome: root})
	if err != nil {
		t.Fatal(err)
	}
	entries, err := store.Entries(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Title != "A lesson" {
		t.Fatalf("the corpus is wrong: %+v", entries)
	}
}

// TestTheLogIsAppendOnly checks that the write log grows and reads back.
func TestTheLogIsAppendOnly(t *testing.T) {
	for _, kind := range []string{"obsidian", "disk", "sqlite"} {
		t.Run(kind, func(t *testing.T) {
			store := newBackend(t, kind)
			ctx := context.Background()
			for _, line := range []string{"add | lesson | One | source", "add | lesson | Two | source"} {
				if err := store.AppendLog(ctx, line); err != nil {
					t.Fatalf("cannot append to the log: %v", err)
				}
			}
			lines, err := store.LogLines(ctx, 10)
			if err != nil {
				t.Fatalf("cannot read the log: %v", err)
			}
			if len(lines) != 2 {
				t.Fatalf("the log has %d lines, want 2: %v", len(lines), lines)
			}
		})
	}
}

// TestSearchFindsTheEntry checks the search of every gateway.
func TestSearchFindsTheEntry(t *testing.T) {
	for _, kind := range []string{"obsidian", "disk", "sqlite"} {
		t.Run(kind, func(t *testing.T) {
			store := newBackend(t, kind)
			write(t, store)
			hits, err := store.Search(context.Background(), "pipefail", 10)
			if err != nil {
				t.Fatalf("cannot search: %v", err)
			}
			if len(hits) == 0 {
				t.Fatal("the search found nothing")
			}
		})
	}
}

// TestCreateDoesNotOverwrite checks that two entries with the same title
// get two files.
func TestCreateDoesNotOverwrite(t *testing.T) {
	store := newBackend(t, "obsidian")
	ctx := context.Background()
	first, err := store.Create(ctx, memory.Entry{Title: "Same title", Type: memory.TypeLesson, Body: "One."})
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.Create(ctx, memory.Entry{Title: "Same title", Type: memory.TypeLesson, Body: "Two."})
	if err != nil {
		t.Fatal(err)
	}
	if first.Path == second.Path {
		t.Fatalf("the second entry overwrote the first: %s", first.Path)
	}
}

// TestTheModificationTimeIsKept checks that every gateway reports when an
// entry changed, which is what the reconciliation rule needs.
func TestTheModificationTimeIsKept(t *testing.T) {
	for _, kind := range []string{"obsidian", "disk", "sqlite"} {
		t.Run(kind, func(t *testing.T) {
			store := newBackend(t, kind)
			entries := write(t, store)
			for _, e := range entries {
				if e.ModTime.IsZero() {
					t.Fatalf("%s: %q has no modification time", kind, e.Title)
				}
				if time.Since(e.ModTime) > time.Hour {
					t.Fatalf("%s: the modification time of %q is wrong: %v", kind, e.Title, e.ModTime)
				}
			}
		})
	}
}
