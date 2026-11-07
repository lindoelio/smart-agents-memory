package app_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lindoelio/smart-agents-memory/internal/app"
	"github.com/lindoelio/smart-agents-memory/internal/memory"
	"github.com/lindoelio/smart-agents-memory/internal/native"
	"github.com/lindoelio/smart-agents-memory/internal/vault"

	_ "github.com/lindoelio/smart-agents-memory/internal/vault/disk"
	_ "github.com/lindoelio/smart-agents-memory/internal/vault/obsidian"
	_ "github.com/lindoelio/smart-agents-memory/internal/vault/sqlite"
)

// harness is an application over a temporary store.
type harness struct {
	app  *app.App
	out  *bytes.Buffer
	err  *bytes.Buffer
	root string
	home string
}

// newHarness builds an application over a temporary store and a temporary
// home, so the links never touch the real machine.
func newHarness(t *testing.T, backend string) *harness {
	t.Helper()
	root := t.TempDir()
	home := t.TempDir()
	t.Setenv("HOME", home)

	cfg := vault.Config{Backend: backend, MemoryHome: root}
	if backend == "sqlite" {
		cfg.SQLitePath = filepath.Join(root, "memory.db")
	}
	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}
	application, err := app.New(cfg, filepath.Join(home, "config.json"), out, errOut)
	if err != nil {
		t.Fatalf("cannot build the application: %v", err)
	}
	return &harness{app: application, out: out, err: errOut, root: root, home: home}
}

func TestAddRecordsAndProjects(t *testing.T) {
	h := newHarness(t, "obsidian")
	ctx := context.Background()

	message, err := h.app.Add(ctx, app.AddOptions{
		Scope: "lesson",
		Title: "A durable lesson",
		Body:  "The rule. Why it matters.",
	})
	if err != nil {
		t.Fatalf("cannot add: %v", err)
	}
	if !strings.HasPrefix(message, "recorded:") {
		t.Fatalf("the message is %q", message)
	}

	entries, err := h.app.Vault.Entries(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("the corpus has %d entries, want 1", len(entries))
	}
	if entries[0].Type != memory.TypeLesson {
		t.Fatalf("the type is %q", entries[0].Type)
	}
	if entries[0].Native != "minimax-agent" {
		t.Fatalf("the native target is %q", entries[0].Native)
	}

	// The cold entry must be projected as a topic file.
	topic := filepath.Join(h.root, "native", "minimax-agent", "topics", "a-durable-lesson.md")
	if _, err := os.Stat(topic); err != nil {
		t.Fatalf("the topic file was not written: %v", err)
	}
}

func TestAddAppendsToAnExistingEntry(t *testing.T) {
	h := newHarness(t, "obsidian")
	ctx := context.Background()

	if _, err := h.app.Add(ctx, app.AddOptions{
		Scope: "lesson", Title: "A lesson", Body: "The first part.",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := h.app.Add(ctx, app.AddOptions{
		Scope: "lesson", Title: "A lesson", Body: "The second part.",
	}); err != nil {
		t.Fatal(err)
	}
	entries, err := h.app.Vault.Entries(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("the corpus has %d entries, want 1", len(entries))
	}
	if !strings.Contains(entries[0].Body, "The first part.") ||
		!strings.Contains(entries[0].Body, "The second part.") {
		t.Fatalf("the body is %q", entries[0].Body)
	}
}

func TestAddDoesNotDuplicateTheSameBody(t *testing.T) {
	h := newHarness(t, "obsidian")
	ctx := context.Background()
	for range 2 {
		message, err := h.app.Add(ctx, app.AddOptions{
			Scope: "lesson", Title: "A lesson", Body: "The same body.",
		})
		if err != nil {
			t.Fatal(err)
		}
		if strings.HasPrefix(message, "already recorded") {
			return
		}
	}
	t.Fatal("the second write was not recognized")
}

func TestTheEnglishWarningDoesNotBlock(t *testing.T) {
	h := newHarness(t, "obsidian")
	ctx := context.Background()

	if _, err := h.app.Add(ctx, app.AddOptions{
		Scope: "lesson",
		Title: "Uma regra",
		Body:  "A regra e que voce sempre escreve o porque, porque uma regra sem motivo nao serve para nada aqui.",
	}); err != nil {
		t.Fatalf("the write was blocked: %v", err)
	}
	if !strings.Contains(h.err.String(), "warning") {
		t.Fatalf("there is no warning: %q", h.err.String())
	}

	h.err.Reset()
	if _, err := h.app.Add(ctx, app.AddOptions{
		Scope:              "lesson",
		Title:              "Another rule",
		Body:               "A regra e que voce sempre escreve o porque, porque uma regra sem motivo nao serve para nada aqui.",
		AllowOtherLanguage: true,
	}); err != nil {
		t.Fatal(err)
	}
	if h.err.Len() != 0 {
		t.Fatalf("the warning was not silenced: %q", h.err.String())
	}
}

func TestIndexIsIdempotent(t *testing.T) {
	h := newHarness(t, "obsidian")
	ctx := context.Background()

	if _, err := h.app.Add(ctx, app.AddOptions{
		Scope: "lesson", Title: "A durable lesson", Body: "The rule. Why it matters.",
	}); err != nil {
		t.Fatal(err)
	}
	first, err := h.app.Index(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if first.Entries != 1 {
		t.Fatalf("the corpus has %d entries, want 1", first.Entries)
	}
	second, err := h.app.Index(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if second.Imported != 0 || second.Updated != 0 || second.Exported != 0 {
		t.Fatalf("the second run had work to do: %+v", second)
	}
}

func TestIndexPublishesTheIndex(t *testing.T) {
	h := newHarness(t, "obsidian")
	ctx := context.Background()
	if _, err := h.app.Add(ctx, app.AddOptions{
		Scope: "lesson", Title: "A durable lesson", Body: "The rule. Why it matters.",
	}); err != nil {
		t.Fatal(err)
	}
	report, err := h.app.Index(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if report.IndexPath == "" {
		t.Fatal("the index was not published")
	}
	data, err := os.ReadFile(filepath.Join(h.root, report.IndexPath))
	if err != nil {
		t.Fatalf("the index was not written: %v", err)
	}
	if !strings.Contains(string(data), "A durable lesson") {
		t.Fatalf("the index does not carry the entry:\n%s", data)
	}
	if !strings.Contains(string(data), "## lesson") {
		t.Fatalf("the index does not group by type:\n%s", data)
	}
}

func TestTheHotTierIsProjectedIntoTheUserStore(t *testing.T) {
	h := newHarness(t, "obsidian")
	ctx := context.Background()

	if err := os.MkdirAll(h.root, 0o755); err != nil {
		t.Fatal(err)
	}
	hot := "---\ndescription: Hot tier\ntype: index\nalwaysApply: true\n---\n\n" +
		native.HotProfileStart + "\n## Profile\n\nThe user.\n" + native.HotProfileEnd + "\n"
	if err := os.WriteFile(filepath.Join(h.root, "MEMORY.md"), []byte(hot), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := h.app.Add(ctx, app.AddOptions{
		Scope: "user", Title: "Identity", Body: "The person.", Tier: "hot",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := h.app.Index(ctx); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(h.root, "native", "minimax-user", "user.md"))
	if err != nil {
		t.Fatalf("the user store was not written: %v", err)
	}
	if !strings.Contains(string(data), "### Identity") {
		t.Fatalf("the hot entry is not in the user store:\n%s", data)
	}
	if !strings.Contains(string(data), native.HotProfileStart) {
		t.Fatalf("the hot tier block is not in the user store:\n%s", data)
	}
}

func TestMigrateReportsAndApplies(t *testing.T) {
	h := newHarness(t, "obsidian")
	ctx := context.Background()

	// A legacy entry: the old vocabulary, the old source, the old markers.
	legacy := "---\ntitle: \"Uma licao\"\ntype: licao\ndate: 2026-11-07\nsource: \"memoria-cli\"\n---\n\n## Uma licao\n\nThe body.\n"
	legacyPath := filepath.Join(h.root, "corpus", "licao", "uma-licao.md")
	if err := os.MkdirAll(filepath.Dir(legacyPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(legacyPath, []byte(legacy), 0o644); err != nil {
		t.Fatal(err)
	}
	hot := "---\ndescription: Hot\ntype: index\n---\n\n" +
		"<!-- memoria:hot:start -->\n## Profile\n\nThe user.\n<!-- memoria:hot:end -->\n"
	if err := os.WriteFile(filepath.Join(h.root, "MEMORY.md"), []byte(hot), 0o644); err != nil {
		t.Fatal(err)
	}

	changes, err := h.app.Migrate(ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) == 0 {
		t.Fatal("the migration found nothing")
	}
	// Without --apply nothing changes.
	data, err := os.ReadFile(legacyPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "type: licao") {
		t.Fatal("the dry run changed the entry")
	}

	changes, err = h.app.Migrate(ctx, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) == 0 {
		t.Fatal("the migration found nothing to apply")
	}
	data, err = os.ReadFile(legacyPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "type: lesson") {
		t.Fatalf("the type was not migrated:\n%s", data)
	}
	if !strings.Contains(string(data), "smart-agents-memory-cli") {
		t.Fatalf("the source was not migrated:\n%s", data)
	}
	hotData, err := os.ReadFile(filepath.Join(h.root, "MEMORY.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(hotData), native.HotProfileStart) {
		t.Fatalf("the markers were not migrated:\n%s", hotData)
	}

	// The migration is idempotent.
	changes, err = h.app.Migrate(ctx, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 0 {
		t.Fatalf("the second run found %d changes", len(changes))
	}
}

func TestInstallCreatesTheLinks(t *testing.T) {
	h := newHarness(t, "obsidian")
	ctx := context.Background()

	result, err := h.app.Install(ctx)
	if err != nil {
		t.Fatalf("cannot install: %v", err)
	}
	if len(result.Lines) < 19 {
		t.Fatalf("the install reported %d lines, want the 18 links and the skill", len(result.Lines))
	}
	targets := h.app.LinkTargets()
	if len(targets) != 18 {
		t.Fatalf("there are %d link targets, want 18", len(targets))
	}
	for _, target := range targets {
		info, err := os.Lstat(target.Path)
		if err != nil {
			t.Fatalf("the link %s was not created: %v", target.Path, err)
		}
		if info.Mode()&os.ModeSymlink == 0 {
			t.Fatalf("%s is not a symlink", target.Path)
		}
	}

	// The install is idempotent.
	second, err := h.app.Install(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range second.Lines {
		if strings.HasPrefix(line, "skill:") {
			continue
		}
		if !strings.Contains(line, "already linked") {
			t.Fatalf("the second install changed something: %s", line)
		}
	}
}

func TestInstallRepairsALinkAFileOverwrote(t *testing.T) {
	h := newHarness(t, "obsidian")
	ctx := context.Background()

	target := filepath.Join(h.home, ".codex", "AGENTS.md")
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("written by a tool"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := h.app.Install(ctx); err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(target)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Fatal("the link was not recreated")
	}
	backup := target + ".bak-smart-agents-memory"
	data, err := os.ReadFile(backup)
	if err != nil {
		t.Fatalf("the overwritten file was not preserved: %v", err)
	}
	if string(data) != "written by a tool" {
		t.Fatalf("the preserved file is %q", data)
	}
}

func TestDoctorFindsAHealthyInstallation(t *testing.T) {
	h := newHarness(t, "obsidian")
	ctx := context.Background()

	if _, err := h.app.Add(ctx, app.AddOptions{
		Scope: "lesson", Title: "A durable lesson", Body: "The rule. Why it matters.",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := h.app.Index(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := h.app.Install(ctx); err != nil {
		t.Fatal(err)
	}
	report, err := h.app.Doctor(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !report.OK() {
		t.Fatalf("the doctor found problems: %v", report.Problems)
	}
}

func TestDoctorFindsABrokenLink(t *testing.T) {
	h := newHarness(t, "obsidian")
	ctx := context.Background()

	if _, err := h.app.Install(ctx); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(h.home, ".codex", "AGENTS.md")
	if err := os.Remove(target); err != nil {
		t.Fatal(err)
	}
	report, err := h.app.Doctor(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if report.OK() {
		t.Fatal("the doctor did not find the broken link")
	}
}

func TestBriefCarriesTheHotTier(t *testing.T) {
	h := newHarness(t, "obsidian")
	ctx := context.Background()

	hot := "---\ndescription: Hot tier\ntype: index\n---\n\n## Profile\n\nThe user writes English memory.\n"
	if err := os.WriteFile(filepath.Join(h.root, "MEMORY.md"), []byte(hot), 0o644); err != nil {
		t.Fatal(err)
	}
	text, err := h.app.Brief(ctx, "", "", 20)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, "The user writes English memory.") {
		t.Fatalf("the brief does not carry the hot tier: %q", text)
	}
	if strings.Contains(text, "description: Hot tier") {
		t.Fatalf("the brief carries the frontmatter: %q", text)
	}
}

func TestBriefCarriesTheProjectNotes(t *testing.T) {
	h := newHarness(t, "obsidian")
	ctx := context.Background()

	dir := filepath.Join(h.root, "projects")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "demo.md"), []byte("# Demo\n\nThe project notes.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	text, err := h.app.Brief(ctx, "demo", "", 20)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, "The project notes.") {
		t.Fatalf("the brief does not carry the project notes: %q", text)
	}
}

func TestMigrateRenamesTheEntriesAndRemovesTheDuplicates(t *testing.T) {
	h := newHarness(t, "obsidian")
	ctx := context.Background()

	// A legacy entry whose title stayed in the previous vocabulary.
	legacy := "---\ntitle: \"Reconciliação do corpus: mtime decide quem manda\"\ntype: licao\ndate: 2026-11-07\nsource: \"minimax-agent-memory\"\n---\n\n## Reconciliação do corpus\n\nThe mtime decides.\n"
	legacyPath := filepath.Join(h.root, "corpus", "lesson", "reconciliação-do-corpus-mtime-decide-quem-manda.md")
	if err := os.MkdirAll(filepath.Dir(legacyPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(legacyPath, []byte(legacy), 0o644); err != nil {
		t.Fatal(err)
	}

	// A duplicate of an entry that already exists.
	duplicate := "---\ntitle: \"Nome\"\ntype: usuario\ndate: 2026-11-07\n---\n\n## Nome\n\n- Keep the name.\n"
	duplicatePath := filepath.Join(h.root, "corpus", "user", "nome.md")
	if err := os.MkdirAll(filepath.Dir(duplicatePath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(duplicatePath, []byte(duplicate), 0o644); err != nil {
		t.Fatal(err)
	}
	original := "---\ntitle: \"Name\"\ntype: usuario\ndate: 2026-11-07\ntier: hot\n---\n\n## Name\n\n- Keep the name.\n"
	if err := os.WriteFile(filepath.Join(h.root, "corpus", "user", "name.md"), []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := h.app.Migrate(ctx, true); err != nil {
		t.Fatal(err)
	}

	// The renamed entry keeps its accents in the address.
	renamed := filepath.Join(h.root, "corpus", "lesson", "corpus-reconciliation-mtime-decides-the-winner.md")
	data, err := os.ReadFile(renamed)
	if err != nil {
		t.Fatalf("the entry was not renamed: %v", err)
	}
	if !strings.Contains(string(data), "Corpus reconciliation: mtime decides the winner") {
		t.Fatalf("the title was not migrated:\n%s", data)
	}
	if !strings.Contains(string(data), "type: lesson") {
		t.Fatalf("the type was not migrated:\n%s", data)
	}
	if _, err := os.Stat(legacyPath); !os.IsNotExist(err) {
		t.Fatal("the old file was not removed")
	}

	// The duplicate is gone.
	if _, err := os.Stat(duplicatePath); !os.IsNotExist(err) {
		t.Fatal("the duplicate was not removed")
	}

	// The projection of the duplicate is an orphan, and is removed.
	orphan := filepath.Join(h.root, "native", "minimax-agent", "topics", "nome.md")
	if err := os.MkdirAll(filepath.Dir(orphan), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(orphan, []byte("---\ndescription: nome\n---\n\n<!-- smart-agents-memory:title:Nome -->\nBody.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	changes, err := h.app.Migrate(ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, change := range changes {
		if strings.Contains(change.Path, "nome.md") {
			found = true
		}
	}
	if !found {
		t.Fatalf("the orphan projection was not reported: %+v", changes)
	}
	if _, err := h.app.Migrate(ctx, true); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(orphan); !os.IsNotExist(err) {
		t.Fatal("the orphan projection was not removed")
	}
}
