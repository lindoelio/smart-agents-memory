package app_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lindoelio/smart-agents-memory/internal/app"
	"github.com/lindoelio/smart-agents-memory/internal/memory"
	"github.com/lindoelio/smart-agents-memory/internal/vault"
)

func createDreamEntry(t *testing.T, h *harness, entry memory.Entry) {
	t.Helper()
	entry.Type = memory.TypeLesson
	if entry.Date == "" {
		entry.Date = "2026-11-24"
	}
	if _, err := h.app.Vault.Create(context.Background(), entry); err != nil {
		t.Fatal(err)
	}
}

func TestDreamDeduplicatesAndBackfills(t *testing.T) {
	for _, backend := range []string{"obsidian", "disk", "sqlite"} {
		t.Run(backend, func(t *testing.T) {
			h := newHarness(t, backend)
			ctx := context.Background()
			if layout, ok := h.app.Vault.(vault.Layout); ok {
				if err := layout.Ensure(ctx); err != nil {
					t.Fatal(err)
				}
			}
			createDreamEntry(t, h, memory.Entry{Title: "Same title", Body: "First rule.", Keywords: []string{"one"}})
			createDreamEntry(t, h, memory.Entry{Title: "Same title", Body: "Second rule.", Keywords: []string{"two"}})
			createDreamEntry(t, h, memory.Entry{Title: "A plain lesson", Body: "A plain body."})

			report, err := h.app.Dream(ctx, app.DreamOptions{DeterministicOnly: true})
			if err != nil {
				t.Fatal(err)
			}
			if report.Merged != 1 || report.Deleted != 1 {
				t.Fatalf("the dedupe report is %+v", report)
			}
			if report.Backfilled != 2 {
				t.Fatalf("the backfill report is %+v", report)
			}
			entries, err := h.app.Vault.Entries(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if len(entries) != 2 {
				t.Fatalf("the corpus is %+v", entries)
			}
			for _, entry := range entries {
				if entry.Category == "" || entry.Description == "" {
					t.Fatalf("the entry was not backfilled: %+v", entry)
				}
			}
		})
	}
}

func TestDreamDeduplicatesBodies(t *testing.T) {
	h := newHarness(t, "obsidian")
	ctx := context.Background()
	createDreamEntry(t, h, memory.Entry{Title: "First title", Body: "Same body."})
	createDreamEntry(t, h, memory.Entry{Title: "Second title", Body: "Same body."})
	report, err := h.app.Dream(ctx, app.DreamOptions{DeterministicOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	if report.Merged != 1 || report.Deleted != 1 {
		t.Fatalf("the body dedupe report is %+v", report)
	}
	entries, err := h.app.Vault.Entries(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("the body duplicate survived: %+v", entries)
	}
}

func TestDreamLockAndStaleLock(t *testing.T) {
	h := newHarness(t, "obsidian")
	ctx := context.Background()
	lock := filepath.Join(h.root, ".consolidation.lock")
	if err := os.WriteFile(lock, []byte(`{"pid":1,"started_at":"`+time.Now().Format(time.RFC3339)+`"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := h.app.Dream(ctx, app.DreamOptions{DeterministicOnly: true}); err == nil {
		t.Fatal("a live lock was accepted")
	}
	if err := os.WriteFile(lock, []byte(`{"pid":1,"started_at":"`+time.Now().Add(-20*time.Minute).Format(time.RFC3339)+`"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := h.app.Dream(ctx, app.DreamOptions{DeterministicOnly: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(lock); !os.IsNotExist(err) {
		t.Fatal("the stale lock was not removed")
	}
}

func TestDreamLLMMerges(t *testing.T) {
	h := newHarness(t, "obsidian")
	ctx := context.Background()
	createDreamEntry(t, h, memory.Entry{Title: "Target", Project: "demo", Body: "Target body."})
	createDreamEntry(t, h, memory.Entry{Title: "Source", Project: "demo", Body: "Source body."})
	h.app.LLM = fakeRecallLLM{payload: `{"operations":[{"action":"merge","target_title":"Target","source_titles":["Source"],"new_body":"Merged rule. Why it matters.","description":"Merged","category":"practice_standard","keywords":["merged"],"usage_scenarios":["when merging"]}]}`}

	report, err := h.app.Dream(ctx, app.DreamOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if report.Merged != 1 || report.Deleted != 1 {
		t.Fatalf("the llm report is %+v", report)
	}
	entries, err := h.app.Vault.Entries(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || !strings.Contains(entries[0].Body, "Merged rule.") {
		t.Fatalf("the merged corpus is %+v", entries)
	}
	if entries[0].Category != "practice_standard" {
		t.Fatalf("the category is %q", entries[0].Category)
	}
}

func TestDreamLLMRejectsPinnedAndSecret(t *testing.T) {
	h := newHarness(t, "obsidian")
	ctx := context.Background()
	createDreamEntry(t, h, memory.Entry{Title: "Pinned target", Project: "demo", Body: "Target body.", Pinned: true})
	createDreamEntry(t, h, memory.Entry{Title: "A source", Project: "demo", Body: "Source body."})
	h.app.LLM = fakeRecallLLM{payload: `{"operations":[{"action":"merge","target_title":"Pinned target","source_titles":["A source"],"new_body":"ghp_` + strings.Repeat("a", 36) + `","description":"Merged","category":"practice_standard"}]}`}

	report, err := h.app.Dream(ctx, app.DreamOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if report.Merged != 0 || report.Deleted != 0 {
		t.Fatalf("a pinned target was merged: %+v", report)
	}
	entries, err := h.app.Vault.Entries(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("the corpus changed: %+v", entries)
	}
}

func TestDreamLLMRejectsASecret(t *testing.T) {
	h := newHarness(t, "obsidian")
	ctx := context.Background()
	createDreamEntry(t, h, memory.Entry{Title: "Target", Project: "demo", Body: "Target body."})
	createDreamEntry(t, h, memory.Entry{Title: "Source", Project: "demo", Body: "Source body."})
	h.app.LLM = fakeRecallLLM{payload: `{"operations":[{"action":"merge","target_title":"Target","source_titles":["Source"],"new_body":"ghp_` + strings.Repeat("a", 36) + `","description":"Merged","category":"practice_standard"}]}`}
	report, err := h.app.Dream(ctx, app.DreamOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if report.Merged != 0 || report.Deleted != 0 {
		t.Fatalf("a secret operation was applied: %+v", report)
	}
	entries, err := h.app.Vault.Entries(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("the corpus changed: %+v", entries)
	}
}

func TestDreamLLMRejectsNonEnglish(t *testing.T) {
	h := newHarness(t, "obsidian")
	ctx := context.Background()
	createDreamEntry(t, h, memory.Entry{Title: "Target", Project: "demo", Body: "Target body."})
	createDreamEntry(t, h, memory.Entry{Title: "Source", Project: "demo", Body: "Source body."})
	h.app.LLM = fakeRecallLLM{payload: `{"operations":[{"action":"update","target_title":"Target","new_body":"Esta regra é importante porque o projeto precisa de consistência em todas as decisões.","description":"Merged","category":"practice_standard"}]}`}
	report, err := h.app.Dream(ctx, app.DreamOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if report.Updated != 0 {
		t.Fatalf("a non-English update was applied: %+v", report)
	}
}

func TestDreamDryRunDoesNotChangeTheCorpus(t *testing.T) {
	h := newHarness(t, "obsidian")
	ctx := context.Background()
	createDreamEntry(t, h, memory.Entry{Title: "Same title", Body: "First."})
	createDreamEntry(t, h, memory.Entry{Title: "Same title", Body: "Second."})
	report, err := h.app.Dream(ctx, app.DreamOptions{DryRun: true, DeterministicOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	if report.Merged == 0 || report.Deleted == 0 {
		t.Fatalf("the dry run did not report the merge: %+v", report)
	}
	entries, err := h.app.Vault.Entries(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("the dry run changed the corpus: %+v", entries)
	}
}

func TestDreamHonorsMinDirty(t *testing.T) {
	h := newHarness(t, "obsidian")
	ctx := context.Background()
	createDreamEntry(t, h, memory.Entry{Title: "Same title", Body: "First."})
	createDreamEntry(t, h, memory.Entry{Title: "Same title", Body: "Second."})
	report, err := h.app.Dream(ctx, app.DreamOptions{DeterministicOnly: true, MinDirty: 10})
	if err != nil {
		t.Fatal(err)
	}
	if report.Merged != 0 {
		t.Fatalf("the min-dirty run changed the corpus: %+v", report)
	}
}

func TestAddIncrementsDreamDirty(t *testing.T) {
	h := newHarness(t, "obsidian")
	if _, err := h.app.Add(context.Background(), app.AddOptions{
		Scope: "lesson", Title: "A lesson", Body: "The rule.",
	}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(h.root, ".smart-agents-memory-dream.json"))
	if err != nil {
		t.Fatal(err)
	}
	var state struct {
		DirtyMutations int `json:"dirty_mutations"`
	}
	if err := json.Unmarshal(data, &state); err != nil {
		t.Fatal(err)
	}
	if state.DirtyMutations == 0 {
		t.Fatal("the add did not mark the corpus dirty")
	}
}
