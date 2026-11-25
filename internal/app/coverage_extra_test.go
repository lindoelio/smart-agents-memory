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
	"github.com/lindoelio/smart-agents-memory/internal/vault"
)

// noteListerOnlyVault can list project notes but cannot remove them.
type noteListerOnlyVault struct {
	vault.Vault
	names []string
}

func (v *noteListerOnlyVault) ProjectNoteNames(context.Context) ([]string, error) {
	return v.names, nil
}

// noteRemoveErrorVault can list and remove notes but fails the removal.
type noteRemoveErrorVault struct{ noteListerOnlyVault }

func (v *noteRemoveErrorVault) RemoveProjectNote(context.Context, string) error {
	return errors.New("note removal failed")
}

// pruneAppendErrorVault forwards the removal capability and fails the log.
type pruneAppendErrorVault struct {
	vault.Vault
	appendErr error
}

func (v *pruneAppendErrorVault) Remove(ctx context.Context, e memory.Entry) error {
	return v.Vault.(vault.Remover).Remove(ctx, e)
}

func (v *pruneAppendErrorVault) AppendLog(ctx context.Context, line string) error {
	if v.appendErr != nil {
		return v.appendErr
	}
	return v.Vault.AppendLog(ctx, line)
}

func TestAddPinnedUpgradeReportsItsWriteFailures(t *testing.T) {
	cases := []struct {
		name string
		wrap func(real vault.Vault) vault.Vault
	}{
		{"update", func(real vault.Vault) vault.Vault {
			return &faultyVault{Vault: real, updateErr: errors.New("update failed")}
		}},
		{"append log", func(real vault.Vault) vault.Vault {
			return &faultyVault{Vault: real, appendErr: errors.New("append failed")}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t, "obsidian")
			ctx := context.Background()
			if _, err := h.app.Add(ctx, app.AddOptions{
				Scope: "lesson", Title: "A rule", Body: "The same body.",
			}); err != nil {
				t.Fatal(err)
			}
			h.app.Vault = tc.wrap(h.app.Vault)
			if _, err := h.app.Add(ctx, app.AddOptions{
				Scope: "lesson", Title: "A rule", Body: "The same body.", Pinned: true,
			}); err == nil {
				t.Fatal("the write failure was swallowed")
			}
		})
	}
	t.Run("index", func(t *testing.T) {
		h := newHarness(t, "obsidian")
		ctx := context.Background()
		if _, err := h.app.Add(ctx, app.AddOptions{
			Scope: "lesson", Title: "A rule", Body: "The same body.",
		}); err != nil {
			t.Fatal(err)
		}
		blocked := filepath.Join(t.TempDir(), "blocked")
		if err := os.WriteFile(blocked, []byte("not a directory"), 0o644); err != nil {
			t.Fatal(err)
		}
		h.app.Projections = projection.New(blocked)
		if _, err := h.app.Add(ctx, app.AddOptions{
			Scope: "lesson", Title: "A rule", Body: "The same body.", Pinned: true,
		}); err == nil {
			t.Fatal("the index failure was swallowed")
		}
	})
}

func TestRecallEdgeCases(t *testing.T) {
	t.Run("unknown ref", func(t *testing.T) {
		h := newHarness(t, "obsidian")
		if _, err := h.app.Recall(context.Background(), app.RecallOptions{
			Refs: []string{"lesson:missing"},
		}); err == nil {
			t.Fatal("an unknown ref was accepted")
		}
	})
	t.Run("llm not configured", func(t *testing.T) {
		h := newHarness(t, "obsidian")
		ctx := context.Background()
		if _, err := h.app.Add(ctx, app.AddOptions{
			Scope: "lesson", Title: "A rule", Body: "The rule.", Category: "practice_standard",
		}); err != nil {
			t.Fatal(err)
		}
		if _, err := h.app.Recall(ctx, app.RecallOptions{Query: "rule", LLM: true}); err == nil {
			t.Fatal("the missing LLM was ignored")
		}
	})
	t.Run("llm with no items", func(t *testing.T) {
		h := newHarness(t, "obsidian")
		h.app.LLM = fakeRecallLLM{payload: `{"refs":[]}`}
		result, err := h.app.Recall(context.Background(), app.RecallOptions{Query: "rule", LLM: true})
		if err != nil {
			t.Fatal(err)
		}
		if result.Text != "" {
			t.Fatalf("the result is %+v", result)
		}
	})
	t.Run("llm error", func(t *testing.T) {
		h := newHarness(t, "obsidian")
		ctx := context.Background()
		if _, err := h.app.Add(ctx, app.AddOptions{
			Scope: "lesson", Title: "A rule", Body: "The rule.", Category: "practice_standard",
		}); err != nil {
			t.Fatal(err)
		}
		h.app.LLM = fakeRecallLLM{err: errors.New("model failed")}
		if _, err := h.app.Recall(ctx, app.RecallOptions{Query: "rule", LLM: true}); err == nil {
			t.Fatal("the model failure was swallowed")
		}
	})
	t.Run("duplicate refs and limit", func(t *testing.T) {
		h := newHarness(t, "obsidian")
		ctx := context.Background()
		for _, title := range []string{"First rule", "Second rule", "Third rule"} {
			if _, err := h.app.Add(ctx, app.AddOptions{
				Scope: "lesson", Title: title, Body: "The rule.", Category: "practice_standard",
			}); err != nil {
				t.Fatal(err)
			}
		}
		h.app.LLM = fakeRecallLLM{payload: `{"refs":["lesson:first-rule","lesson:first-rule","lesson:second-rule"]}`}
		result, err := h.app.Recall(ctx, app.RecallOptions{Query: "rule", LLM: true, Limit: 2})
		if err != nil {
			t.Fatal(err)
		}
		if len(result.Refs) != 2 {
			t.Fatalf("the refs are %v", result.Refs)
		}
	})
	t.Run("manifest does not fit", func(t *testing.T) {
		h := newHarness(t, "obsidian")
		ctx := context.Background()
		if _, err := h.app.Add(ctx, app.AddOptions{
			Scope: "lesson", Title: "A huge rule", Body: "The rule.",
			Description: strings.Repeat("x", 30000), Category: "practice_standard",
		}); err != nil {
			t.Fatal(err)
		}
		h.app.LLM = fakeRecallLLM{payload: `{"refs":[]}`}
		_, err := h.app.Recall(ctx, app.RecallOptions{Query: "rule", LLM: true})
		if err == nil || !strings.Contains(err.Error(), "does not fit") {
			t.Fatalf("the manifest error is %v", err)
		}
	})
	t.Run("entries fail", func(t *testing.T) {
		h := newHarness(t, "obsidian")
		h.app.Vault = &faultyVault{Vault: h.app.Vault, entriesErr: errors.New("entries failed")}
		if _, err := h.app.Recall(context.Background(), app.RecallOptions{}); err == nil {
			t.Fatal("the entries failure was swallowed")
		}
	})
	t.Run("default router", func(t *testing.T) {
		h := newHarness(t, "obsidian")
		result, err := h.app.Recall(context.Background(), app.RecallOptions{})
		if err != nil {
			t.Fatal(err)
		}
		if result.Text == "" {
			t.Fatal("the router is empty")
		}
	})
	t.Run("rank truncates and filters categories", func(t *testing.T) {
		h := newHarness(t, "obsidian")
		ctx := context.Background()
		if _, err := h.app.Add(ctx, app.AddOptions{
			Scope: "lesson", Title: "A test rule", Body: "The test rule.",
			Category: "testing_standard",
		}); err != nil {
			t.Fatal(err)
		}
		if _, err := h.app.Add(ctx, app.AddOptions{
			Scope: "lesson", Title: "A build rule", Body: "The build rule.",
			Category: "build_configuration",
		}); err != nil {
			t.Fatal(err)
		}
		result, err := h.app.Recall(ctx, app.RecallOptions{
			Query: "rule", Limit: 1, Categories: []string{"testing_standard"},
		})
		if err != nil {
			t.Fatal(err)
		}
		if len(result.Refs) != 1 || result.Refs[0] != "lesson:a-test-rule" {
			t.Fatalf("the ranked refs are %v", result.Refs)
		}
	})
	t.Run("rank uses the scenario and the scope", func(t *testing.T) {
		h := newHarness(t, "obsidian")
		ctx := context.Background()
		repo := newRepo(t)
		worktree := newLinkedWorktree(t, repo, "feature")
		if _, err := h.app.Add(ctx, app.AddOptions{
			Scope: "project", Title: "A branch rule", Body: "The rule.",
			Category: "practice_standard", Project: memory.Slugify("repo"), Worktree: "feature",
			UsageScenarios: []string{"flaky release"},
		}); err != nil {
			t.Fatal(err)
		}
		result, err := h.app.Recall(ctx, app.RecallOptions{Query: "flaky", CWD: worktree})
		if err != nil {
			t.Fatal(err)
		}
		if len(result.Refs) != 1 {
			t.Fatalf("the ranked refs are %v", result.Refs)
		}
	})
	t.Run("rank breaks score ties by title", func(t *testing.T) {
		h := newHarness(t, "obsidian")
		ctx := context.Background()
		for _, title := range []string{"Bravo rule", "Alpha rule"} {
			if _, err := h.app.Add(ctx, app.AddOptions{
				Scope: "lesson", Title: title, Body: "The rule.", Category: "practice_standard",
			}); err != nil {
				t.Fatal(err)
			}
		}
		result, err := h.app.Recall(ctx, app.RecallOptions{Query: "rule"})
		if err != nil {
			t.Fatal(err)
		}
		if len(result.Refs) != 2 || result.Refs[0] != "lesson:alpha-rule" {
			t.Fatalf("the tie order is %v", result.Refs)
		}
	})
}

func TestIndexSkeletonAndMigrateCapabilitySkips(t *testing.T) {
	t.Run("skeleton fails", func(t *testing.T) {
		h := newHarness(t, "obsidian")
		dir := filepath.Join(h.root, "native", "minimax-user")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(dir, 0o555); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
		if _, err := h.app.Index(context.Background()); err == nil {
			t.Fatal("the skeleton failure was swallowed")
		}
	})
	t.Run("migration without a renamer", func(t *testing.T) {
		h := newHarness(t, "obsidian")
		note := "---\ntitle: \"Reconciliação do corpus: mtime decide quem manda\"\ntype: lesson\ndate: 2026-11-07\n---\n\nBody.\n"
		path := filepath.Join(h.root, "corpus", "lesson", "legacy.md")
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(note), 0o644); err != nil {
			t.Fatal(err)
		}
		h.app.Vault = &faultyVault{Vault: h.app.Vault}
		if _, err := h.app.Migrate(context.Background(), true); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("migration skips a duplicate without a remover", func(t *testing.T) {
		h := newHarness(t, "obsidian")
		h.app.Vault = noRemovalVault{entries: []memory.Entry{{
			Title: "Nome", Type: memory.TypeUser, Body: "Body.", Path: "corpus/user/nome.md",
		}}}
		if _, err := h.app.Migrate(context.Background(), true); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("orphan removal fails", func(t *testing.T) {
		h := newHarness(t, "obsidian")
		topics := filepath.Join(h.root, "native", "minimax-agent", "topics")
		if err := os.MkdirAll(topics, 0o755); err != nil {
			t.Fatal(err)
		}
		orphan := "---\ndescription: An orphan\n---\n\n<!-- smart-agents-memory:title:An orphan -->\n\nBody.\n"
		if err := os.WriteFile(filepath.Join(topics, "orphan.md"), []byte(orphan), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(topics, 0o555); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(topics, 0o755) })
		if _, err := h.app.Migrate(context.Background(), true); err == nil {
			t.Fatal("the orphan removal failure was swallowed")
		}
	})
}

func TestExtractManifestAndCursorEdges(t *testing.T) {
	t.Run("assistant only save fails", func(t *testing.T) {
		h := newHarness(t, "obsidian")
		ctx := context.Background()
		h.app.LLM = &extractLLM{payload: `{"candidates":[]}`}
		if err := os.Chmod(h.root, 0o555); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(h.root, 0o755) })
		_, err := h.app.Extract(ctx, app.ExtractOptions{
			Transcript: `{"role":"assistant","content":"noted"}`,
			SessionID:  "assistant",
		})
		if err == nil {
			t.Fatal("the cursor save failure was swallowed")
		}
	})
	t.Run("auto dream default threshold", func(t *testing.T) {
		h := newHarness(t, "obsidian")
		h.app.LLM = &extractLLM{payload: `{"candidates":[]}`}
		report, err := h.app.Extract(context.Background(), app.ExtractOptions{
			Transcript: `{"role":"user","content":"extract"}`,
			SessionID:  "auto", AutoDream: true,
		})
		if err != nil {
			t.Fatal(err)
		}
		if report.Dream != nil {
			t.Fatalf("the dream ran below the threshold: %+v", report)
		}
	})
	t.Run("manifest carries the description and is bounded", func(t *testing.T) {
		h := newHarness(t, "obsidian")
		ctx := context.Background()
		if _, err := h.app.Add(ctx, app.AddOptions{
			Scope: "lesson", Title: "A described rule", Body: "The rule.",
			Description: "A description for the manifest.",
		}); err != nil {
			t.Fatal(err)
		}
		if _, err := h.app.Add(ctx, app.AddOptions{
			Scope: "lesson", Title: "A huge rule", Body: "The rule.",
			Description: strings.Repeat("y", 30000),
		}); err != nil {
			t.Fatal(err)
		}
		h.app.LLM = &extractLLM{payload: `{"candidates":[]}`}
		if _, err := h.app.Extract(ctx, app.ExtractOptions{
			Transcript: `{"role":"user","content":"extract"}`,
			SessionID:  "manifest",
		}); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("project resolves from the process directory", func(t *testing.T) {
		h := newHarness(t, "obsidian")
		repo := newRepo(t)
		t.Chdir(repo)
		h.app.LLM = &extractLLM{payload: `{"candidates":[{"scope":"project","title":"A process rule","body":"The rule. Why it matters."}]}`}
		report, err := h.app.Extract(context.Background(), app.ExtractOptions{
			Transcript: `{"role":"user","content":"extract"}`,
			SessionID:  "process",
		})
		if err != nil {
			t.Fatal(err)
		}
		if report.Added != 1 {
			t.Fatalf("the report is %+v", report)
		}
		entries, err := h.app.Vault.Entries(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if len(entries) != 1 || entries[0].Project != memory.Slugify("repo") {
			t.Fatalf("the process scope is %+v", entries)
		}
	})
}

func TestDreamAppliesAValidUpdate(t *testing.T) {
	h := newHarness(t, "obsidian")
	ctx := context.Background()
	createDreamEntry(t, h, memory.Entry{
		Title: "Target", Project: "demo", Body: "Target body.",
		Category: "practice_standard", Description: "Target",
	})
	createDreamEntry(t, h, memory.Entry{
		Title: "Source", Project: "demo", Body: "Source body.",
		Category: "practice_standard", Description: "Source",
	})
	h.app.LLM = fakeRecallLLM{payload: `{"operations":[{"action":"update","target_title":"Target","new_body":"An updated rule. Why it matters.","description":"Updated","category":"practice_standard","keywords":["updated"],"usage_scenarios":["when updating"]}]}`}
	report, err := h.app.Dream(ctx, app.DreamOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if report.Updated != 1 {
		t.Fatalf("the report is %+v", report)
	}
	entries, err := h.app.Vault.Entries(ctx)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, entry := range entries {
		if entry.Title == "Target" && strings.Contains(entry.Body, "An updated rule.") {
			found = true
			if len(entry.Keywords) != 1 || len(entry.UsageScenarios) != 1 {
				t.Fatalf("the taxonomy was not merged: %+v", entry)
			}
		}
	}
	if !found {
		t.Fatalf("the update was not stored: %+v", entries)
	}
}

func TestDreamStateAndLockEdges(t *testing.T) {
	t.Run("state save fails after the run", func(t *testing.T) {
		h := newHarness(t, "obsidian")
		if err := os.MkdirAll(filepath.Join(h.root, ".smart-agents-memory-dream.json"), 0o755); err != nil {
			t.Fatal(err)
		}
		if _, err := h.app.Dream(context.Background(), app.DreamOptions{DeterministicOnly: true}); err == nil {
			t.Fatal("the state save failure was swallowed")
		}
	})
	t.Run("lock cannot be created", func(t *testing.T) {
		h := newHarness(t, "obsidian")
		if err := os.Chmod(h.root, 0o555); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(h.root, 0o755) })
		if _, err := h.app.Dream(context.Background(), app.DreamOptions{DeterministicOnly: true}); err == nil {
			t.Fatal("the lock failure was swallowed")
		}
	})
	t.Run("lock cannot be cleared", func(t *testing.T) {
		h := newHarness(t, "obsidian")
		lock := filepath.Join(h.root, ".consolidation.lock")
		if err := os.MkdirAll(lock, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(lock, "keep"), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		_, err := h.app.Dream(context.Background(), app.DreamOptions{DeterministicOnly: true})
		if err == nil || !strings.Contains(err.Error(), "already running") {
			t.Fatalf("the lock error is %v", err)
		}
	})
	t.Run("version zero is normalized", func(t *testing.T) {
		h := newHarness(t, "obsidian")
		path := filepath.Join(h.root, ".smart-agents-memory-dream.json")
		if err := os.WriteFile(path, []byte(`{"version":0,"dirty_mutations":2}`), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := h.app.Add(context.Background(), app.AddOptions{
			Scope: "lesson", Title: "A rule", Body: "The rule.",
		}); err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(data), `"version": 1`) {
			t.Fatalf("the version was not normalized: %s", data)
		}
	})
	t.Run("body dedupe needs the remover", func(t *testing.T) {
		h := newHarness(t, "obsidian")
		createDreamEntry(t, h, memory.Entry{Title: "First title", Body: "Same body."})
		createDreamEntry(t, h, memory.Entry{Title: "Second title", Body: "Same body."})
		h.app.Vault = &faultyVault{Vault: h.app.Vault}
		if _, err := h.app.Dream(context.Background(), app.DreamOptions{DeterministicOnly: true}); err == nil {
			t.Fatal("the body dedupe failure was swallowed")
		}
	})
}

func TestPruneWorktreesCapabilityErrors(t *testing.T) {
	slug := memory.Slugify("repo")
	setup := func(t *testing.T) (*harness, string) {
		t.Helper()
		h := newHarness(t, "obsidian")
		ctx := context.Background()
		repo := newRepo(t)
		newLinkedWorktree(t, repo, "feature")
		if _, err := h.app.Add(ctx, app.AddOptions{
			Scope: "project", Title: "A branch decision", Body: "The decision.",
			Project: slug, Worktree: "feature",
		}); err != nil {
			t.Fatal(err)
		}
		if err := os.RemoveAll(filepath.Join(repo, ".git", "worktrees", "feature")); err != nil {
			t.Fatal(err)
		}
		return h, repo
	}

	t.Run("entry removal fails", func(t *testing.T) {
		h, repo := setup(t)
		h.app.Vault = &removerErrorVault{Vault: h.app.Vault}
		if _, err := h.app.PruneWorktrees(context.Background(), app.PruneWorktreesOptions{CWD: repo, Apply: true}); err == nil {
			t.Fatal("the removal failure was swallowed")
		}
	})
	t.Run("note remover missing", func(t *testing.T) {
		h := newHarness(t, "obsidian")
		repo := newRepo(t)
		h.app.Vault = &noteListerOnlyVault{Vault: h.app.Vault, names: []string{slug + "@feature"}}
		_, err := h.app.PruneWorktrees(context.Background(), app.PruneWorktreesOptions{CWD: repo, Apply: true})
		if err == nil || !strings.Contains(err.Error(), "cannot remove project notes") {
			t.Fatalf("the note capability is %v", err)
		}
	})
	t.Run("note removal fails", func(t *testing.T) {
		h := newHarness(t, "obsidian")
		repo := newRepo(t)
		h.app.Vault = &noteRemoveErrorVault{noteListerOnlyVault{Vault: h.app.Vault, names: []string{slug + "@feature"}}}
		if _, err := h.app.PruneWorktrees(context.Background(), app.PruneWorktreesOptions{CWD: repo, Apply: true}); err == nil {
			t.Fatal("the note removal failure was swallowed")
		}
	})
	t.Run("log fails", func(t *testing.T) {
		h, repo := setup(t)
		h.app.Vault = &pruneAppendErrorVault{Vault: h.app.Vault, appendErr: errors.New("append failed")}
		if _, err := h.app.PruneWorktrees(context.Background(), app.PruneWorktreesOptions{CWD: repo, Apply: true}); err == nil {
			t.Fatal("the log failure was swallowed")
		}
	})
	t.Run("index fails", func(t *testing.T) {
		h, repo := setup(t)
		blocked := filepath.Join(t.TempDir(), "blocked")
		if err := os.WriteFile(blocked, []byte("not a directory"), 0o644); err != nil {
			t.Fatal(err)
		}
		h.app.Projections = projection.New(blocked)
		if _, err := h.app.PruneWorktrees(context.Background(), app.PruneWorktreesOptions{CWD: repo, Apply: true}); err == nil {
			t.Fatal("the index failure was swallowed")
		}
	})
}

func TestTeamWorktreeAutoAndSearchLimit(t *testing.T) {
	t.Run("worktree auto", func(t *testing.T) {
		h := newHarness(t, "obsidian")
		ctx := context.Background()
		repo := newRepo(t)
		worktree := newLinkedWorktree(t, repo, "feature")
		if _, err := h.app.Add(ctx, app.AddOptions{
			Scope: "lesson", Title: "A branch rule", Body: "The branch rule.",
			Team: true, WorktreeAuto: true, CWD: worktree,
		}); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("search limit", func(t *testing.T) {
		h := newHarness(t, "obsidian")
		ctx := context.Background()
		repo := newRepo(t)
		for _, title := range []string{"A shared first", "A shared second"} {
			if _, err := h.app.Add(ctx, app.AddOptions{
				Scope: "lesson", Title: title, Body: "The shared body.",
				Team: true, CWD: repo,
			}); err != nil {
				t.Fatal(err)
			}
		}
		t.Chdir(repo)
		hits, err := h.app.Search(ctx, "shared", 1)
		if err != nil {
			t.Fatal(err)
		}
		if len(hits) != 1 {
			t.Fatalf("the team limit is %+v", hits)
		}
	})
	t.Run("outside a repository", func(t *testing.T) {
		h := newHarness(t, "obsidian")
		h.app.Vault = nil
		t.Chdir(t.TempDir())
		if _, err := h.app.Search(context.Background(), "anything", 10); err != nil {
			t.Fatal(err)
		}
	})
}

func TestWatchOnceSkipsAnUnreadableSubdirectory(t *testing.T) {
	h := newHarness(t, "obsidian")
	dir := t.TempDir()
	sub := filepath.Join(dir, "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, "session.jsonl"), []byte(`{"role":"user","content":"x"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(sub, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(sub, 0o755) })
	if _, err := h.app.WatchOnce(context.Background(), app.WatchOptions{Dirs: []string{dir}}); err != nil {
		t.Fatal(err)
	}
}

func TestDreamUpdatesKeepTheDryRunState(t *testing.T) {
	h := newHarness(t, "obsidian")
	ctx := context.Background()
	createDreamEntry(t, h, memory.Entry{
		Title: "Target", Project: "demo", Body: "Target body.",
		Category: "practice_standard", Description: "Target",
	})
	createDreamEntry(t, h, memory.Entry{
		Title: "Source", Project: "demo", Body: "Source body.",
		Category: "practice_standard", Description: "Source",
	})
	h.app.LLM = fakeRecallLLM{payload: `{"operations":[]}`}
	if _, err := h.app.Dream(ctx, app.DreamOptions{DryRun: true}); err != nil {
		t.Fatal(err)
	}
	statePath := filepath.Join(h.root, ".smart-agents-memory-dream.json")
	data, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"last_status": "noop"`) {
		t.Fatalf("the dry-run state is %s", data)
	}
	if info, err := os.Stat(statePath); err != nil || time.Since(info.ModTime()) > time.Minute {
		t.Fatalf("the state file is %v %v", info, err)
	}
}
