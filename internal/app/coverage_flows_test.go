package app_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lindoelio/smart-agents-memory/internal/app"
	"github.com/lindoelio/smart-agents-memory/internal/memory"
	"github.com/lindoelio/smart-agents-memory/internal/projection"
)

func TestExtractRejectsEveryCandidateGate(t *testing.T) {
	cases := []struct {
		name    string
		payload string
		seed    []app.AddOptions
		added   int
	}{
		{
			"invalid scope",
			`{"candidates":[{"scope":"bogus","title":"A rule","body":"The rule. Why it matters."}]}`,
			nil, 0,
		},
		{
			"empty title",
			`{"candidates":[{"scope":"lesson","title":"  ","body":"The rule. Why it matters."}]}`,
			nil, 0,
		},
		{
			"empty body",
			`{"candidates":[{"scope":"lesson","title":"A rule","body":"  "}]}`,
			nil, 0,
		},
		{
			"non English",
			`{"candidates":[{"scope":"lesson","title":"Uma regra","body":"Esta regra existe porque o projeto precisa de consistencia em todas as decisoes."}]}`,
			nil, 0,
		},
		{
			"secret",
			`{"candidates":[{"scope":"lesson","title":"A leaked key","body":"ghp_` + strings.Repeat("a", 36) + `"}]}`,
			nil, 0,
		},
		{
			"pinned title",
			`{"candidates":[{"scope":"lesson","title":"Pinned rule","body":"A new body. Why it matters."}]}`,
			[]app.AddOptions{{Scope: "lesson", Title: "Pinned rule", Body: "The curated rule.", Pinned: true}},
			0,
		},
		{
			"hot title",
			`{"candidates":[{"scope":"lesson","title":"Hot rule","body":"A new body. Why it matters."}]}`,
			[]app.AddOptions{{Scope: "user", Title: "Hot rule", Body: "The hot rule.", Tier: "hot"}},
			0,
		},
		{
			"invalid category is dropped and the candidate accepted",
			`{"candidates":[{"scope":"lesson","title":"A rule","body":"The rule. Why it matters.","category":"not_a_category"}]}`,
			nil, 1,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t, "obsidian")
			ctx := context.Background()
			for _, seed := range tc.seed {
				if _, err := h.app.Add(ctx, seed); err != nil {
					t.Fatal(err)
				}
			}
			h.app.LLM = &extractLLM{payload: tc.payload}
			report, err := h.app.Extract(ctx, app.ExtractOptions{
				Transcript: `{"role":"user","content":"extract this"}`,
				SessionID:  "gate",
			})
			if err != nil {
				t.Fatal(err)
			}
			if report.Candidates != 1 || report.Added != tc.added {
				t.Fatalf("the report is %+v, want added=%d", report, tc.added)
			}
			if tc.added == 0 && len(report.Skipped) != 1 {
				t.Fatalf("the rejection was not reported: %+v", report)
			}
			entries, err := h.app.Vault.Entries(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if len(entries) != len(tc.seed)+tc.added {
				t.Fatalf("the corpus is %+v", entries)
			}
			if tc.name == "invalid category is dropped and the candidate accepted" && entries[0].Category != "" {
				t.Fatalf("the invalid category was stored: %+v", entries[0])
			}
		})
	}
}

func TestExtractResolvesTheProjectCandidateScope(t *testing.T) {
	h := newHarness(t, "obsidian")
	ctx := context.Background()
	repo := newRepo(t)
	h.app.LLM = &extractLLM{payload: `{"candidates":[{"scope":"project","title":"A project rule","body":"The rule. Why it matters."}]}`}
	report, err := h.app.Extract(ctx, app.ExtractOptions{
		Transcript: `{"role":"user","content":"extract this"}`,
		SessionID:  "project", CWD: repo,
	})
	if err != nil {
		t.Fatal(err)
	}
	if report.Added != 1 {
		t.Fatalf("the report is %+v", report)
	}
	entries, err := h.app.Vault.Entries(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Project != memory.Slugify("repo") {
		t.Fatalf("the project was not resolved: %+v", entries)
	}
}

func TestExtractNeedsAnLLM(t *testing.T) {
	h := newHarness(t, "obsidian")
	report, err := h.app.Extract(context.Background(), app.ExtractOptions{
		Transcript: `{"role":"user","content":"extract this"}`,
	})
	if err == nil || !strings.Contains(err.Error(), "needs an LLM client") {
		t.Fatalf("the missing LLM is %v", err)
	}
	if report.Candidates != 0 {
		t.Fatalf("the report is %+v", report)
	}
}

func TestExtractReportsTheModelError(t *testing.T) {
	h := newHarness(t, "obsidian")
	h.app.LLM = fakeRecallLLM{err: errors.New("model failed")}
	if _, err := h.app.Extract(context.Background(), app.ExtractOptions{
		Transcript: `{"role":"user","content":"extract this"}`,
	}); err == nil || !strings.Contains(err.Error(), "model failed") {
		t.Fatalf("the model error is %v", err)
	}
}

func TestExtractRejectsAnEmptyOrMissingTranscript(t *testing.T) {
	h := newHarness(t, "obsidian")
	ctx := context.Background()
	h.app.LLM = &extractLLM{payload: `{"candidates":[]}`}
	if _, err := h.app.Extract(ctx, app.ExtractOptions{Transcript: "   "}); err == nil {
		t.Fatal("an empty transcript was accepted")
	}
	if _, err := h.app.Extract(ctx, app.ExtractOptions{
		TranscriptPath: filepath.Join(t.TempDir(), "missing.jsonl"),
	}); err == nil {
		t.Fatal("a missing transcript was accepted")
	}
}

func TestExtractTurnsWithoutAUserTurnIsANoOp(t *testing.T) {
	h := newHarness(t, "obsidian")
	report, err := h.app.ExtractTurns(context.Background(), nil, app.ExtractOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if report.SkippedReason != "no_new_user_turns" {
		t.Fatalf("the report is %+v", report)
	}
}

func TestExtractFailsWhenTheCursorCannotBeRead(t *testing.T) {
	h := newHarness(t, "obsidian")
	ctx := context.Background()
	if err := os.WriteFile(filepath.Join(h.root, ".extract-cursor.json"), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	h.app.LLM = &extractLLM{payload: `{"candidates":[]}`}
	if _, err := h.app.Extract(ctx, app.ExtractOptions{
		Transcript: `{"role":"user","content":"extract this"}`,
		SessionID:  "cursor",
	}); err == nil {
		t.Fatal("the invalid cursor was ignored")
	}
}

func TestExtractFailsWhenTheCursorCannotBeSaved(t *testing.T) {
	h := newHarness(t, "obsidian")
	ctx := context.Background()
	h.app.LLM = &extractLLM{payload: `{"candidates":[]}`}
	if err := os.Chmod(h.root, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(h.root, 0o755) })
	if _, err := h.app.Extract(ctx, app.ExtractOptions{
		Transcript: `{"role":"user","content":"extract this"}`,
		SessionID:  "cursor",
	}); err == nil {
		t.Fatal("the cursor save failure was swallowed")
	}
}

func TestExtractPropagatesTheAutoDreamFailure(t *testing.T) {
	h := newHarness(t, "obsidian")
	ctx := context.Background()
	lock := filepath.Join(h.root, ".consolidation.lock")
	if err := os.WriteFile(lock, []byte(`{"pid":1,"started_at":"`+time.Now().Format(time.RFC3339)+`"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	h.app.LLM = &extractLLM{payload: `{"candidates":[{"scope":"lesson","title":"A dreamy rule","body":"The rule. Why it matters."}]}`}
	_, err := h.app.Extract(ctx, app.ExtractOptions{
		Transcript: `{"role":"user","content":"extract this"}`,
		SessionID:  "dream", AutoDream: true, DreamThreshold: 1,
	})
	if err == nil || !strings.Contains(err.Error(), "already running") {
		t.Fatalf("the auto-dream failure is %v", err)
	}
}

func TestWatchOnceReportsMissingAndUnreadableFiles(t *testing.T) {
	t.Run("missing directory", func(t *testing.T) {
		h := newHarness(t, "obsidian")
		report, err := h.app.WatchOnce(context.Background(), app.WatchOptions{
			Dirs: []string{filepath.Join(t.TempDir(), "missing")},
		})
		if err != nil {
			t.Fatal(err)
		}
		if report.Files != 0 || len(report.Errors) != 0 {
			t.Fatalf("the report is %+v", report)
		}
	})
	t.Run("unreadable file", func(t *testing.T) {
		h := newHarness(t, "obsidian")
		dir := t.TempDir()
		file := filepath.Join(dir, "locked.jsonl")
		if err := os.WriteFile(file, []byte(`{"role":"user","content":"x"}`), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(file, 0o000); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(file, 0o600) })
		report, err := h.app.WatchOnce(context.Background(), app.WatchOptions{Dirs: []string{dir}})
		if err != nil {
			t.Fatal(err)
		}
		if len(report.Errors) != 1 || report.Files != 0 {
			t.Fatalf("the report is %+v", report)
		}
	})
	t.Run("extraction error is reported", func(t *testing.T) {
		h := newHarness(t, "obsidian")
		dir := t.TempDir()
		file := filepath.Join(dir, "session.jsonl")
		if err := os.WriteFile(file, []byte(`{"role":"user","content":"a durable rule"}`), 0o644); err != nil {
			t.Fatal(err)
		}
		report, err := h.app.WatchOnce(context.Background(), app.WatchOptions{Dirs: []string{dir}})
		if err != nil {
			t.Fatal(err)
		}
		if report.Files != 1 || len(report.Errors) != 1 {
			t.Fatalf("the report is %+v", report)
		}
	})
}

func TestDreamRecoversFromABrokenStateFile(t *testing.T) {
	h := newHarness(t, "obsidian")
	ctx := context.Background()
	path := filepath.Join(h.root, ".smart-agents-memory-dream.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := h.app.Add(ctx, app.AddOptions{
		Scope: "lesson", Title: "A rule", Body: "The rule.",
	}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"dirty_mutations": 1`) {
		t.Fatalf("the state was not rebuilt: %s", data)
	}
}

func TestDreamFailsOnAVaultReadError(t *testing.T) {
	h := newHarness(t, "obsidian")
	h.app.Vault = &faultyVault{Vault: h.app.Vault, entriesErr: errors.New("entries failed")}
	if _, err := h.app.Dream(context.Background(), app.DreamOptions{DeterministicOnly: true}); err == nil {
		t.Fatal("the read failure was swallowed")
	}
}

func TestDreamFailsWhenTheGatewayCannotRemove(t *testing.T) {
	h := newHarness(t, "obsidian")
	createDreamEntry(t, h, memory.Entry{Title: "Same title", Body: "First rule."})
	createDreamEntry(t, h, memory.Entry{Title: "Same title", Body: "Second rule."})
	h.app.Vault = &faultyVault{Vault: h.app.Vault}
	_, err := h.app.Dream(context.Background(), app.DreamOptions{DeterministicOnly: true})
	if err == nil || !strings.Contains(err.Error(), "cannot remove entries") {
		t.Fatalf("the removal capability is %v", err)
	}
}

func TestDreamFailsWhenTheBackfillCannotWrite(t *testing.T) {
	h := newHarness(t, "obsidian")
	createDreamEntry(t, h, memory.Entry{Title: "A plain lesson", Body: "A plain body."})
	h.app.Vault = &faultyVault{Vault: h.app.Vault, updateErr: errors.New("update failed")}
	if _, err := h.app.Dream(context.Background(), app.DreamOptions{DeterministicOnly: true}); err == nil {
		t.Fatal("the backfill failure was swallowed")
	}
}

func TestDreamReportsTheModelError(t *testing.T) {
	h := newHarness(t, "obsidian")
	ctx := context.Background()
	createDreamEntry(t, h, memory.Entry{Title: "One", Project: "demo", Body: "First body.", Category: "practice_standard", Description: "One"})
	createDreamEntry(t, h, memory.Entry{Title: "Two", Project: "demo", Body: "Second body.", Category: "practice_standard", Description: "Two"})
	h.app.LLM = fakeRecallLLM{err: errors.New("model failed")}
	if _, err := h.app.Dream(ctx, app.DreamOptions{}); err == nil {
		t.Fatal("the model failure was swallowed")
	}
}

func TestDreamPropagatesTheApplyWriteFailure(t *testing.T) {
	h := newHarness(t, "obsidian")
	ctx := context.Background()
	createDreamEntry(t, h, memory.Entry{Title: "Target", Project: "demo", Body: "Target body.", Category: "practice_standard", Description: "Target"})
	createDreamEntry(t, h, memory.Entry{Title: "Source", Project: "demo", Body: "Source body.", Category: "practice_standard", Description: "Source"})
	h.app.LLM = fakeRecallLLM{payload: `{"operations":[{"action":"update","target_title":"Target","new_body":"An updated rule. Why it matters.","description":"Updated","category":"practice_standard"}]}`}
	h.app.Vault = &faultyVault{Vault: h.app.Vault, updateErr: errors.New("update failed")}
	if _, err := h.app.Dream(ctx, app.DreamOptions{}); err == nil {
		t.Fatal("the apply write failure was swallowed")
	}
}

func TestDreamFailsWhenAMergeCannotRemove(t *testing.T) {
	h := newHarness(t, "obsidian")
	ctx := context.Background()
	createDreamEntry(t, h, memory.Entry{Title: "Target", Project: "demo", Body: "Target body.", Category: "practice_standard", Description: "Target"})
	createDreamEntry(t, h, memory.Entry{Title: "Source", Project: "demo", Body: "Source body.", Category: "practice_standard", Description: "Source"})
	h.app.LLM = fakeRecallLLM{payload: `{"operations":[{"action":"merge","target_title":"Target","source_titles":["Source"],"new_body":"A merged rule. Why it matters.","description":"Merged","category":"practice_standard"}]}`}
	h.app.Vault = &faultyVault{Vault: h.app.Vault}
	if _, err := h.app.Dream(ctx, app.DreamOptions{}); err == nil || !strings.Contains(err.Error(), "cannot remove entries") {
		t.Fatalf("the merge capability is %v", err)
	}
}

func TestDreamPropagatesAMergeRemovalFailure(t *testing.T) {
	h := newHarness(t, "obsidian")
	ctx := context.Background()
	createDreamEntry(t, h, memory.Entry{Title: "Target", Project: "demo", Body: "Target body.", Category: "practice_standard", Description: "Target"})
	createDreamEntry(t, h, memory.Entry{Title: "Source", Project: "demo", Body: "Source body.", Category: "practice_standard", Description: "Source"})
	h.app.LLM = fakeRecallLLM{payload: `{"operations":[{"action":"merge","target_title":"Target","source_titles":["Source"],"new_body":"A merged rule. Why it matters.","description":"Merged","category":"practice_standard"}]}`}
	h.app.Vault = &removerErrorVault{Vault: h.app.Vault}
	if _, err := h.app.Dream(ctx, app.DreamOptions{}); err == nil {
		t.Fatal("the removal failure was swallowed")
	}
}

func TestDreamDryRunReportsAnLLMMerge(t *testing.T) {
	h := newHarness(t, "obsidian")
	ctx := context.Background()
	createDreamEntry(t, h, memory.Entry{Title: "Target", Project: "demo", Body: "Target body.", Category: "practice_standard", Description: "Target"})
	createDreamEntry(t, h, memory.Entry{Title: "Source", Project: "demo", Body: "Source body.", Category: "practice_standard", Description: "Source"})
	h.app.LLM = fakeRecallLLM{payload: `{"operations":[{"action":"merge","target_title":"Target","source_titles":["Source"],"new_body":"A merged rule. Why it matters.","description":"Merged","category":"practice_standard"}]}`}
	report, err := h.app.Dream(ctx, app.DreamOptions{DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	if report.Merged != 1 || report.Deleted != 1 {
		t.Fatalf("the dry run is %+v", report)
	}
	entries, err := h.app.Vault.Entries(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("the dry run changed the corpus: %+v", entries)
	}
}

func TestDreamSkipsInvalidOperations(t *testing.T) {
	h := newHarness(t, "obsidian")
	ctx := context.Background()
	createDreamEntry(t, h, memory.Entry{Title: "Target", Project: "demo", Body: "Target body.", Category: "practice_standard", Description: "Target"})
	createDreamEntry(t, h, memory.Entry{Title: "Source", Project: "demo", Body: "Source body.", Category: "practice_standard", Description: "Source"})
	createDreamEntry(t, h, memory.Entry{Title: "Existing", Project: "demo", Body: "Existing body.", Category: "practice_standard", Description: "Existing"})
	h.app.LLM = fakeRecallLLM{payload: `{"operations":[
		{"action":"merge","target_title":"Missing target","source_titles":["Source"],"new_body":"A merged rule."},
		{"action":"merge","target_title":"Target","source_titles":["Missing source"],"new_body":"A merged rule."},
		{"action":"bogus","target_title":"Target","new_body":"A new rule."},
		{"action":"update","target_title":"Target","new_title":"Existing","new_body":"A new rule."},
		{"action":"update","target_title":"Target","new_body":"A new rule.","category":"not_a_category"}
	]}`}
	report, err := h.app.Dream(ctx, app.DreamOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if report.Updated != 0 || report.Merged != 0 || report.Deleted != 0 {
		t.Fatalf("an invalid operation was applied: %+v", report)
	}
	entries, err := h.app.Vault.Entries(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 3 {
		t.Fatalf("the corpus changed: %+v", entries)
	}
}

func TestDreamFailsWhenTheStateCannotBeSaved(t *testing.T) {
	h := newHarness(t, "obsidian")
	if err := os.MkdirAll(filepath.Join(h.root, ".smart-agents-memory-dream.json"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := h.app.Dream(context.Background(), app.DreamOptions{DryRun: true, DeterministicOnly: true}); err == nil {
		t.Fatal("the state save failure was swallowed")
	}
}

func TestDreamFailsWhenTheIndexFails(t *testing.T) {
	h := newHarness(t, "obsidian")
	blocked := filepath.Join(t.TempDir(), "blocked")
	if err := os.WriteFile(blocked, []byte("not a directory"), 0o644); err != nil {
		t.Fatal(err)
	}
	h.app.Projections = projection.New(blocked)
	if _, err := h.app.Dream(context.Background(), app.DreamOptions{DeterministicOnly: true}); err == nil {
		t.Fatal("the index failure was swallowed")
	}
}

func TestDreamFailsWhenTheLogCannotBeWritten(t *testing.T) {
	h := newHarness(t, "obsidian")
	h.app.Vault = &faultyVault{Vault: h.app.Vault, appendErr: errors.New("append failed")}
	if _, err := h.app.Dream(context.Background(), app.DreamOptions{DeterministicOnly: true}); err == nil {
		t.Fatal("the log failure was swallowed")
	}
}

func TestDreamNoopIsRecorded(t *testing.T) {
	h := newHarness(t, "obsidian")
	ctx := context.Background()
	report, err := h.app.Dream(ctx, app.DreamOptions{DeterministicOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	if report.Merged+report.Updated+report.Deleted+report.Backfilled != 0 {
		t.Fatalf("the report is %+v", report)
	}
	data, err := os.ReadFile(filepath.Join(h.root, ".smart-agents-memory-dream.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"last_status": "noop"`) {
		t.Fatalf("the noop status is missing: %s", data)
	}
}
