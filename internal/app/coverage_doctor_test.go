package app_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lindoelio/smart-agents-memory/internal/memory"
	"github.com/lindoelio/smart-agents-memory/internal/native"
	"github.com/lindoelio/smart-agents-memory/internal/projection"
	"github.com/lindoelio/smart-agents-memory/internal/sources"
)

func TestDoctorReportsVaultFailures(t *testing.T) {
	t.Run("store not ready", func(t *testing.T) {
		h := newHarness(t, "obsidian")
		h.app.Vault = &faultyVault{Vault: h.app.Vault, readyErr: errors.New("store is missing")}
		report, err := h.app.Doctor(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(strings.Join(report.Problems, "\n"), "store is missing") {
			t.Fatalf("the ready failure is %v", report.Problems)
		}
	})
	t.Run("corpus unreadable", func(t *testing.T) {
		h := newHarness(t, "obsidian")
		h.app.Vault = &faultyVault{Vault: h.app.Vault, entriesErr: errors.New("corpus unreadable")}
		report, err := h.app.Doctor(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(strings.Join(report.Problems, "\n"), "cannot read the corpus") {
			t.Fatalf("the corpus failure is %v", report.Problems)
		}
	})
	t.Run("diagnostics fail", func(t *testing.T) {
		h := newHarness(t, "obsidian")
		h.app.Vault = &diagnosticsErrorVault{Vault: h.app.Vault}
		report, err := h.app.Doctor(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(strings.Join(report.Problems, "\n"), "cannot read the store") {
			t.Fatalf("the diagnostics failure is %v", report.Problems)
		}
	})
	t.Run("worktree check fails", func(t *testing.T) {
		h := newHarness(t, "obsidian")
		repo := newRepo(t)
		t.Chdir(repo)
		h.app.Vault = &faultyVault{Vault: h.app.Vault, entriesErr: errors.New("corpus unreadable")}
		report, err := h.app.Doctor(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(strings.Join(report.Problems, "\n"), "cannot check worktrees") {
			t.Fatalf("the worktree failure is %v", report.Problems)
		}
	})
}

func TestDoctorReportsCorpusAndProjectionProblems(t *testing.T) {
	t.Run("duplicate title", func(t *testing.T) {
		h := newHarness(t, "obsidian")
		ctx := context.Background()
		for range 2 {
			if _, err := h.app.Vault.Create(ctx, memory.Entry{
				Title: "Twice", Type: memory.TypeLesson, Date: "2026-11-24", Body: "Body.",
			}); err != nil {
				t.Fatal(err)
			}
		}
		report, err := h.app.Doctor(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(strings.Join(report.Problems, "\n"), "duplicate title") {
			t.Fatalf("the duplicate is %v", report.Problems)
		}
	})
	t.Run("invalid type", func(t *testing.T) {
		h := newHarness(t, "obsidian")
		note := filepath.Join(h.root, "corpus", "bogus.md")
		if err := os.MkdirAll(filepath.Dir(note), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(note, []byte("---\ntitle: \"A bad type\"\ntype: bogus\n---\n\nBody.\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		report, err := h.app.Doctor(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(strings.Join(report.Problems, "\n"), "invalid type") {
			t.Fatalf("the invalid type is %v", report.Problems)
		}
	})
	t.Run("invalid topic", func(t *testing.T) {
		h := newHarness(t, "obsidian")
		if err := h.app.Projections.Write(native.TargetMiniMaxAgent, "topics/bad.md", "no frontmatter"); err != nil {
			t.Fatal(err)
		}
		report, err := h.app.Doctor(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(strings.Join(report.Problems, "\n"), "invalid topic file") {
			t.Fatalf("the invalid topic is %v", report.Problems)
		}
	})
	t.Run("topic without a corpus pair", func(t *testing.T) {
		h := newHarness(t, "obsidian")
		topic := native.RenderTopic(memory.Entry{
			Title: "An orphan topic", Type: memory.TypeLesson, Body: "Body.", Description: "Orphan",
		})
		if err := h.app.Projections.Write(native.TargetMiniMaxAgent, "topics/orphan.md", topic); err != nil {
			t.Fatal(err)
		}
		report, err := h.app.Doctor(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(strings.Join(report.Problems, "\n"), "topic with no corpus pair") {
			t.Fatalf("the orphan topic is %v", report.Problems)
		}
	})
	t.Run("projection listing fails", func(t *testing.T) {
		h := newHarness(t, "obsidian")
		if err := os.MkdirAll(filepath.Join(h.root, "native", "minimax-agent"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(h.root, "native", "minimax-agent", "topics"), []byte("not a directory"), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := h.app.Doctor(context.Background()); err != nil {
			t.Fatal(err)
		}
	})
}

func TestDoctorReportsMissingContentAndIndex(t *testing.T) {
	t.Run("missing projection content", func(t *testing.T) {
		h := newHarness(t, "obsidian")
		ctx := context.Background()
		if _, err := h.app.Install(ctx); err != nil {
			t.Fatal(err)
		}
		if err := os.Remove(filepath.Join(h.root, "native", "minimax-user", "user.md")); err != nil {
			t.Fatal(err)
		}
		if err := os.Remove(filepath.Join(h.root, "native", "minimax-agent", "MEMORY.md")); err != nil {
			t.Fatal(err)
		}
		report, err := h.app.Doctor(ctx)
		if err != nil {
			t.Fatal(err)
		}
		problems := strings.Join(report.Problems, "\n")
		if !strings.Contains(problems, "content missing (user profile)") ||
			!strings.Contains(problems, "content missing (agent memory)") {
			t.Fatalf("the missing content is %v", report.Problems)
		}
	})
	t.Run("missing hot tier", func(t *testing.T) {
		h := newHarness(t, "obsidian")
		ctx := context.Background()
		if _, err := h.app.Install(ctx); err != nil {
			t.Fatal(err)
		}
		if err := os.Remove(filepath.Join(h.root, "MEMORY.md")); err != nil {
			t.Fatal(err)
		}
		report, err := h.app.Doctor(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(strings.Join(report.Problems, "\n"), "content missing (hot tier)") {
			t.Fatalf("the missing hot tier is %v", report.Problems)
		}
	})
	t.Run("index writer without a locator", func(t *testing.T) {
		h := newHarness(t, "obsidian")
		h.app.Vault = &writerOnlyVault{Vault: h.app.Vault}
		report, err := h.app.Doctor(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(strings.Join(report.Problems, "\n"), "did not say where") {
			t.Fatalf("the missing locator is %v", report.Problems)
		}
	})
	t.Run("index missing", func(t *testing.T) {
		h := newHarness(t, "obsidian")
		ctx := context.Background()
		if _, err := h.app.Install(ctx); err != nil {
			t.Fatal(err)
		}
		if err := os.Remove(filepath.Join(h.root, "Smart Agents Memory.md")); err != nil {
			t.Fatal(err)
		}
		report, err := h.app.Doctor(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(strings.Join(report.Problems, "\n"), "no index") {
			t.Fatalf("the missing index is %v", report.Problems)
		}
	})
	t.Run("index stale", func(t *testing.T) {
		h := newHarness(t, "obsidian")
		ctx := context.Background()
		if _, err := h.app.Install(ctx); err != nil {
			t.Fatal(err)
		}
		index := filepath.Join(h.root, "Smart Agents Memory.md")
		old := time.Now().Add(-48 * time.Hour)
		if err := os.Chtimes(index, old, old); err != nil {
			t.Fatal(err)
		}
		report, err := h.app.Doctor(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(strings.Join(report.Notes, "\n"), "index is stale") {
			t.Fatalf("the stale index is %v", report.Notes)
		}
	})
	t.Run("skill missing", func(t *testing.T) {
		h := newHarness(t, "obsidian")
		ctx := context.Background()
		if _, err := h.app.Install(ctx); err != nil {
			t.Fatal(err)
		}
		if err := os.RemoveAll(filepath.Join(h.home, ".agents", "skills", "smart-agents-memory")); err != nil {
			t.Fatal(err)
		}
		report, err := h.app.Doctor(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(strings.Join(report.Problems, "\n"), "skill not installed") {
			t.Fatalf("the missing skill is %v", report.Problems)
		}
	})
}

func TestDoctorReportsTheSourceMatrix(t *testing.T) {
	h := newHarness(t, "obsidian")
	ctx := context.Background()
	h.app.Cfg.SyncDisabled = []string{"cursor"}
	missing := filepath.Join(t.TempDir(), "missing")
	h.app.Cfg.SyncRoots = map[string][]string{"claude": {missing}}
	state := sources.NewState()
	state.Sources["cursor"] = sources.SourceState{
		LastSync: time.Now().Add(-time.Hour),
		Errors:   []string{"boom"},
	}
	state.Sources["claude"] = sources.SourceState{LastSync: time.Now().Add(-2 * time.Hour)}
	data, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(h.root, ".sync-state.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	report, err := h.app.Doctor(ctx)
	if err != nil {
		t.Fatal(err)
	}
	notes := strings.Join(report.Notes, "\n")
	if !strings.Contains(notes, "source cursor: disabled") {
		t.Fatalf("the disabled source is %v", report.Notes)
	}
	if !strings.Contains(notes, "source claude: missing") {
		t.Fatalf("the missing source is %v", report.Notes)
	}
	if !strings.Contains(notes, "errors: boom") {
		t.Fatalf("the source errors are %v", report.Notes)
	}
}

func TestInstallEdgeCases(t *testing.T) {
	t.Run("default local root", func(t *testing.T) {
		h := newHarness(t, "obsidian")
		copy := *h.app
		copy.LocalRoot = ""
		copy.Projections = nil
		copy.ConfigPath = ""
		if _, err := copy.Install(context.Background()); err != nil {
			t.Fatal(err)
		}
		if copy.LocalRoot != filepath.Join(h.home, "Smart Agents Memory") {
			t.Fatalf("the default root is %q", copy.LocalRoot)
		}
	})
	t.Run("layout fails", func(t *testing.T) {
		h := newHarness(t, "obsidian")
		h.app.Vault = &layoutErrorVault{Vault: h.app.Vault}
		if _, err := h.app.Install(context.Background()); err == nil {
			t.Fatal("the layout failure was swallowed")
		}
	})
	t.Run("local root cannot be created", func(t *testing.T) {
		h := newHarness(t, "obsidian")
		file := filepath.Join(t.TempDir(), "file")
		if err := os.WriteFile(file, []byte("not a directory"), 0o644); err != nil {
			t.Fatal(err)
		}
		h.app.Vault = &faultyVault{Vault: h.app.Vault}
		h.app.LocalRoot = filepath.Join(file, "nested")
		if _, err := h.app.Install(context.Background()); err == nil {
			t.Fatal("the local root failure was swallowed")
		}
	})
	t.Run("hot tier cannot be written", func(t *testing.T) {
		h := newHarness(t, "obsidian")
		local := t.TempDir()
		if err := os.Chmod(local, 0o555); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(local, 0o755) })
		h.app.Vault = &faultyVault{Vault: h.app.Vault}
		h.app.LocalRoot = local
		if _, err := h.app.Install(context.Background()); err == nil {
			t.Fatal("the hot tier failure was swallowed")
		}
	})
	t.Run("projection directories fail", func(t *testing.T) {
		h := newHarness(t, "obsidian")
		blocked := filepath.Join(t.TempDir(), "blocked")
		if err := os.WriteFile(blocked, []byte("not a directory"), 0o644); err != nil {
			t.Fatal(err)
		}
		h.app.Projections = projection.New(blocked)
		if _, err := h.app.Install(context.Background()); err == nil {
			t.Fatal("the projection failure was swallowed")
		}
	})
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
		if _, err := h.app.Install(context.Background()); err == nil {
			t.Fatal("the skeleton failure was swallowed")
		}
	})
	t.Run("index fails", func(t *testing.T) {
		h := newHarness(t, "obsidian")
		h.app.Vault = &faultyVault{Vault: h.app.Vault, entriesErr: errors.New("entries failed")}
		if _, err := h.app.Install(context.Background()); err == nil {
			t.Fatal("the index failure was swallowed")
		}
	})
	t.Run("skill fails", func(t *testing.T) {
		h := newHarness(t, "obsidian")
		share := filepath.Join(h.home, ".local", "share", "smart-agents-memory")
		if err := os.MkdirAll(filepath.Dir(share), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(share, []byte("not a directory"), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := h.app.Install(context.Background()); err == nil {
			t.Fatal("the skill failure was swallowed")
		}
	})
	t.Run("link fails", func(t *testing.T) {
		h := newHarness(t, "obsidian")
		if err := os.WriteFile(filepath.Join(h.home, ".minimax"), []byte("not a directory"), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := h.app.Install(context.Background()); err == nil {
			t.Fatal("the link failure was swallowed")
		}
	})
}
