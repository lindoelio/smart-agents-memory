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
	"github.com/lindoelio/smart-agents-memory/internal/native"
	"github.com/lindoelio/smart-agents-memory/internal/projection"
	"github.com/lindoelio/smart-agents-memory/internal/vault"
)

// faultyVault delegates to a real gateway but fails the operation under
// test. Embedding the interface keeps the optional capabilities away, which
// is also useful for the tests of a gateway that cannot remove or rename.
type faultyVault struct {
	vault.Vault
	readyErr   error
	entriesErr error
	createErr  error
	updateErr  error
	appendErr  error
	searchErr  error
}

func (v *faultyVault) Ready(ctx context.Context) error {
	if v.readyErr != nil {
		return v.readyErr
	}
	return v.Vault.Ready(ctx)
}

func (v *faultyVault) Entries(ctx context.Context) ([]memory.Entry, error) {
	if v.entriesErr != nil {
		return nil, v.entriesErr
	}
	return v.Vault.Entries(ctx)
}

func (v *faultyVault) Create(ctx context.Context, e memory.Entry) (memory.Entry, error) {
	if v.createErr != nil {
		return memory.Entry{}, v.createErr
	}
	return v.Vault.Create(ctx, e)
}

func (v *faultyVault) Update(ctx context.Context, e memory.Entry) error {
	if v.updateErr != nil {
		return v.updateErr
	}
	return v.Vault.Update(ctx, e)
}

func (v *faultyVault) AppendLog(ctx context.Context, line string) error {
	if v.appendErr != nil {
		return v.appendErr
	}
	return v.Vault.AppendLog(ctx, line)
}

func (v *faultyVault) Search(ctx context.Context, query string, limit int) ([]vault.Hit, error) {
	if v.searchErr != nil {
		return nil, v.searchErr
	}
	return v.Vault.Search(ctx, query, limit)
}

// notesErrorVault keeps project and worktree notes but can fail either one.
type notesErrorVault struct {
	vault.Vault
	projectErr  error
	worktreeErr error
}

func (v *notesErrorVault) ProjectNote(ctx context.Context, name string) (string, bool, error) {
	if v.projectErr != nil {
		return "", false, v.projectErr
	}
	return "", false, nil
}

func (v *notesErrorVault) WorktreeNote(ctx context.Context, project, worktree string) (string, bool, error) {
	if v.worktreeErr != nil {
		return "", false, v.worktreeErr
	}
	return "", false, nil
}

// projectOnlyVault has project notes and no worktree notes.
type projectOnlyVault struct{ vault.Vault }

func (v *projectOnlyVault) ProjectNote(context.Context, string) (string, bool, error) {
	return "", false, nil
}

// diagnosticsErrorVault fails the store diagnostics.
type diagnosticsErrorVault struct{ vault.Vault }

func (v *diagnosticsErrorVault) Problems(context.Context) ([]string, error) {
	return nil, errors.New("diagnostics failed")
}

// writerOnlyVault publishes an index without saying where.
type writerOnlyVault struct{ vault.Vault }

func (v *writerOnlyVault) WriteIndex(context.Context, []memory.Entry) (string, error) {
	return "", nil
}

// layoutErrorVault cannot create its layout.
type layoutErrorVault struct{ vault.Vault }

func (v *layoutErrorVault) Ensure(context.Context) error {
	return errors.New("layout failed")
}

// removerErrorVault implements the removal capability and fails it.
type removerErrorVault struct{ vault.Vault }

func (v *removerErrorVault) Remove(context.Context, memory.Entry) error {
	return errors.New("remove failed")
}

// renameErrorVault implements the rename capability and fails it.
type renameErrorVault struct{ vault.Vault }

func (v *renameErrorVault) Rename(_ context.Context, e memory.Entry, _ string) (memory.Entry, error) {
	return e, errors.New("rename failed")
}

func TestAddRejectsInvalidOptions(t *testing.T) {
	cases := []struct {
		name string
		opts app.AddOptions
		want string
	}{
		{"invalid scope", app.AddOptions{Scope: "bogus", Title: "A", Body: "A body."}, "invalid scope"},
		{"invalid category", app.AddOptions{Scope: "lesson", Title: "A", Body: "A body.", Category: "not_a_category"}, "invalid category"},
		{"empty body", app.AddOptions{Scope: "lesson", Title: "A", Body: "   "}, "empty body"},
		{"secret", app.AddOptions{Scope: "lesson", Title: "A", Body: "ghp_" + strings.Repeat("a", 36)}, "GitHub PAT"},
		{"invalid native", app.AddOptions{Scope: "lesson", Title: "A", Body: "A body.", Native: "bogus"}, "invalid native projection"},
		{"invalid tier", app.AddOptions{Scope: "lesson", Title: "A", Body: "A body.", Tier: "warm"}, "invalid tier"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t, "obsidian")
			_, err := h.app.Add(context.Background(), tc.opts)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("the error is %v, want %q", err, tc.want)
			}
		})
	}
}

func TestAddDefaultsTheScopeAndTier(t *testing.T) {
	h := newHarness(t, "obsidian")
	ctx := context.Background()
	if _, err := h.app.Add(ctx, app.AddOptions{Title: "A defaulted rule", Body: "The rule. Why it matters."}); err != nil {
		t.Fatal(err)
	}
	entries, err := h.app.Vault.Entries(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("the corpus is %+v", entries)
	}
	if entries[0].Type != memory.TypeLesson || entries[0].EffectiveTier() != memory.TierCold {
		t.Fatalf("the defaults are %+v", entries[0])
	}
	if entries[0].Native != "minimax-agent" || entries[0].NativeType != "reference" {
		t.Fatalf("the native defaults are %+v", entries[0])
	}
}

func TestAddAppendCarriesEveryMetadataField(t *testing.T) {
	h := newHarness(t, "obsidian")
	ctx := context.Background()
	if _, err := h.app.Add(ctx, app.AddOptions{
		Scope: "lesson", Title: "A project rule", Body: "The first part.",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := h.app.Add(ctx, app.AddOptions{
		Scope: "lesson", Title: "A project rule", Body: "The second part.",
		Description: "A described rule", Reason: "Recorded for a reason.", Tier: "hot",
		Category: "practice_standard", UsageScenarios: []string{"before a release", ""},
		Pinned: true, Project: "demo", Worktree: "feature",
	}); err != nil {
		t.Fatal(err)
	}
	entries, err := h.app.Vault.Entries(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("the corpus is %+v", entries)
	}
	e := entries[0]
	if e.Description != "A described rule" || e.Reason != "Recorded for a reason." {
		t.Fatalf("the description and reason are %+v", e)
	}
	if e.Tier != memory.TierHot || !e.Pinned {
		t.Fatalf("the tier and pin are %+v", e)
	}
	if e.Category != "practice_standard" || len(e.UsageScenarios) != 1 {
		t.Fatalf("the taxonomy is %+v", e)
	}
	if e.Project != "demo" || e.Worktree != "feature" {
		t.Fatalf("the scope is %q@%q", e.Project, e.Worktree)
	}
}

func TestAddFailsOnVaultErrors(t *testing.T) {
	errBoom := errors.New("boom")
	cases := []struct {
		name string
		wrap func(real vault.Vault) vault.Vault
		opts app.AddOptions
	}{
		{"entries", func(real vault.Vault) vault.Vault {
			return &faultyVault{Vault: real, entriesErr: errBoom}
		}, app.AddOptions{Scope: "lesson", Title: "A rule", Body: "The rule."}},
		{"create", func(real vault.Vault) vault.Vault {
			return &faultyVault{Vault: real, createErr: errBoom}
		}, app.AddOptions{Scope: "lesson", Title: "A rule", Body: "The rule."}},
		{"append on create", func(real vault.Vault) vault.Vault {
			return &faultyVault{Vault: real, appendErr: errBoom}
		}, app.AddOptions{Scope: "lesson", Title: "A rule", Body: "The rule."}},
		{"update on append", func(real vault.Vault) vault.Vault {
			return &faultyVault{Vault: real, updateErr: errBoom}
		}, app.AddOptions{Scope: "lesson", Title: "A rule", Body: "The second part."}},
		{"append log on append", func(real vault.Vault) vault.Vault {
			return &faultyVault{Vault: real, appendErr: errBoom}
		}, app.AddOptions{Scope: "lesson", Title: "A rule", Body: "The second part."}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t, "obsidian")
			ctx := context.Background()
			if tc.name != "entries" && tc.name != "create" && tc.name != "append on create" {
				if _, err := h.app.Add(ctx, app.AddOptions{
					Scope: "lesson", Title: "A rule", Body: "The first part.",
				}); err != nil {
					t.Fatal(err)
				}
			}
			h.app.Vault = tc.wrap(h.app.Vault)
			if _, err := h.app.Add(ctx, tc.opts); err == nil {
				t.Fatal("the vault failure was swallowed")
			}
		})
	}
}

func TestAddFailsWhenTheIndexFails(t *testing.T) {
	for _, appendExisting := range []bool{false, true} {
		name := "create"
		if appendExisting {
			name = "append"
		}
		t.Run(name, func(t *testing.T) {
			h := newHarness(t, "obsidian")
			ctx := context.Background()
			if appendExisting {
				if _, err := h.app.Add(ctx, app.AddOptions{
					Scope: "lesson", Title: "A rule", Body: "The first part.",
				}); err != nil {
					t.Fatal(err)
				}
			}
			blocked := filepath.Join(t.TempDir(), "blocked")
			if err := os.WriteFile(blocked, []byte("not a directory"), 0o644); err != nil {
				t.Fatal(err)
			}
			h.app.Projections = projection.New(blocked)
			opts := app.AddOptions{Scope: "lesson", Title: "A rule", Body: "The rule."}
			if appendExisting {
				opts.Body = "The second part."
			}
			if _, err := h.app.Add(ctx, opts); err == nil {
				t.Fatal("the index failure was swallowed")
			}
		})
	}
}

func TestAddResolvesTheWorktreeScopeAndItsErrors(t *testing.T) {
	repo := newRepo(t)
	cases := []struct {
		name string
		opts app.AddOptions
		want string
	}{
		{
			"auto needs a project",
			app.AddOptions{Scope: "lesson", Title: "A", Body: "A body.", WorktreeAuto: true, CWD: repo},
			"needs a project",
		},
		{
			"auto outside a repository",
			app.AddOptions{Scope: "lesson", Title: "A", Body: "A body.", WorktreeAuto: true, Project: "demo", CWD: t.TempDir()},
			"git repository",
		},
		{
			"auto resolves another repository",
			app.AddOptions{Scope: "lesson", Title: "A", Body: "A body.", WorktreeAuto: true, Project: "other", CWD: repo},
			"resolved",
		},
		{
			"unsafe worktree",
			app.AddOptions{Scope: "lesson", Title: "A", Body: "A body.", Project: "demo", Worktree: "../x"},
			"invalid worktree",
		},
		{
			"worktree without project",
			app.AddOptions{Scope: "lesson", Title: "A", Body: "A body.", Worktree: "feature"},
			"needs a project",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t, "obsidian")
			_, err := h.app.Add(context.Background(), tc.opts)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("the error is %v, want %q", err, tc.want)
			}
		})
	}
}

func TestAddTeamAppendsAndDetectsEquals(t *testing.T) {
	h := newHarness(t, "obsidian")
	ctx := context.Background()
	repo := newRepo(t)
	opts := app.AddOptions{
		Scope: "lesson", Title: "A shared rule", Body: "The first part.",
		Team: true, CWD: repo,
	}
	if _, err := h.app.Add(ctx, opts); err != nil {
		t.Fatal(err)
	}
	opts.Body = "The second part."
	message, err := h.app.Add(ctx, opts)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(message, "appended (team):") {
		t.Fatalf("the message is %q", message)
	}
	opts.Body = "The second part."
	message, err = h.app.Add(ctx, opts)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(message, "already recorded (team):") {
		t.Fatalf("the message is %q", message)
	}
}

func TestAddTeamRejectsScopeMismatches(t *testing.T) {
	repo := newRepo(t)
	slug := memory.Slugify("repo")
	cases := []struct {
		name string
		opts app.AddOptions
		want string
	}{
		{
			"project mismatch",
			app.AddOptions{Scope: "lesson", Title: "A", Body: "A body.", Team: true, Project: "other", CWD: repo},
			"belongs to",
		},
		{
			"worktree auto mismatch",
			app.AddOptions{Scope: "lesson", Title: "A", Body: "A body.", Team: true, WorktreeAuto: true, Project: "other", CWD: repo},
			"resolved",
		},
		{
			"invalid worktree",
			app.AddOptions{Scope: "lesson", Title: "A", Body: "A body.", Team: true, Project: slug, Worktree: "../x", CWD: repo},
			"invalid worktree",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t, "obsidian")
			_, err := h.app.Add(context.Background(), tc.opts)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("the error is %v, want %q", err, tc.want)
			}
		})
	}
}

func TestAddTeamFailsWhenTheTeamStoreIsBroken(t *testing.T) {
	h := newHarness(t, "obsidian")
	repo := newRepo(t)
	if err := os.MkdirAll(filepath.Join(repo, ".qwen"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, ".qwen", "team-memory"), []byte("not a directory"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := h.app.Add(context.Background(), app.AddOptions{
		Scope: "lesson", Title: "A rule", Body: "The rule.", Native: "qwen",
		Team: true, CWD: repo,
	})
	if err == nil || !strings.Contains(err.Error(), "non-directory") {
		t.Fatalf("the team store failure is %v", err)
	}
}

func TestTeamStoreErrorsReachBriefSearchAndRecall(t *testing.T) {
	for _, operation := range []string{"brief", "search", "recall"} {
		t.Run(operation, func(t *testing.T) {
			h := newHarness(t, "obsidian")
			ctx := context.Background()
			repo := newRepo(t)
			if err := os.MkdirAll(filepath.Join(repo, ".agents"), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(repo, ".agents", "memory"), []byte("not a directory"), 0o644); err != nil {
				t.Fatal(err)
			}
			var err error
			switch operation {
			case "brief":
				_, err = h.app.Brief(ctx, app.BriefOptions{CWD: repo})
			case "search":
				t.Chdir(repo)
				_, err = h.app.Search(ctx, "rule", 10)
			case "recall":
				_, err = h.app.Recall(ctx, app.RecallOptions{Query: "rule", CWD: repo})
			}
			if err == nil {
				t.Fatal("the broken team store was ignored")
			}
		})
	}
}

func TestSearchEdgeCases(t *testing.T) {
	t.Run("empty query", func(t *testing.T) {
		h := newHarness(t, "obsidian")
		if _, err := h.app.Search(context.Background(), "   ", 10); err == nil {
			t.Fatal("an empty query was accepted")
		}
	})
	t.Run("no private store", func(t *testing.T) {
		h := newHarness(t, "obsidian")
		h.app.Vault = nil
		if _, err := h.app.Search(context.Background(), "anything", 10); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("private search fails", func(t *testing.T) {
		h := newHarness(t, "obsidian")
		h.app.Vault = &faultyVault{Vault: h.app.Vault, searchErr: errors.New("search failed")}
		if _, err := h.app.Search(context.Background(), "anything", 10); err == nil {
			t.Fatal("the search failure was swallowed")
		}
	})
	t.Run("limit truncates", func(t *testing.T) {
		h := newHarness(t, "obsidian")
		ctx := context.Background()
		if _, err := h.app.Add(ctx, app.AddOptions{
			Scope: "lesson", Title: "A repeated rule",
			Body: "needle one\nneedle two\nneedle three",
		}); err != nil {
			t.Fatal(err)
		}
		hits, err := h.app.Search(ctx, "needle", 1)
		if err != nil {
			t.Fatal(err)
		}
		if len(hits) != 1 {
			t.Fatalf("the limit was ignored: %+v", hits)
		}
	})
}

func TestBriefEdgeCases(t *testing.T) {
	cases := []struct {
		name string
		opts app.BriefOptions
		want string
	}{
		{"invalid project", app.BriefOptions{Project: "bad/slug"}, "invalid project slug"},
		{"invalid worktree", app.BriefOptions{Project: "demo", Worktree: "bad/id"}, "invalid worktree id"},
		{"worktree without project", app.BriefOptions{Worktree: "feature", Global: true}, "needs a project"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t, "obsidian")
			_, err := h.app.Brief(context.Background(), tc.opts)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("the error is %v, want %q", err, tc.want)
			}
		})
	}
	t.Run("entries fail", func(t *testing.T) {
		h := newHarness(t, "obsidian")
		h.app.Vault = &faultyVault{Vault: h.app.Vault, entriesErr: errors.New("entries failed")}
		if _, err := h.app.Brief(context.Background(), app.BriefOptions{}); err == nil {
			t.Fatal("the entries failure was swallowed")
		}
	})
	t.Run("project note fails", func(t *testing.T) {
		h := newHarness(t, "obsidian")
		h.app.Vault = &notesErrorVault{Vault: h.app.Vault, projectErr: errors.New("note failed")}
		if _, err := h.app.Brief(context.Background(), app.BriefOptions{Project: "demo", ProjectExplicit: true}); err == nil {
			t.Fatal("the project note failure was swallowed")
		}
	})
	t.Run("worktree note fails", func(t *testing.T) {
		h := newHarness(t, "obsidian")
		h.app.Vault = &notesErrorVault{Vault: h.app.Vault, worktreeErr: errors.New("note failed")}
		if _, err := h.app.Brief(context.Background(), app.BriefOptions{Project: "demo", Worktree: "feature"}); err == nil {
			t.Fatal("the worktree note failure was swallowed")
		}
	})
	t.Run("gateway without worktree notes", func(t *testing.T) {
		h := newHarness(t, "obsidian")
		h.app.Vault = &projectOnlyVault{Vault: h.app.Vault}
		_, err := h.app.Brief(context.Background(), app.BriefOptions{Project: "demo", Worktree: "feature"})
		if err == nil || !strings.Contains(err.Error(), "does not keep worktree notes") {
			t.Fatalf("the error is %v", err)
		}
	})
}

func TestBriefCarriesHotScopedAndTeamSections(t *testing.T) {
	h := newHarness(t, "obsidian")
	ctx := context.Background()
	repo := newRepo(t)
	slug := memory.Slugify("repo")

	if _, err := h.app.Add(ctx, app.AddOptions{
		Scope: "user", Title: "Hot identity", Body: "The hot profile.", Tier: "hot",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := h.app.Add(ctx, app.AddOptions{
		Scope: "project", Title: "Scoped rule", Body: "The scoped body.", Project: slug,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := h.app.Add(ctx, app.AddOptions{
		Scope: "lesson", Title: "A shared rule", Body: "The shared body.", Team: true, CWD: repo,
	}); err != nil {
		t.Fatal(err)
	}
	text, err := h.app.Brief(ctx, app.BriefOptions{CWD: repo})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, "The hot profile.") {
		t.Fatalf("the hot body is missing:\n%s", text)
	}
	if !strings.Contains(text, "(hot)") {
		t.Fatalf("the hot marker is missing:\n%s", text)
	}
	if !strings.Contains(text, "## Project: "+slug) || !strings.Contains(text, "The scoped body.") {
		t.Fatalf("the project section is missing:\n%s", text)
	}
	if !strings.Contains(text, "## Team Memory ("+slug+")") {
		t.Fatalf("the team section is missing:\n%s", text)
	}
}

func TestSetPinnedEdgeCases(t *testing.T) {
	t.Run("empty title", func(t *testing.T) {
		h := newHarness(t, "obsidian")
		if _, err := h.app.SetPinned(context.Background(), "  ", true); err == nil {
			t.Fatal("an empty title was accepted")
		}
	})
	t.Run("not found", func(t *testing.T) {
		h := newHarness(t, "obsidian")
		if _, err := h.app.SetPinned(context.Background(), "Missing", true); err == nil {
			t.Fatal("a missing entry was pinned")
		}
	})
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
		if _, err := h.app.SetPinned(ctx, "Twice", true); err == nil {
			t.Fatal("an ambiguous title was pinned")
		}
	})
	t.Run("entries fail", func(t *testing.T) {
		h := newHarness(t, "obsidian")
		h.app.Vault = &faultyVault{Vault: h.app.Vault, entriesErr: errors.New("entries failed")}
		if _, err := h.app.SetPinned(context.Background(), "A rule", true); err == nil {
			t.Fatal("the entries failure was swallowed")
		}
	})
}

func TestSetPinnedReportsWriteFailures(t *testing.T) {
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
				Scope: "lesson", Title: "A rule", Body: "The rule.",
			}); err != nil {
				t.Fatal(err)
			}
			h.app.Vault = tc.wrap(h.app.Vault)
			if _, err := h.app.SetPinned(ctx, "A rule", true); err == nil {
				t.Fatal("the write failure was swallowed")
			}
		})
	}
	t.Run("index", func(t *testing.T) {
		h := newHarness(t, "obsidian")
		ctx := context.Background()
		if _, err := h.app.Add(ctx, app.AddOptions{
			Scope: "lesson", Title: "A rule", Body: "The rule.",
		}); err != nil {
			t.Fatal(err)
		}
		blocked := filepath.Join(t.TempDir(), "blocked")
		if err := os.WriteFile(blocked, []byte("not a directory"), 0o644); err != nil {
			t.Fatal(err)
		}
		h.app.Projections = projection.New(blocked)
		if _, err := h.app.SetPinned(ctx, "A rule", true); err == nil {
			t.Fatal("the index failure was swallowed")
		}
	})
}

func TestNewAndAccessors(t *testing.T) {
	if _, err := app.New(vault.Config{Backend: "not-a-backend"}, "", os.Stdout, os.Stderr); err == nil {
		t.Fatal("an unknown backend was accepted")
	}
	h := newHarness(t, "obsidian")
	ctx := context.Background()
	if err := h.app.RequireStore(ctx); err != nil {
		t.Fatal(err)
	}
	if h.app.Path() == "" {
		t.Fatal("the store path is empty")
	}
	layout := h.app.Layout()
	if layout.Corpus != filepath.Join(h.root, "corpus") || layout.Hot != filepath.Join(h.root, "MEMORY.md") {
		t.Fatalf("the layout is %+v", layout)
	}
}

func TestIndexEdgeCases(t *testing.T) {
	t.Run("entries fail", func(t *testing.T) {
		h := newHarness(t, "obsidian")
		h.app.Vault = &faultyVault{Vault: h.app.Vault, entriesErr: errors.New("entries failed")}
		if _, err := h.app.Index(context.Background()); err == nil {
			t.Fatal("the entries failure was swallowed")
		}
	})
	t.Run("import fails", func(t *testing.T) {
		h := newHarness(t, "obsidian")
		ctx := context.Background()
		if _, err := h.app.Add(ctx, app.AddOptions{
			Scope: "lesson", Title: "A durable lesson", Body: "The rule. Why it matters.",
		}); err != nil {
			t.Fatal(err)
		}
		topic := native.RenderTopic(memory.Entry{
			Title: "A durable lesson", Type: memory.TypeLesson,
			Body: "A newer native body.", Description: "A native edit",
		})
		if err := h.app.Projections.Write(native.TargetMiniMaxAgent, "topics/a-durable-lesson.md", topic); err != nil {
			t.Fatal(err)
		}
		future := time.Now().Add(2 * time.Second)
		if err := os.Chtimes(filepath.Join(h.root, "native", "minimax-agent", "topics", "a-durable-lesson.md"), future, future); err != nil {
			t.Fatal(err)
		}
		h.app.Vault = &faultyVault{Vault: h.app.Vault, updateErr: errors.New("update failed")}
		if _, err := h.app.Index(ctx); err == nil {
			t.Fatal("the import failure was swallowed")
		}
	})
	t.Run("hot tier fails", func(t *testing.T) {
		h := newHarness(t, "obsidian")
		if err := os.Mkdir(filepath.Join(h.root, "MEMORY.md"), 0o755); err != nil {
			t.Fatal(err)
		}
		if _, err := h.app.Index(context.Background()); err == nil {
			t.Fatal("the hot tier failure was swallowed")
		}
	})
	t.Run("export fails", func(t *testing.T) {
		h := newHarness(t, "obsidian")
		if err := os.MkdirAll(filepath.Join(h.root, "native", "minimax-agent", "MEMORY.md"), 0o755); err != nil {
			t.Fatal(err)
		}
		if _, err := h.app.Index(context.Background()); err == nil {
			t.Fatal("the export failure was swallowed")
		}
	})
	t.Run("projection state fails", func(t *testing.T) {
		h := newHarness(t, "obsidian")
		if err := os.MkdirAll(filepath.Join(h.root, ".smart-agents-memory-state.json"), 0o755); err != nil {
			t.Fatal(err)
		}
		if _, err := h.app.Index(context.Background()); err == nil {
			t.Fatal("the projection state failure was swallowed")
		}
	})
	t.Run("index publish fails", func(t *testing.T) {
		h := newHarness(t, "obsidian")
		if err := os.MkdirAll(filepath.Join(h.root, "Smart Agents Memory.md"), 0o755); err != nil {
			t.Fatal(err)
		}
		if _, err := h.app.Index(context.Background()); err == nil {
			t.Fatal("the index publish failure was swallowed")
		}
	})
}

func TestMigrateEdgeCases(t *testing.T) {
	writeNote := func(t *testing.T, h *harness, rel, content string) {
		t.Helper()
		path := filepath.Join(h.root, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	const legacyLesson = "---\ntitle: \"Uma licao\"\ntype: licao\ndate: 2026-11-07\nsource: \"memoria-cli\"\n---\n\n## Uma licao\n\nThe body.\n"

	t.Run("entries fail", func(t *testing.T) {
		h := newHarness(t, "obsidian")
		h.app.Vault = &faultyVault{Vault: h.app.Vault, entriesErr: errors.New("entries failed")}
		if _, err := h.app.Migrate(context.Background(), false); err == nil {
			t.Fatal("the entries failure was swallowed")
		}
	})
	t.Run("remove fails", func(t *testing.T) {
		h := newHarness(t, "obsidian")
		ctx := context.Background()
		duplicate := "---\ntitle: \"Nome\"\ntype: usuario\ndate: 2026-11-07\n---\n\n## Nome\n\n- Keep the name.\n"
		writeNote(t, h, "corpus/user/nome.md", duplicate)
		original := "---\ntitle: \"Name\"\ntype: usuario\ndate: 2026-11-07\n---\n\n## Name\n\n- Keep the name.\n"
		writeNote(t, h, "corpus/user/name.md", original)
		h.app.Vault = &removerErrorVault{Vault: h.app.Vault}
		if _, err := h.app.Migrate(ctx, true); err == nil {
			t.Fatal("the removal failure was swallowed")
		}
	})
	t.Run("rename fails", func(t *testing.T) {
		h := newHarness(t, "obsidian")
		writeNote(t, h, "corpus/lesson/legacy.md",
			"---\ntitle: \"Reconciliação do corpus: mtime decide quem manda\"\ntype: lesson\ndate: 2026-11-07\n---\n\nBody.\n")
		h.app.Vault = &renameErrorVault{Vault: h.app.Vault}
		if _, err := h.app.Migrate(context.Background(), true); err == nil {
			t.Fatal("the rename failure was swallowed")
		}
	})
	t.Run("update fails", func(t *testing.T) {
		h := newHarness(t, "obsidian")
		writeNote(t, h, "corpus/lesson/legacy.md", legacyLesson)
		h.app.Vault = &faultyVault{Vault: h.app.Vault, updateErr: errors.New("update failed")}
		if _, err := h.app.Migrate(context.Background(), true); err == nil {
			t.Fatal("the update failure was swallowed")
		}
	})
	t.Run("orphans fail", func(t *testing.T) {
		h := newHarness(t, "obsidian")
		if err := os.MkdirAll(filepath.Join(h.root, "native", "minimax-agent"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(h.root, "native", "minimax-agent", "topics"), []byte("not a directory"), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := h.app.Migrate(context.Background(), false); err == nil {
			t.Fatal("the orphan listing failure was swallowed")
		}
	})
	t.Run("hot markers fail", func(t *testing.T) {
		h := newHarness(t, "obsidian")
		hot := "---\ndescription: Hot\ntype: index\n---\n\n<!-- memoria:hot:start -->\n## Profile\n\n<!-- memoria:hot:end -->\n"
		if err := os.WriteFile(filepath.Join(h.root, "MEMORY.md"), []byte(hot), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(filepath.Join(h.root, "MEMORY.md"), 0o444); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(filepath.Join(h.root, "MEMORY.md"), 0o644) })
		if _, err := h.app.Migrate(context.Background(), true); err == nil {
			t.Fatal("the hot marker write failure was swallowed")
		}
	})
	t.Run("projection state fails", func(t *testing.T) {
		h := newHarness(t, "obsidian")
		if err := os.MkdirAll(filepath.Join(h.root, ".smart-agents-memory-state.json"), 0o755); err != nil {
			t.Fatal(err)
		}
		if _, err := h.app.Migrate(context.Background(), true); err == nil {
			t.Fatal("the projection state failure was swallowed")
		}
	})
}
