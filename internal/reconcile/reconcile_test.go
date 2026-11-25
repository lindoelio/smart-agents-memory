package reconcile

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lindoelio/smart-agents-memory/internal/memory"
	"github.com/lindoelio/smart-agents-memory/internal/native"
	"github.com/lindoelio/smart-agents-memory/internal/projection"
	"github.com/lindoelio/smart-agents-memory/internal/vault"
)

// fakeVault is an in-memory corpus that can fail on command.
type fakeVault struct {
	entries    []memory.Entry
	createErr  error
	updateErr  error
	entriesErr error
	created    []memory.Entry
	updated    []memory.Entry
}

func (v *fakeVault) Kind() string { return "fake" }
func (v *fakeVault) Root() string { return "" }
func (v *fakeVault) Ready(context.Context) error {
	return nil
}
func (v *fakeVault) Search(context.Context, string, int) ([]vault.Hit, error) { return nil, nil }
func (v *fakeVault) AppendLog(context.Context, string) error                  { return nil }
func (v *fakeVault) LogLines(context.Context, int) ([]string, error)          { return nil, nil }

func (v *fakeVault) Entries(context.Context) ([]memory.Entry, error) {
	if v.entriesErr != nil {
		return nil, v.entriesErr
	}
	return append([]memory.Entry(nil), v.entries...), nil
}

func (v *fakeVault) Create(_ context.Context, e memory.Entry) (memory.Entry, error) {
	if v.createErr != nil {
		return e, v.createErr
	}
	e.Path = "entries/" + memory.Slugify(e.Title)
	if e.ModTime.IsZero() {
		e.ModTime = time.Now()
	}
	v.entries = append(v.entries, e)
	v.created = append(v.created, e)
	return e, nil
}

func (v *fakeVault) Update(_ context.Context, e memory.Entry) error {
	if v.updateErr != nil {
		return v.updateErr
	}
	for i := range v.entries {
		if memory.NormTitle(v.entries[i].Title) == memory.NormTitle(e.Title) {
			v.entries[i] = e
			v.updated = append(v.updated, e)
			return nil
		}
	}
	v.entries = append(v.entries, e)
	v.updated = append(v.updated, e)
	return nil
}

func newReconciler(t *testing.T) (*Reconciler, *fakeVault, *projection.Store) {
	t.Helper()
	projections := projection.New(t.TempDir())
	if err := projections.EnsureDirs(); err != nil {
		t.Fatal(err)
	}
	vault := &fakeVault{}
	return &Reconciler{Vault: vault, Projections: projections, Warn: &bytes.Buffer{}}, vault, projections
}

// writeProjection writes a native file and sets its modification time.
func writeProjection(t *testing.T, store *projection.Store, target native.Target, rel, content string, mod time.Time) {
	t.Helper()
	if err := store.Write(target, rel, content); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(store.TargetDir(target), rel)
	if err := os.Chtimes(path, mod, mod); err != nil {
		t.Fatal(err)
	}
}

func TestImportAddsEveryNativeEntry(t *testing.T) {
	ctx := context.Background()
	r, vault, projections := newReconciler(t)
	future := time.Now().Add(time.Minute)

	writeProjection(t, projections, native.TargetMiniMaxAgent, "MEMORY.md",
		native.RenderAgent(memory.Entry{Title: "An agent lesson", Body: "The agent body."}), future)
	writeProjection(t, projections, native.TargetMiniMaxUser, "user.md",
		native.RenderUser(memory.Entry{Title: "A preference", Body: "The user body."}), future)
	writeProjection(t, projections, native.TargetMiniMaxAgent, "topics/a-topic.md",
		native.RenderTopic(memory.Entry{Title: "A topic", Body: "The topic body."}), future)
	writeProjection(t, projections, native.TargetQwen, "a-qwen.md",
		native.RenderQwen(memory.Entry{Title: "A qwen fact", Body: "The qwen body.", NativeType: "reference"}), future)
	writeProjection(t, projections, native.TargetQwen, "pinned/a-pinned.md",
		native.RenderQwen(memory.Entry{Title: "A pinned fact", Body: "The pinned body.", NativeType: "reference"}), future)
	writeProjection(t, projections, native.TargetQwen, "MEMORY.md", "- index\n", future)

	added, updated, err := r.Import(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if added != 5 || updated != 0 {
		t.Fatalf("the import is %d added, %d updated", added, updated)
	}
	want := map[string]bool{
		"An agent lesson": true, "A preference": true, "A topic": true,
		"A qwen fact": true, "A pinned fact": true,
	}
	for _, e := range vault.created {
		delete(want, e.Title)
	}
	if len(want) != 0 {
		t.Fatalf("the import is missing %v", want)
	}
}

func TestImportSkipsAProjectionTheToolDidNotWrite(t *testing.T) {
	ctx := context.Background()
	r, vault, projections := newReconciler(t)
	if err := projections.SetLastProjection(time.Now()); err != nil {
		t.Fatal(err)
	}
	writeProjection(t, projections, native.TargetMiniMaxAgent, "MEMORY.md",
		native.RenderAgent(memory.Entry{Title: "An old lesson", Body: "Body."}),
		time.Now().Add(-time.Hour))
	added, updated, err := r.Import(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if added != 0 || updated != 0 || len(vault.created) != 0 {
		t.Fatalf("an old projection was imported: %d %d", added, updated)
	}
}

func TestImportSkipsPinnedAndKnownBodies(t *testing.T) {
	ctx := context.Background()
	r, vault, projections := newReconciler(t)
	future := time.Now().Add(time.Minute)
	writeProjection(t, projections, native.TargetMiniMaxAgent, "MEMORY.md",
		native.RenderAgent(memory.Entry{Title: "A pinned rule", Body: "The pinned body."})+
			native.RenderAgent(memory.Entry{Title: "A known rule", Body: "The known body."}), future)

	corpus := []memory.Entry{
		{Title: "A pinned rule", Type: memory.TypeLesson, Body: "Old.", Pinned: true,
			ModTime: time.Now().Add(-time.Hour)},
		{Title: "A known rule", Type: memory.TypeLesson, Body: "The known body.",
			ModTime: time.Now().Add(-time.Hour)},
	}
	added, updated, err := r.Import(ctx, corpus)
	if err != nil {
		t.Fatal(err)
	}
	if added != 0 || updated != 0 || len(vault.created) != 0 || len(vault.updated) != 0 {
		t.Fatalf("a protected entry was imported: %d %d", added, updated)
	}
}

func TestImportKeepsTheCorpusWhenItIsNewer(t *testing.T) {
	ctx := context.Background()
	r, vault, projections := newReconciler(t)
	writeProjection(t, projections, native.TargetMiniMaxAgent, "MEMORY.md",
		native.RenderAgent(memory.Entry{Title: "A rule", Body: "The projection body."}), time.Now())
	corpus := []memory.Entry{{
		Title: "A rule", Type: memory.TypeLesson, Body: "The corpus body.",
		ModTime: time.Now().Add(time.Hour),
	}}
	added, updated, err := r.Import(ctx, corpus)
	if err != nil {
		t.Fatal(err)
	}
	if added != 0 || updated != 0 || len(vault.updated) != 0 {
		t.Fatalf("an older projection replaced the corpus: %d %d", added, updated)
	}
}

func TestImportUpdatesAnOlderCorpusEntry(t *testing.T) {
	ctx := context.Background()
	r, vault, projections := newReconciler(t)
	candidate := memory.Entry{
		Title: "A rule", Body: "The projection body.", Description: "A description",
		Date: "2026-11-07", Reason: "because",
	}
	writeProjection(t, projections, native.TargetMiniMaxAgent, "topics/a-rule.md",
		native.RenderTopic(candidate), time.Now().Add(time.Minute))
	corpus := []memory.Entry{{
		Title: "A rule", Type: memory.TypeProject, Body: "The corpus body.",
		ModTime: time.Now().Add(-time.Hour),
	}}
	added, updated, err := r.Import(ctx, corpus)
	if err != nil {
		t.Fatal(err)
	}
	if added != 0 || updated != 1 || len(vault.updated) != 1 {
		t.Fatalf("the import is %d added, %d updated", added, updated)
	}
	got := vault.updated[0]
	if got.Body != "The projection body." || got.Description != "A description" ||
		got.NativeFile != "a-rule.md" {
		t.Fatalf("the update is %+v", got)
	}
	// The corpus keeps the type the projection cannot know.
	if got.Type != memory.TypeProject {
		t.Fatalf("the type became %q", got.Type)
	}
}

func TestImportSkipsASecret(t *testing.T) {
	ctx := context.Background()
	r, vault, projections := newReconciler(t)
	writeProjection(t, projections, native.TargetMiniMaxAgent, "MEMORY.md",
		native.RenderAgent(memory.Entry{
			Title: "A leaked key", Body: "token ghp_" + strings.Repeat("a", 36),
		}), time.Now().Add(time.Minute))
	added, updated, err := r.Import(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if added != 0 || updated != 0 || len(vault.created) != 0 {
		t.Fatalf("a secret was imported: %d %d", added, updated)
	}
	warnings := r.Warn.(*bytes.Buffer).String()
	if !strings.Contains(warnings, "skipped native entry") {
		t.Fatalf("the warning is %q", warnings)
	}
}

func TestImportReportsItsWriteErrors(t *testing.T) {
	ctx := context.Background()
	t.Run("create", func(t *testing.T) {
		r, vault, projections := newReconciler(t)
		vault.createErr = errors.New("cannot create")
		writeProjection(t, projections, native.TargetMiniMaxAgent, "MEMORY.md",
			native.RenderAgent(memory.Entry{Title: "A new lesson", Body: "Body."}), time.Now().Add(time.Minute))
		if _, _, err := r.Import(ctx, nil); err == nil ||
			!strings.Contains(err.Error(), "cannot import") {
			t.Fatalf("the error is %v", err)
		}
	})
	t.Run("update", func(t *testing.T) {
		r, vault, projections := newReconciler(t)
		vault.updateErr = errors.New("cannot update")
		writeProjection(t, projections, native.TargetMiniMaxAgent, "MEMORY.md",
			native.RenderAgent(memory.Entry{Title: "A rule", Body: "New body."}), time.Now().Add(time.Minute))
		corpus := []memory.Entry{{
			Title: "A rule", Type: memory.TypeLesson, Body: "Old body.",
			ModTime: time.Now().Add(-time.Hour),
		}}
		if _, _, err := r.Import(ctx, corpus); err == nil ||
			!strings.Contains(err.Error(), "cannot update") {
			t.Fatalf("the error is %v", err)
		}
	})
}

// exportCorpus is the corpus the export tests project.
func exportCorpus() []memory.Entry {
	return []memory.Entry{
		{Title: "Hot agent", Type: memory.TypeLesson, Tier: memory.TierHot, Native: "minimax-agent", Body: "Hot agent body."},
		{Title: "Hot user", Type: memory.TypeUser, Tier: memory.TierHot, Native: "minimax-user", Body: "Hot user body."},
		{Title: "Cold agent", Type: memory.TypeLesson, Native: "minimax-agent", Body: "Cold agent body.", Description: "A cold topic"},
		{Title: "Cold user", Type: memory.TypeUser, Native: "minimax-user", Body: "Cold user body."},
		{Title: "Qwen root", Type: memory.TypeLesson, Native: "qwen", Body: "Qwen root body.", NativeFile: "legacy/other-name.md"},
		{Title: "Qwen pinned", Type: memory.TypeLesson, Native: "qwen", Pinned: true, Body: "Qwen pinned body.", NativeFile: "pinned-name"},
	}
}

func TestExportWritesAndRemovesTheProjections(t *testing.T) {
	ctx := context.Background()
	r, _, projections := newReconciler(t)
	if err := projections.Write(native.TargetMiniMaxAgent, "topics/stale.md", "stale"); err != nil {
		t.Fatal(err)
	}
	if err := projections.Write(native.TargetQwen, "stale.md", "stale"); err != nil {
		t.Fatal(err)
	}
	if err := projections.Write(native.TargetQwen, "pinned/stale.md", "stale"); err != nil {
		t.Fatal(err)
	}

	changed, err := r.Export(ctx, exportCorpus())
	if err != nil {
		t.Fatal(err)
	}
	if changed == 0 {
		t.Fatal("the export changed nothing")
	}

	agent, err := projections.Read(native.TargetMiniMaxAgent, "MEMORY.md")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(agent, "### Hot agent") || strings.Contains(agent, "Cold agent") {
		t.Fatalf("the agent store is:\n%s", agent)
	}
	user, err := projections.Read(native.TargetMiniMaxUser, "user.md")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(user, native.HotProfileStart) || !strings.Contains(user, "### Hot user") {
		t.Fatalf("the user store is:\n%s", user)
	}
	for _, rel := range []string{"topics/cold-agent.md", "topics/cold-user.md"} {
		if !projections.Exists(native.TargetMiniMaxAgent, rel) {
			t.Fatalf("the topic %s is missing", rel)
		}
	}
	if projections.Exists(native.TargetMiniMaxAgent, "topics/stale.md") {
		t.Fatal("a stale topic survived")
	}
	// The directory of an old NativeFile is normalized; only the base name
	// and the pinned state decide where the file lands.
	if !projections.Exists(native.TargetQwen, "other-name.md") {
		t.Fatal("the qwen root file is missing")
	}
	if !projections.Exists(native.TargetQwen, "pinned/pinned-name.md") {
		t.Fatal("the qwen pinned file is missing")
	}
	if projections.Exists(native.TargetQwen, "stale.md") || projections.Exists(native.TargetQwen, "pinned/stale.md") {
		t.Fatal("a stale qwen file survived")
	}
	index, err := projections.Read(native.TargetQwen, "MEMORY.md")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(index, "other-name.md") || !strings.Contains(index, "pinned/pinned-name.md") {
		t.Fatalf("the qwen index is:\n%s", index)
	}

	again, err := r.Export(ctx, exportCorpus())
	if err != nil {
		t.Fatal(err)
	}
	if again != 0 {
		t.Fatalf("the second export changed %d files", again)
	}
}

func TestExportReportsItsFailures(t *testing.T) {
	ctx := context.Background()

	t.Run("the root is a file", func(t *testing.T) {
		file := filepath.Join(t.TempDir(), "root-file")
		if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		r := &Reconciler{Vault: &fakeVault{}, Projections: projection.New(file)}
		if _, err := r.Export(ctx, exportCorpus()); err == nil {
			t.Fatal("the export wrote under a file")
		}
	})

	t.Run("the topics folder is a file", func(t *testing.T) {
		r, _, projections := newReconciler(t)
		topics := filepath.Join(projections.TargetDir(native.TargetMiniMaxAgent), "topics")
		if err := os.RemoveAll(topics); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(topics, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := r.Export(ctx, nil); err == nil {
			t.Fatal("the export listed a topics file")
		}
	})
}

func TestQwenFilenameAndRel(t *testing.T) {
	entry := memory.Entry{Title: "A title", NativeFile: "legacy/other.md"}
	if got := qwenFilename(entry); got != "other.md" {
		t.Fatalf("the filename is %q", got)
	}
	if got := qwenRel(entry); got != "other.md" {
		t.Fatalf("the rel is %q", got)
	}
	if got := qwenFilename(memory.Entry{Title: "A title", NativeFile: "."}); got != "a-title.md" {
		t.Fatalf("a dot filename is %q", got)
	}
	if got := qwenFilename(memory.Entry{Title: "A title", NativeFile: "name"}); got != "name.md" {
		t.Fatalf("the missing suffix became %q", got)
	}
	pinned := memory.Entry{Title: "A title", Pinned: true}
	if got := qwenRel(pinned); got != "pinned/a-title.md" {
		t.Fatalf("the pinned rel is %q", got)
	}
}

func TestReconcileHotCreatesTheFile(t *testing.T) {
	ctx := context.Background()
	r, _, _ := newReconciler(t)
	hotPath := filepath.Join(t.TempDir(), "MEMORY.md")
	corpus := []memory.Entry{
		{Title: "A hot rule", Type: memory.TypeUser, Tier: memory.TierHot, Body: "The body."},
	}
	added, updated, err := r.ReconcileHot(ctx, hotPath, corpus)
	if err != nil {
		t.Fatal(err)
	}
	if added != 0 || updated != 0 {
		t.Fatalf("a missing file imported %d %d", added, updated)
	}
	data, err := os.ReadFile(hotPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "description:") || !strings.Contains(string(data), "### A hot rule") {
		t.Fatalf("the hot file is:\n%s", data)
	}
}

func TestReconcileHotImportsAFreeformBlock(t *testing.T) {
	ctx := context.Background()
	r, vault, _ := newReconciler(t)
	hotPath := filepath.Join(t.TempDir(), "MEMORY.md")
	content := "---\ndescription: mine\n---\n\n" + native.HotProfileStart + "\nA freeform fact.\n" + native.HotProfileEnd + "\n"
	if err := os.WriteFile(hotPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	added, updated, err := r.ReconcileHot(ctx, hotPath, nil)
	if err != nil {
		t.Fatal(err)
	}
	if added != 1 || updated != 0 {
		t.Fatalf("the import is %d %d", added, updated)
	}
	if len(vault.created) != 1 || vault.created[0].Title != ProfileTitle || vault.created[0].Tier != memory.TierHot {
		t.Fatalf("the profile is %+v", vault.created)
	}
	data, err := os.ReadFile(hotPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "description: mine") || !strings.Contains(string(data), "A freeform fact.") {
		t.Fatalf("the file was rewritten as:\n%s", data)
	}
}

func TestReconcileHotImportsHeadings(t *testing.T) {
	ctx := context.Background()
	r, vault, _ := newReconciler(t)
	hotPath := filepath.Join(t.TempDir(), "MEMORY.md")
	content := native.HotProfileStart + "\n### A preference\n\nThe body.\n" + native.HotProfileEnd
	if err := os.WriteFile(hotPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	added, updated, err := r.ReconcileHot(ctx, hotPath, nil)
	if err != nil {
		t.Fatal(err)
	}
	if added != 1 || updated != 0 || len(vault.created) != 1 || vault.created[0].Title != "A preference" {
		t.Fatalf("the import is %d %d %+v", added, updated, vault.created)
	}
}

func TestReconcileHotSkipsProtectedEntries(t *testing.T) {
	ctx := context.Background()
	now := time.Now()
	block := native.HotProfileStart + "\n### A rule\n\nNew body.\n" + native.HotProfileEnd

	cases := map[string][]memory.Entry{
		"pinned": {{Title: "A rule", Tier: memory.TierHot, Body: "Old body.", Pinned: true,
			ModTime: now.Add(-time.Hour)}},
		"newer corpus": {{Title: "A rule", Tier: memory.TierHot, Body: "Old body.",
			ModTime: now.Add(time.Hour)}},
		"same body": {{Title: "A rule", Tier: memory.TierHot, Body: "New body.",
			ModTime: now.Add(-time.Hour)}},
	}
	for name, corpus := range cases {
		t.Run(name, func(t *testing.T) {
			r, vault, _ := newReconciler(t)
			hotPath := filepath.Join(t.TempDir(), "MEMORY.md")
			if err := os.WriteFile(hotPath, []byte(block), 0o644); err != nil {
				t.Fatal(err)
			}
			added, updated, err := r.ReconcileHot(ctx, hotPath, corpus)
			if err != nil {
				t.Fatal(err)
			}
			if added != 0 || updated != 0 || len(vault.created)+len(vault.updated) != 0 {
				t.Fatalf("a protected entry was imported: %d %d", added, updated)
			}
		})
	}
}

func TestReconcileHotSkipsAKnownFreeformBody(t *testing.T) {
	ctx := context.Background()
	r, vault, _ := newReconciler(t)
	hotPath := filepath.Join(t.TempDir(), "MEMORY.md")
	content := native.HotProfileStart + "\nA known body.\n" + native.HotProfileEnd
	if err := os.WriteFile(hotPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	corpus := []memory.Entry{{Title: "Known", Tier: memory.TierHot, Body: "A known body."}}
	added, updated, err := r.ReconcileHot(ctx, hotPath, corpus)
	if err != nil {
		t.Fatal(err)
	}
	if added != 0 || updated != 0 || len(vault.created) != 0 {
		t.Fatalf("a known body was imported: %d %d", added, updated)
	}
}

func TestReconcileHotSkipsAProjectionItAlreadyWrote(t *testing.T) {
	ctx := context.Background()
	r, vault, projections := newReconciler(t)
	if err := projections.SetLastProjection(time.Now().Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	hotPath := filepath.Join(t.TempDir(), "MEMORY.md")
	// The block is hand-edited, but the projection is newer than the file.
	content := native.HotProfileStart + "\nA hand edit.\n" + native.HotProfileEnd
	if err := os.WriteFile(hotPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	corpus := []memory.Entry{{Title: "A hot rule", Tier: memory.TierHot, Body: "The canonical body."}}
	added, updated, err := r.ReconcileHot(ctx, hotPath, corpus)
	if err != nil {
		t.Fatal(err)
	}
	if added != 0 || updated != 0 || len(vault.created) != 0 {
		t.Fatalf("an already projected file was imported: %d %d", added, updated)
	}
	data, err := os.ReadFile(hotPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "A hand edit.") || !strings.Contains(string(data), "The canonical body.") {
		t.Fatalf("the projection was not restored:\n%s", data)
	}
}

func TestReconcileHotReportsItsWriteAndReadErrors(t *testing.T) {
	ctx := context.Background()

	t.Run("read", func(t *testing.T) {
		r, _, _ := newReconciler(t)
		hotPath := filepath.Join(t.TempDir(), "a-folder")
		if err := os.MkdirAll(hotPath, 0o755); err != nil {
			t.Fatal(err)
		}
		if _, _, err := r.ReconcileHot(ctx, hotPath, nil); err == nil {
			t.Fatal("a folder was read as the hot file")
		}
	})

	t.Run("write", func(t *testing.T) {
		if os.Geteuid() == 0 {
			t.Skip("the permission case needs a non-root user")
		}
		r, _, _ := newReconciler(t)
		hotPath := filepath.Join(t.TempDir(), "MEMORY.md")
		if err := os.WriteFile(hotPath, []byte("plain text without a block"), 0o400); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(hotPath, 0o644) })
		if _, _, err := r.ReconcileHot(ctx, hotPath, nil); err == nil {
			t.Fatal("a read-only hot file was rewritten")
		}
	})
}

func TestReconcileHotReportsItsVaultErrors(t *testing.T) {
	ctx := context.Background()
	now := time.Now()

	t.Run("create", func(t *testing.T) {
		r, vault, _ := newReconciler(t)
		vault.createErr = errors.New("cannot create")
		hotPath := filepath.Join(t.TempDir(), "MEMORY.md")
		if err := os.WriteFile(hotPath, []byte(native.HotProfileStart+"\nA fact.\n"+native.HotProfileEnd), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, _, err := r.ReconcileHot(ctx, hotPath, nil); err == nil {
			t.Fatal("a failed create was not reported")
		}
	})

	t.Run("update", func(t *testing.T) {
		r, vault, _ := newReconciler(t)
		vault.updateErr = errors.New("cannot update")
		hotPath := filepath.Join(t.TempDir(), "MEMORY.md")
		content := native.HotProfileStart + "\n### A rule\n\nNew body.\n" + native.HotProfileEnd
		if err := os.WriteFile(hotPath, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		corpus := []memory.Entry{{Title: "A rule", Tier: memory.TierHot, Body: "Old body.",
			ModTime: now.Add(-2 * time.Hour)}}
		if _, _, err := r.ReconcileHot(ctx, hotPath, corpus); err == nil {
			t.Fatal("a failed update was not reported")
		}
	})

	t.Run("entries after an import", func(t *testing.T) {
		r, vault, _ := newReconciler(t)
		vault.entriesErr = errors.New("cannot read")
		hotPath := filepath.Join(t.TempDir(), "MEMORY.md")
		if err := os.WriteFile(hotPath, []byte(native.HotProfileStart+"\nA fact.\n"+native.HotProfileEnd), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, _, err := r.ReconcileHot(ctx, hotPath, nil); err == nil {
			t.Fatal("a failed read after the import was not reported")
		}
	})
}

func TestApplyHotBlockAndInterior(t *testing.T) {
	block := "### A rule\n\nThe body."
	wrapped := applyHotBlock("", block)
	if !strings.Contains(wrapped, "description: Shared memory") ||
		!strings.Contains(wrapped, native.HotProfileStart) ||
		!strings.Contains(wrapped, block) {
		t.Fatalf("the empty file is:\n%s", wrapped)
	}
	if got := hotInterior(wrapped); got != block {
		t.Fatalf("the interior is %q", got)
	}

	existing := "---\ndescription: mine\n---\n\nbefore\n" + native.HotProfileStart + "\nold\n" + native.HotProfileEnd + "\nafter\n"
	replaced := applyHotBlock(existing, block)
	if !strings.HasPrefix(replaced, "---\ndescription: mine\n---\n\nbefore\n") ||
		!strings.HasSuffix(replaced, "\nafter\n") ||
		!strings.Contains(replaced, block) ||
		strings.Contains(replaced, "\nold\n") {
		t.Fatalf("the splice is:\n%s", replaced)
	}

	appended := applyHotBlock("plain text", block)
	if !strings.HasPrefix(appended, "plain text") || !strings.Contains(appended, block) {
		t.Fatalf("the append is:\n%s", appended)
	}

	// A $ in the body must not be read as a backreference.
	dollar := applyHotBlock(existing, "costs $1 and $2")
	if !strings.Contains(dollar, "costs $1 and $2") {
		t.Fatalf("the dollar body is:\n%s", dollar)
	}

	if got := hotInterior("no block here"); got != "" {
		t.Fatalf("a text without a block has interior %q", got)
	}
	empty := applyHotBlock("", "")
	if !strings.Contains(empty, native.HotProfileStart) || !strings.Contains(empty, native.HotProfileEnd) {
		t.Fatalf("the empty block is:\n%s", empty)
	}
}

func TestRenderAgentAndUserFile(t *testing.T) {
	corpus := []memory.Entry{
		{Title: "A hot agent", Tier: memory.TierHot, Native: "minimax-agent", Body: "One."},
		{Title: "Another hot agent", Tier: memory.TierHot, Native: "minimax-agent", Body: "Two."},
		{Title: "A cold agent", Native: "minimax-agent", Body: "Hidden."},
	}
	agent := renderAgent(corpus)
	if !strings.Contains(agent, "### A hot agent") || !strings.Contains(agent, "### Another hot agent") {
		t.Fatalf("the agent render is:\n%s", agent)
	}
	if strings.Contains(agent, "Hidden") {
		t.Fatalf("a cold entry entered the agent render:\n%s", agent)
	}
	if strings.Contains(agent, "\n\n\n") {
		t.Fatalf("the agent entries are not separated by one blank line:\n%s", agent)
	}
	if got := renderAgent(nil); got != "" {
		t.Fatalf("an empty corpus renders %q", got)
	}
	if user := renderUserFile("### A hot user\n\nBody.", corpus); !strings.Contains(user, "### A hot user") {
		t.Fatalf("the user file is:\n%s", user)
	}
}

func TestOrphans(t *testing.T) {
	r, _, projections := newReconciler(t)
	if err := projections.Write(native.TargetMiniMaxAgent, "topics/known.md",
		native.RenderTopic(memory.Entry{Title: "Known", Body: "Body."})); err != nil {
		t.Fatal(err)
	}
	if err := projections.Write(native.TargetMiniMaxAgent, "topics/stale.md",
		native.RenderTopic(memory.Entry{Title: "Stale", Body: "Body."})); err != nil {
		t.Fatal(err)
	}
	if err := projections.Write(native.TargetMiniMaxAgent, "topics/garbage.md", "no frontmatter"); err != nil {
		t.Fatal(err)
	}
	corpus := []memory.Entry{{Title: "Known", Type: memory.TypeLesson, Body: "Body."}}
	orphans, err := r.Orphans(corpus)
	if err != nil {
		t.Fatal(err)
	}
	if len(orphans) != 2 || orphans[0] != "topics/garbage.md" || orphans[1] != "topics/stale.md" {
		t.Fatalf("the orphans are %v", orphans)
	}

	// The report is an error when the topics folder cannot be listed.
	topics := filepath.Join(projections.TargetDir(native.TargetMiniMaxAgent), "topics")
	if err := os.RemoveAll(topics); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(topics, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Orphans(corpus); err == nil {
		t.Fatal("a topics file was listed as a folder")
	}
}

func TestWriteHot(t *testing.T) {
	root := t.TempDir()
	if err := WriteHot(root, "content"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(root, "MEMORY.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "content" {
		t.Fatalf("the hot file is %q", data)
	}

	file := filepath.Join(t.TempDir(), "root-file")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := WriteHot(file, "content"); err == nil {
		t.Fatal("the hot file was written under a file")
	}
}

func TestImportCopiesTheUserReason(t *testing.T) {
	ctx := context.Background()
	r, vault, projections := newReconciler(t)
	writeProjection(t, projections, native.TargetMiniMaxUser, "user.md",
		native.RenderUser(memory.Entry{Title: "A preference", Body: "New body.", Reason: "because it is true"}),
		time.Now().Add(time.Minute))
	corpus := []memory.Entry{{
		Title: "A preference", Type: memory.TypeUser, Body: "Old body.",
		ModTime: time.Now().Add(-time.Hour),
	}}
	added, updated, err := r.Import(ctx, corpus)
	if err != nil {
		t.Fatal(err)
	}
	if added != 0 || updated != 1 || len(vault.updated) != 1 {
		t.Fatalf("the import is %d %d", added, updated)
	}
	if vault.updated[0].Reason != "because it is true" {
		t.Fatalf("the reason is %q", vault.updated[0].Reason)
	}
}

func TestImportWithANilWarningWriter(t *testing.T) {
	ctx := context.Background()
	r, vault, projections := newReconciler(t)
	r.Warn = nil
	writeProjection(t, projections, native.TargetMiniMaxAgent, "MEMORY.md",
		native.RenderAgent(memory.Entry{
			Title: "A leaked key", Body: "token ghp_" + strings.Repeat("a", 36),
		}), time.Now().Add(time.Minute))
	added, updated, err := r.Import(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if added != 0 || updated != 0 || len(vault.created) != 0 {
		t.Fatalf("a secret was imported: %d %d", added, updated)
	}
}

func TestCandidatesSkipWhatItCannotRead(t *testing.T) {
	_, vault, projections := newReconciler(t)
	if err := projections.Write(native.TargetMiniMaxAgent, "topics/garbage.md", "no frontmatter"); err != nil {
		t.Fatal(err)
	}
	if err := projections.Write(native.TargetMiniMaxAgent, "topics/a-topic.md",
		native.RenderTopic(memory.Entry{Title: "A topic", Body: "Body."})); err != nil {
		t.Fatal(err)
	}
	if err := projections.Write(native.TargetQwen, "garbage.md", "no frontmatter"); err != nil {
		t.Fatal(err)
	}
	if err := projections.Write(native.TargetQwen, "a-qwen.md",
		native.RenderQwen(memory.Entry{Title: "A qwen fact", Body: "Body."})); err != nil {
		t.Fatal(err)
	}
	if err := projections.Write(native.TargetQwen, "MEMORY.md", "- index\n"); err != nil {
		t.Fatal(err)
	}

	if os.Geteuid() != 0 {
		for _, path := range []string{
			filepath.Join(projections.TargetDir(native.TargetMiniMaxAgent), "topics", "locked.md"),
			filepath.Join(projections.TargetDir(native.TargetQwen), "locked.md"),
		} {
			if err := os.WriteFile(path, []byte("x"), 0o000); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = os.Chmod(path, 0o644) })
		}
	}

	r := &Reconciler{Vault: vault, Projections: projections}
	candidates := r.candidates()
	var titles []string
	for _, e := range candidates {
		titles = append(titles, e.Title)
	}
	if len(titles) != 2 {
		t.Fatalf("the candidates are %v", titles)
	}
}

func TestOrphansSkipsAnUnreadableTopic(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("the permission case needs a non-root user")
	}
	r, _, projections := newReconciler(t)
	locked := filepath.Join(projections.TargetDir(native.TargetMiniMaxAgent), "topics", "locked.md")
	if err := os.WriteFile(locked, []byte("---\ndescription: x\n---\n"), 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o644) })
	orphans, err := r.Orphans(nil)
	if err != nil {
		t.Fatalf("an unreadable topic failed the report: %v", err)
	}
	if len(orphans) != 0 {
		t.Fatalf("the orphans are %v", orphans)
	}
}

func TestExportReportsTheUserAndQwenWriteErrors(t *testing.T) {
	ctx := context.Background()

	t.Run("user store", func(t *testing.T) {
		r, _, projections := newReconciler(t)
		userDir := projections.TargetDir(native.TargetMiniMaxUser)
		if err := os.RemoveAll(userDir); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(userDir, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := r.Export(ctx, exportCorpus()); err == nil {
			t.Fatal("the export wrote under a user store file")
		}
	})

	t.Run("topics", func(t *testing.T) {
		r, _, projections := newReconciler(t)
		topics := filepath.Join(projections.TargetDir(native.TargetMiniMaxAgent), "topics")
		if err := os.RemoveAll(topics); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(topics, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		corpus := []memory.Entry{{Title: "A cold topic", Type: memory.TypeLesson, Native: "minimax-agent", Body: "Body."}}
		if _, err := r.Export(ctx, corpus); err == nil {
			t.Fatal("the export wrote a topic under a file")
		}
	})

	t.Run("qwen entry", func(t *testing.T) {
		r, _, projections := newReconciler(t)
		qwenDir := projections.TargetDir(native.TargetQwen)
		if err := os.RemoveAll(qwenDir); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(qwenDir, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		corpus := []memory.Entry{{Title: "A qwen fact", Type: memory.TypeLesson, Native: "qwen", Body: "Body."}}
		if _, err := r.Export(ctx, corpus); err == nil {
			t.Fatal("the export wrote a qwen entry under a file")
		}
	})

	t.Run("qwen index", func(t *testing.T) {
		r, _, projections := newReconciler(t)
		index := filepath.Join(projections.TargetDir(native.TargetQwen), "MEMORY.md")
		if err := os.MkdirAll(index, 0o755); err != nil {
			t.Fatal(err)
		}
		if _, err := r.Export(ctx, nil); err == nil {
			t.Fatal("the export wrote the qwen index over a folder")
		}
	})
}

func TestExportReportsAStaleFileItCannotRemove(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("the permission case needs a non-root user")
	}
	ctx := context.Background()

	t.Run("qwen root", func(t *testing.T) {
		r, _, projections := newReconciler(t)
		// The index already matches, so no write needs the read-only folder.
		if err := projections.Write(native.TargetQwen, "MEMORY.md", ""); err != nil {
			t.Fatal(err)
		}
		if err := projections.Write(native.TargetQwen, "stale.md", "stale"); err != nil {
			t.Fatal(err)
		}
		dir := projections.TargetDir(native.TargetQwen)
		if err := os.Chmod(dir, 0o500); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
		if _, err := r.Export(ctx, nil); err == nil {
			t.Fatal("a stale file in a read-only folder was removed")
		}
	})

	t.Run("qwen pinned", func(t *testing.T) {
		r, _, projections := newReconciler(t)
		pinned := filepath.Join(projections.TargetDir(native.TargetQwen), "pinned")
		if err := os.MkdirAll(pinned, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := projections.Write(native.TargetQwen, "pinned/stale.md", "stale"); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(pinned, 0o500); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(pinned, 0o755) })
		if _, err := r.Export(ctx, nil); err == nil {
			t.Fatal("a stale pinned file in a read-only folder was removed")
		}
	})
}

func TestUpsertHotUpdatesTheCorpus(t *testing.T) {
	ctx := context.Background()
	r, vault, _ := newReconciler(t)
	hotPath := filepath.Join(t.TempDir(), "MEMORY.md")
	content := native.HotProfileStart + "\n<!-- mem-append-reason: why -->\n### A rule\n\nNew body.\n" + native.HotProfileEnd
	if err := os.WriteFile(hotPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	corpus := []memory.Entry{
		{Title: "An unrelated rule", Tier: memory.TierHot, Body: "Other."},
		{Title: "A rule", Body: "Old body.",
			ModTime: time.Now().Add(-time.Hour)},
	}
	added, updated, err := r.ReconcileHot(ctx, hotPath, corpus)
	if err != nil {
		t.Fatal(err)
	}
	if added != 0 || updated != 1 || len(vault.updated) != 1 {
		t.Fatalf("the import is %d %d %+v", added, updated, vault.updated)
	}
	got := vault.updated[0]
	if got.Body != "New body." || got.Reason != "why" || got.Tier != memory.TierHot {
		t.Fatalf("the update is %+v", got)
	}
}

func TestReconcileHotSkipsASecretHeading(t *testing.T) {
	ctx := context.Background()
	r, vault, _ := newReconciler(t)
	hotPath := filepath.Join(t.TempDir(), "MEMORY.md")
	content := native.HotProfileStart + "\n### A leaked key\n\ntoken ghp_" + strings.Repeat("a", 36) +
		"\n" + native.HotProfileEnd
	if err := os.WriteFile(hotPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	added, updated, err := r.ReconcileHot(ctx, hotPath, nil)
	if err != nil {
		t.Fatal(err)
	}
	if added != 0 || updated != 0 || len(vault.created) != 0 {
		t.Fatalf("a secret heading was imported: %d %d", added, updated)
	}
	if warnings := r.Warn.(*bytes.Buffer).String(); !strings.Contains(warnings, "skipped native hot entry") {
		t.Fatalf("the warning is %q", warnings)
	}
}

func TestImportHotReportsAReadFailureBetweenEntries(t *testing.T) {
	ctx := context.Background()
	r, vault, _ := newReconciler(t)
	vault.entriesErr = errors.New("cannot read")
	hotPath := filepath.Join(t.TempDir(), "MEMORY.md")
	content := native.HotProfileStart + "\n### A rule\n\nNew body.\n" + native.HotProfileEnd
	if err := os.WriteFile(hotPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := r.ReconcileHot(ctx, hotPath, nil); err == nil {
		t.Fatal("a failed read between entries was not reported")
	}
}
