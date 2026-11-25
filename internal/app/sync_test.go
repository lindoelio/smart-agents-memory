package app_test

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lindoelio/smart-agents-memory/internal/app"
	"github.com/lindoelio/smart-agents-memory/internal/sources"
	_ "modernc.org/sqlite"
)

func writeCursorSession(t *testing.T, home string, lines ...string) string {
	t.Helper()
	path := filepath.Join(home, ".cursor", "projects", "p", "agent-transcripts", "s", "s.jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestSyncBackfillExtractsAndResumes(t *testing.T) {
	h := newHarness(t, "obsidian")
	ctx := context.Background()
	writeCursorSession(t, h.home,
		`{"role":"user","content":"Remember the sync rule. Why: sessions are the source."}`,
		`{"role":"assistant","content":"noted"}`)
	fake := &extractLLM{payload: `{"candidates":[{"scope":"lesson","title":"A synced rule","body":"The rule. Why it matters."}]}`}
	h.app.LLM = fake

	report, err := h.app.Sync(ctx, app.SyncOptions{Backfill: true})
	if err != nil {
		t.Fatal(err)
	}
	if fake.calls != 1 || report.Added != 1 || report.Changed != 1 {
		t.Fatalf("the first sync is %+v calls=%d", report, fake.calls)
	}
	entries, err := h.app.Vault.Entries(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("the corpus is %+v", entries)
	}

	second, err := h.app.Sync(ctx, app.SyncOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if fake.calls != 1 || second.Changed != 0 {
		t.Fatalf("the second sync re-extracted: %+v calls=%d", second, fake.calls)
	}
}

func TestSyncSeedsTheFirstRun(t *testing.T) {
	h := newHarness(t, "obsidian")
	ctx := context.Background()
	path := writeCursorSession(t, h.home,
		`{"role":"user","content":"old history"}`)
	fake := &extractLLM{payload: `{"candidates":[]}`}
	h.app.LLM = fake

	report, err := h.app.Sync(ctx, app.SyncOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !report.Seeded || fake.calls != 0 {
		t.Fatalf("the seed run is %+v calls=%d", report, fake.calls)
	}
	if _, err := os.Stat(filepath.Join(h.root, ".sync-state.json")); err != nil {
		t.Fatal(err)
	}

	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(`{"role":"user","content":"a new rule after seed"}` + "\n"); err != nil {
		f.Close()
		t.Fatal(err)
	}
	f.Close()
	if _, err := h.app.Sync(ctx, app.SyncOptions{}); err != nil {
		t.Fatal(err)
	}
	if fake.calls != 1 {
		t.Fatalf("the appended turn produced %d calls", fake.calls)
	}
}

func TestSyncDryRunDoesNotCallOrWrite(t *testing.T) {
	h := newHarness(t, "obsidian")
	ctx := context.Background()
	writeCursorSession(t, h.home, `{"role":"user","content":"a dry rule"}`)
	fake := &extractLLM{payload: `{"candidates":[]}`}
	h.app.LLM = fake

	report, err := h.app.Sync(ctx, app.SyncOptions{DryRun: true, Backfill: true})
	if err != nil {
		t.Fatal(err)
	}
	if fake.calls != 0 || report.Turns == 0 {
		t.Fatalf("the dry run is %+v calls=%d", report, fake.calls)
	}
	if _, err := os.Stat(filepath.Join(h.root, ".sync-state.json")); !os.IsNotExist(err) {
		t.Fatal("the dry run wrote state")
	}
}

func TestSyncRequiresAnLLMForBackfill(t *testing.T) {
	h := newHarness(t, "obsidian")
	ctx := context.Background()
	writeCursorSession(t, h.home, `{"role":"user","content":"a rule with no llm"}`)
	report, err := h.app.Sync(ctx, app.SyncOptions{Backfill: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Errors) == 0 {
		t.Fatalf("the missing LLM was ignored: %+v", report)
	}
}

func TestSyncSourceFilterAndUnknown(t *testing.T) {
	h := newHarness(t, "obsidian")
	ctx := context.Background()
	writeCursorSession(t, h.home, `{"role":"user","content":"a filtered rule"}`)
	h.app.LLM = &extractLLM{payload: `{"candidates":[]}`}
	report, err := h.app.Sync(ctx, app.SyncOptions{Backfill: true, SourceID: "cursor"})
	if err != nil {
		t.Fatal(err)
	}
	if report.Sources != 1 {
		t.Fatalf("the filtered run is %+v", report)
	}
	if _, err := h.app.Sync(ctx, app.SyncOptions{SourceID: "nope"}); err == nil {
		t.Fatal("an unknown source was accepted")
	}
}

func TestSyncReadsOpenCodeReadOnly(t *testing.T) {
	h := newHarness(t, "obsidian")
	ctx := context.Background()
	dbPath := filepath.Join(h.home, ".local", "share", "opencode", "opencode.db")
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE session_message (session_id TEXT, seq INTEGER, type TEXT, data TEXT)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO session_message VALUES ('s1', 1, 'user', '{"text":"remember the sqlite rule"}')`); err != nil {
		t.Fatal(err)
	}
	db.Close()
	fake := &extractLLM{payload: `{"candidates":[{"scope":"lesson","title":"A sqlite rule","body":"The rule. Why it matters."}]}`}
	h.app.LLM = fake
	report, err := h.app.Sync(ctx, app.SyncOptions{Backfill: true, SourceID: "opencode"})
	if err != nil {
		t.Fatal(err)
	}
	if fake.calls != 1 || report.Added != 1 {
		t.Fatalf("the sqlite sync is %+v calls=%d", report, fake.calls)
	}
}

func TestSyncWriteFailureDoesNotAdvanceTheFile(t *testing.T) {
	h := newHarness(t, "obsidian")
	ctx := context.Background()
	writeCursorSession(t, h.home, `{"role":"user","content":"a lost rule"}`)
	h.app.LLM = &extractLLM{payload: `{"candidates":[{"scope":"lesson","title":"A lost rule","body":"The rule. Why it matters."}]}`}
	real := h.app.Vault
	h.app.Vault = &failingVault{Vault: real, err: errors.New("disk full")}
	report, err := h.app.Sync(ctx, app.SyncOptions{Backfill: true, SourceID: "cursor"})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Errors) == 0 {
		t.Fatalf("the write failure was ignored: %+v", report)
	}
	state, err := sources.LoadState(filepath.Join(h.root, ".sync-state.json"))
	if err != nil {
		t.Fatal(err)
	}
	for key := range state.Files {
		if strings.Contains(key, "cursor") {
			t.Fatalf("the cursor advanced: %v", state.Files)
		}
	}
	h.app.Vault = real
	if _, err := h.app.Sync(ctx, app.SyncOptions{Backfill: true, SourceID: "cursor"}); err != nil {
		t.Fatal(err)
	}
	entries, err := real.Entries(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("the retry left %+v", entries)
	}
}

func TestSourceStatuses(t *testing.T) {
	h := newHarness(t, "obsidian")
	if err := os.MkdirAll(filepath.Join(h.home, ".cursor", "projects"), 0o755); err != nil {
		t.Fatal(err)
	}
	statuses := h.app.SourceStatuses()
	if len(statuses) != 7 {
		t.Fatalf("the statuses are %+v", statuses)
	}
	found := false
	for _, status := range statuses {
		if status.ID == "cursor" {
			found = true
			if !status.Exists {
				t.Fatalf("the cursor source is %+v", status)
			}
		}
	}
	if !found {
		t.Fatal("the cursor source is missing")
	}
}
