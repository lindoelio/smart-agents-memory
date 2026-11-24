package app_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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

	cfg := vault.Config{Backend: backend, MemoryHome: root, ProjectionRoot: root}
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

func TestAddPinnedLandsInTheQwenProtectedDirectory(t *testing.T) {
	h := newHarness(t, "obsidian")
	ctx := context.Background()

	if _, err := h.app.Add(ctx, app.AddOptions{
		Scope: "lesson", Title: "A pinned rule", Body: "The rule. Why it matters.",
		Native: "qwen", Description: "A curated rule", Pinned: true,
	}); err != nil {
		t.Fatal(err)
	}

	pinned := filepath.Join(h.root, "native", "qwen", "pinned", "a-pinned-rule.md")
	if _, err := os.Stat(pinned); err != nil {
		t.Fatalf("the pinned projection was not written: %v", err)
	}
	if _, err := os.Stat(filepath.Join(h.root, "native", "qwen", "a-pinned-rule.md")); !os.IsNotExist(err) {
		t.Fatal("the pinned entry also landed outside pinned/")
	}
	index, err := os.ReadFile(filepath.Join(h.root, "native", "qwen", "MEMORY.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(index), "[A pinned rule](pinned/a-pinned-rule.md)") {
		t.Fatalf("the Qwen index does not point at pinned/:\n%s", index)
	}
}

func TestANewerPinnedNativeEditCannotOverwriteTheCorpus(t *testing.T) {
	h := newHarness(t, "obsidian")
	ctx := context.Background()

	if _, err := h.app.Add(ctx, app.AddOptions{
		Scope: "lesson", Title: "A pinned rule", Body: "The rule. Why it matters.",
		Native: "qwen", Pinned: true,
	}); err != nil {
		t.Fatal(err)
	}
	pinned := filepath.Join(h.root, "native", "qwen", "pinned", "a-pinned-rule.md")
	data, err := os.ReadFile(pinned)
	if err != nil {
		t.Fatal(err)
	}
	edited := strings.Replace(string(data), "The rule. Why it matters.", "A native edit that must not win.", 1)
	if err := os.WriteFile(pinned, []byte(edited), 0o644); err != nil {
		t.Fatal(err)
	}
	future := time.Now().Add(2 * time.Second)
	if err := os.Chtimes(pinned, future, future); err != nil {
		t.Fatal(err)
	}

	if _, err := h.app.Index(ctx); err != nil {
		t.Fatal(err)
	}
	entries, err := h.app.Vault.Entries(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || !strings.Contains(entries[0].Body, "The rule. Why it matters.") {
		t.Fatalf("the pinned corpus entry changed: %+v", entries)
	}
	if !entries[0].Pinned {
		t.Fatal("the entry lost its pinned state")
	}
	data, err = os.ReadFile(pinned)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "A native edit") {
		t.Fatalf("the projection was not restored:\n%s", data)
	}
}

func TestANewerHotEditCannotOverwriteAPinnedEntry(t *testing.T) {
	h := newHarness(t, "obsidian")
	ctx := context.Background()

	if _, err := h.app.Add(ctx, app.AddOptions{
		Scope: "user", Title: "A pinned identity", Body: "The person.",
		Tier: "hot", Pinned: true,
	}); err != nil {
		t.Fatal(err)
	}
	hotPath := filepath.Join(h.root, "MEMORY.md")
	data, err := os.ReadFile(hotPath)
	if err != nil {
		t.Fatal(err)
	}
	edited := strings.Replace(string(data), "The person.", "An edit that must not win.", 1)
	if err := os.WriteFile(hotPath, []byte(edited), 0o644); err != nil {
		t.Fatal(err)
	}
	future := time.Now().Add(2 * time.Second)
	if err := os.Chtimes(hotPath, future, future); err != nil {
		t.Fatal(err)
	}

	if _, err := h.app.Index(ctx); err != nil {
		t.Fatal(err)
	}
	entries, err := h.app.Vault.Entries(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || !strings.Contains(entries[0].Body, "The person.") {
		t.Fatalf("the pinned hot entry changed: %+v", entries)
	}
	data, err = os.ReadFile(hotPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "An edit that must not win.") {
		t.Fatalf("the hot block was not restored:\n%s", data)
	}
}

func TestAddRefusesToModifyAPinnedEntryWithoutForce(t *testing.T) {
	h := newHarness(t, "obsidian")
	ctx := context.Background()

	if _, err := h.app.Add(ctx, app.AddOptions{
		Scope: "lesson", Title: "A pinned rule", Body: "The rule.",
		Pinned: true,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := h.app.Add(ctx, app.AddOptions{
		Scope: "lesson", Title: "A pinned rule", Body: "Another body.",
	}); err == nil || !strings.Contains(err.Error(), "pinned") {
		t.Fatalf("the pinned entry was modified without force: %v", err)
	}

	if _, err := h.app.Add(ctx, app.AddOptions{
		Scope: "lesson", Title: "A pinned rule", Body: "Another body.",
		Force: true,
	}); err != nil {
		t.Fatalf("cannot force an append: %v", err)
	}
	entries, err := h.app.Vault.Entries(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || !entries[0].Pinned {
		t.Fatalf("the forced append lost the pin: %+v", entries)
	}
	if !strings.Contains(entries[0].Body, "The rule.") ||
		!strings.Contains(entries[0].Body, "Another body.") {
		t.Fatalf("the forced append did not reach the entry: %q", entries[0].Body)
	}
	lines, err := h.app.Log(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(lines, "\n"), "forced on pinned") {
		t.Fatalf("the log does not record the forced write: %v", lines)
	}
}

func TestAddPinnedPinsAnExistingEntry(t *testing.T) {
	h := newHarness(t, "obsidian")
	ctx := context.Background()

	if _, err := h.app.Add(ctx, app.AddOptions{
		Scope: "lesson", Title: "A rule", Body: "The rule.",
	}); err != nil {
		t.Fatal(err)
	}
	message, err := h.app.Add(ctx, app.AddOptions{
		Scope: "lesson", Title: "A rule", Body: "The rule.", Pinned: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(message, "pinned:") {
		t.Fatalf("the message is %q", message)
	}
	entries, err := h.app.Vault.Entries(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || !entries[0].Pinned {
		t.Fatalf("the existing entry was not pinned: %+v", entries)
	}
	if strings.Count(entries[0].Body, "The rule.") != 1 {
		t.Fatalf("the body was duplicated: %q", entries[0].Body)
	}
}

func TestPinAndUnpinMoveTheQwenProjection(t *testing.T) {
	h := newHarness(t, "obsidian")
	ctx := context.Background()

	if _, err := h.app.Add(ctx, app.AddOptions{
		Scope: "lesson", Title: "A movable rule", Body: "The rule.",
		Native: "qwen",
	}); err != nil {
		t.Fatal(err)
	}
	rootFile := filepath.Join(h.root, "native", "qwen", "a-movable-rule.md")
	pinnedFile := filepath.Join(h.root, "native", "qwen", "pinned", "a-movable-rule.md")
	if _, err := os.Stat(rootFile); err != nil {
		t.Fatalf("the root projection was not written: %v", err)
	}

	message, err := h.app.SetPinned(ctx, "A movable rule", true)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(message, "pinned:") {
		t.Fatalf("the message is %q", message)
	}
	if _, err := os.Stat(rootFile); !os.IsNotExist(err) {
		t.Fatal("pin left the stale root projection")
	}
	if _, err := os.Stat(pinnedFile); err != nil {
		t.Fatalf("pin did not write the protected projection: %v", err)
	}
	if message, err := h.app.SetPinned(ctx, "A movable rule", true); err != nil || !strings.HasPrefix(message, "already pinned:") {
		t.Fatalf("pin is not idempotent: %q %v", message, err)
	}

	message, err = h.app.SetPinned(ctx, "A movable rule", false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(message, "unpinned:") {
		t.Fatalf("the message is %q", message)
	}
	if _, err := os.Stat(pinnedFile); !os.IsNotExist(err) {
		t.Fatal("unpin left the stale protected projection")
	}
	if _, err := os.Stat(rootFile); err != nil {
		t.Fatalf("unpin did not restore the root projection: %v", err)
	}
	lines, err := h.app.Log(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	log := strings.Join(lines, "\n")
	if !strings.Contains(log, "pin | lesson | A movable rule") ||
		!strings.Contains(log, "unpin | lesson | A movable rule") {
		t.Fatalf("the write log is missing the transitions: %s", log)
	}
}

func TestSetPinnedUpdatesEveryGateway(t *testing.T) {
	for _, backend := range []string{"obsidian", "disk", "sqlite"} {
		t.Run(backend, func(t *testing.T) {
			h := newHarness(t, backend)
			ctx := context.Background()
			if layout, ok := h.app.Vault.(vault.Layout); ok {
				if err := layout.Ensure(ctx); err != nil {
					t.Fatal(err)
				}
			}

			if _, err := h.app.Add(ctx, app.AddOptions{
				Scope: "lesson", Title: "A rule", Body: "The rule.",
			}); err != nil {
				t.Fatal(err)
			}
			if _, err := h.app.SetPinned(ctx, "A rule", true); err != nil {
				t.Fatal(err)
			}
			entries, err := h.app.Vault.Entries(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if len(entries) != 1 || !entries[0].Pinned {
				t.Fatalf("%s: the pin was not stored: %+v", backend, entries)
			}
			if _, err := h.app.SetPinned(ctx, "A rule", false); err != nil {
				t.Fatal(err)
			}
			entries, err = h.app.Vault.Entries(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if len(entries) != 1 || entries[0].Pinned {
				t.Fatalf("%s: the unpin was not stored: %+v", backend, entries)
			}
		})
	}
}

func TestAPinnedNativeFileBecomesAPinnedCorpusEntry(t *testing.T) {
	h := newHarness(t, "obsidian")
	ctx := context.Background()

	pinned := filepath.Join(h.root, "native", "qwen", "pinned", "manual.md")
	if err := os.MkdirAll(filepath.Dir(pinned), 0o755); err != nil {
		t.Fatal(err)
	}
	content := "---\nname: A manual pinned rule\ndescription: Curated by hand.\ntype: reference\ncategory: shared_memory\n---\n\nThe body.\n"
	if err := os.WriteFile(pinned, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := h.app.Index(ctx); err != nil {
		t.Fatal(err)
	}
	entries, err := h.app.Vault.Entries(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Title != "A manual pinned rule" || !entries[0].Pinned {
		t.Fatalf("the pinned projection was not imported as pinned: %+v", entries)
	}
}

func TestUnpinRefusesAPathPinnedEntry(t *testing.T) {
	h := newHarness(t, "obsidian")
	ctx := context.Background()

	path := filepath.Join(h.root, "corpus", "pinned", "a-curated-rule.md")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	note := "---\ntitle: \"A curated rule\"\ntype: lesson\ndate: 2026-11-07\n---\n\n## A curated rule\n\nBody.\n"
	if err := os.WriteFile(path, []byte(note), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := h.app.SetPinned(ctx, "A curated rule", false); err == nil || !strings.Contains(err.Error(), "pinned by its path") {
		t.Fatalf("the path-pinned entry was unpinned: %v", err)
	}
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
	if len(result.Lines) < 18 {
		t.Fatalf("the install reported %d lines, want the 17 links and the skill", len(result.Lines))
	}
	targets := h.app.LinkTargets()
	if len(targets) != 17 {
		t.Fatalf("there are %d link targets, want 17", len(targets))
	}
	for _, target := range targets {
		if strings.Contains(target.Path, "memories") && strings.Contains(target.Path, ".minimax") {
			t.Fatalf("the empty minimax store is still linked: %s", target.Path)
		}
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

func TestAddTeamWritesTheSharedStore(t *testing.T) {
	h := newHarness(t, "obsidian")
	ctx := context.Background()
	repo := newRepo(t)
	slug := memory.Slugify("repo")

	message, err := h.app.Add(ctx, app.AddOptions{
		Scope: "lesson", Title: "A shared rule", Body: "The rule. Why it matters.",
		Native: "qwen", Team: true, CWD: repo,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(message, "recorded (team):") {
		t.Fatalf("the message is %q", message)
	}
	primary := filepath.Join(repo, ".agents", "memory", "corpus", "lesson", "a-shared-rule.md")
	data, err := os.ReadFile(primary)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "project: "+slug) {
		t.Fatalf("the team note has no project:\n%s", data)
	}
	if _, err := os.Stat(filepath.Join(repo, ".qwen", "team-memory", "lesson", "a-shared-rule.md")); err != nil {
		t.Fatalf("the Qwen projection was not written: %v", err)
	}
	entries, err := h.app.Vault.Entries(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("the team write entered the private vault: %+v", entries)
	}
}

func TestAddTeamRejectsUserScope(t *testing.T) {
	h := newHarness(t, "obsidian")
	_, err := h.app.Add(context.Background(), app.AddOptions{
		Scope: "user", Title: "A person", Body: "The person.", Team: true, CWD: newRepo(t),
	})
	if err == nil || !strings.Contains(err.Error(), "personal user profile") {
		t.Fatalf("the error is %v", err)
	}
}

func TestAddTeamRequiresAGitRepository(t *testing.T) {
	h := newHarness(t, "obsidian")
	_, err := h.app.Add(context.Background(), app.AddOptions{
		Scope: "lesson", Title: "A rule", Body: "The rule.", Team: true, CWD: t.TempDir(),
	})
	if err == nil || !strings.Contains(err.Error(), "git repository") {
		t.Fatalf("the error is %v", err)
	}
}

func TestAddRejectsASecret(t *testing.T) {
	h := newHarness(t, "obsidian")
	_, err := h.app.Add(context.Background(), app.AddOptions{
		Scope: "lesson", Title: "A leaked key", Body: "ghp_" + strings.Repeat("a", 36),
	})
	if err == nil || !strings.Contains(err.Error(), "GitHub PAT") {
		t.Fatalf("the secret was accepted: %v", err)
	}
	entries, err := h.app.Vault.Entries(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("the secret entered the corpus: %+v", entries)
	}
}

func TestBriefCarriesTheTeamMemory(t *testing.T) {
	h := newHarness(t, "obsidian")
	ctx := context.Background()
	repo := newRepo(t)
	if _, err := h.app.Add(ctx, app.AddOptions{
		Scope: "lesson", Title: "A shared rule", Body: "The shared body.",
		Team: true, CWD: repo,
	}); err != nil {
		t.Fatal(err)
	}
	text, err := h.app.Brief(ctx, app.BriefOptions{CWD: repo})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, "## Team Memory ("+memory.Slugify("repo")+")") ||
		!strings.Contains(text, "The shared body.") {
		t.Fatalf("the brief does not carry team memory:\n%s", text)
	}
}

func TestSearchFindsTheTeamEntry(t *testing.T) {
	h := newHarness(t, "obsidian")
	ctx := context.Background()
	repo := newRepo(t)
	if _, err := h.app.Add(ctx, app.AddOptions{
		Scope: "lesson", Title: "A shared rule", Body: "The shared body.",
		Team: true, CWD: repo,
	}); err != nil {
		t.Fatal(err)
	}
	t.Chdir(repo)
	hits, err := h.app.Search(ctx, "shared", 10)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, hit := range hits {
		if strings.HasPrefix(hit.Path, "[team] ") && strings.Contains(hit.Text, "shared") {
			found = true
		}
	}
	if !found {
		t.Fatalf("the team hit is missing: %+v", hits)
	}
}

func TestTheNativeImportSkipsASecret(t *testing.T) {
	h := newHarness(t, "obsidian")
	ctx := context.Background()
	topic := native.RenderTopic(memory.Entry{
		Title: "A leaked native lesson", Type: memory.TypeLesson,
		Body: "ghp_" + strings.Repeat("a", 36), Description: "A leaked lesson",
	})
	if err := h.app.Projections.Write(native.TargetMiniMaxAgent, "topics/leaked.md", topic); err != nil {
		t.Fatal(err)
	}
	future := time.Now().Add(2 * time.Second)
	path := filepath.Join(h.root, "native", "minimax-agent", "topics", "leaked.md")
	if err := os.Chtimes(path, future, future); err != nil {
		t.Fatal(err)
	}
	if _, err := h.app.Index(ctx); err != nil {
		t.Fatal(err)
	}
	entries, err := h.app.Vault.Entries(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("the secret was imported: %+v", entries)
	}
	if !strings.Contains(h.err.String(), "skipped native entry") {
		t.Fatalf("the import was not reported: %q", h.err.String())
	}
}

func TestAHotNativeEditWithASecretIsSkipped(t *testing.T) {
	h := newHarness(t, "obsidian")
	ctx := context.Background()
	if _, err := h.app.Add(ctx, app.AddOptions{
		Scope: "user", Title: "Identity", Body: "The person.", Tier: "hot",
	}); err != nil {
		t.Fatal(err)
	}
	hotPath := filepath.Join(h.root, "MEMORY.md")
	data, err := os.ReadFile(hotPath)
	if err != nil {
		t.Fatal(err)
	}
	edited := strings.Replace(string(data), "The person.", "ghp_"+strings.Repeat("a", 36), 1)
	if err := os.WriteFile(hotPath, []byte(edited), 0o644); err != nil {
		t.Fatal(err)
	}
	future := time.Now().Add(2 * time.Second)
	if err := os.Chtimes(hotPath, future, future); err != nil {
		t.Fatal(err)
	}
	if _, err := h.app.Index(ctx); err != nil {
		t.Fatal(err)
	}
	entries, err := h.app.Vault.Entries(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || !strings.Contains(entries[0].Body, "The person.") {
		t.Fatalf("the secret entered the hot entry: %+v", entries)
	}
	if !strings.Contains(h.err.String(), "skipped native hot entry") {
		t.Fatalf("the hot import was not reported: %q", h.err.String())
	}
}

func TestAddAutoDetectsTheRepositoryScope(t *testing.T) {
	h := newHarness(t, "obsidian")
	ctx := context.Background()
	repo := newRepo(t)

	if _, err := h.app.Add(ctx, app.AddOptions{
		Scope: "project", Title: "A project decision", Body: "The decision.",
		CWD: repo,
	}); err != nil {
		t.Fatal(err)
	}
	entries, err := h.app.Vault.Entries(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("the corpus has %d entries", len(entries))
	}
	if entries[0].Project != memory.Slugify("repo") || entries[0].Worktree != "" {
		t.Fatalf("the auto scope is %q@%q", entries[0].Project, entries[0].Worktree)
	}
}

func TestAddAutoDetectsTheWorktreeScope(t *testing.T) {
	h := newHarness(t, "obsidian")
	ctx := context.Background()
	repo := newRepo(t)
	worktree := newLinkedWorktree(t, repo, "feature")

	if _, err := h.app.Add(ctx, app.AddOptions{
		Scope: "project", Title: "A branch decision", Body: "The branch decision.",
		WorktreeAuto: true, CWD: worktree,
	}); err != nil {
		t.Fatal(err)
	}
	entries, err := h.app.Vault.Entries(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Project != memory.Slugify("repo") || entries[0].Worktree != "feature" {
		t.Fatalf("the auto scope is %+v", entries)
	}
}

func TestAddRejectsProjectScopeOutsideAGitRepository(t *testing.T) {
	h := newHarness(t, "obsidian")
	_, err := h.app.Add(context.Background(), app.AddOptions{
		Scope: "project", Title: "A decision", Body: "The decision.",
		CWD: t.TempDir(),
	})
	if err == nil || !strings.Contains(err.Error(), "git repository") {
		t.Fatalf("the error is %v", err)
	}
}

func TestAddRejectsAnUnsafeProjectSlug(t *testing.T) {
	h := newHarness(t, "obsidian")
	_, err := h.app.Add(context.Background(), app.AddOptions{
		Scope: "project", Title: "A decision", Body: "The decision.",
		Project: "../outside",
	})
	if err == nil || !strings.Contains(err.Error(), "invalid project slug") {
		t.Fatalf("the error is %v", err)
	}
}

func TestBriefScopesTheProjectAndWorktreeEntries(t *testing.T) {
	h := newHarness(t, "obsidian")
	ctx := context.Background()
	repo := newRepo(t)
	worktree := newLinkedWorktree(t, repo, "feature")
	slug := memory.Slugify("repo")

	if _, err := h.app.Add(ctx, app.AddOptions{
		Scope: "lesson", Title: "Global rule", Body: "global body",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := h.app.Add(ctx, app.AddOptions{
		Scope: "project", Title: "Project decision", Body: "project body", Project: slug,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := h.app.Add(ctx, app.AddOptions{
		Scope: "project", Title: "Worktree decision", Body: "worktree body",
		Project: slug, Worktree: "feature",
	}); err != nil {
		t.Fatal(err)
	}

	text, err := h.app.Brief(ctx, app.BriefOptions{CWD: worktree})
	if err != nil {
		t.Fatal(err)
	}
	project := strings.Index(text, "## Project:")
	projectBody := strings.Index(text, "project body")
	worktreeSection := strings.Index(text, "## Worktree:")
	worktreeBody := strings.Index(text, "worktree body")
	if project < 0 || worktreeSection < 0 {
		t.Fatalf("the brief is missing a section:\n%s", text)
	}
	if !(project < projectBody && projectBody < worktreeSection && worktreeSection < worktreeBody) {
		t.Fatalf("the scopes are out of order:\n%s", text)
	}
	if strings.Contains(text[worktreeSection:], "project body") {
		t.Fatalf("the project body leaked into the worktree section:\n%s", text)
	}
}

func TestBriefAutoDetectionDoesNotWarnAboutAMissingNote(t *testing.T) {
	h := newHarness(t, "obsidian")
	ctx := context.Background()
	repo := newRepo(t)
	slug := memory.Slugify("repo")
	if _, err := h.app.Add(ctx, app.AddOptions{
		Scope: "project", Title: "A project decision", Body: "The decision.", Project: slug,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := h.app.Brief(ctx, app.BriefOptions{CWD: repo}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(h.err.String(), "not found") {
		t.Fatalf("auto-detection warned about the missing note: %q", h.err.String())
	}
}

func TestBriefExplicitProjectStillWarns(t *testing.T) {
	h := newHarness(t, "obsidian")
	_, err := h.app.Brief(context.Background(), app.BriefOptions{
		Project: "missing", ProjectExplicit: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(h.err.String(), "project 'missing' not found") {
		t.Fatalf("the explicit project did not warn: %q", h.err.String())
	}
}

func TestBriefGlobalSkipsAutoDetection(t *testing.T) {
	h := newHarness(t, "obsidian")
	ctx := context.Background()
	repo := newRepo(t)
	if _, err := h.app.Add(ctx, app.AddOptions{
		Scope: "project", Title: "A project decision", Body: "The decision.",
		Project: memory.Slugify("repo"),
	}); err != nil {
		t.Fatal(err)
	}
	text, err := h.app.Brief(ctx, app.BriefOptions{Global: true, CWD: repo})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(text, "## Project:") || strings.Contains(text, "## Worktree:") {
		t.Fatalf("--global still scoped the brief:\n%s", text)
	}
}

func TestDoctorReportsAStaleWorktreeAndItsNote(t *testing.T) {
	h := newHarness(t, "obsidian")
	ctx := context.Background()
	repo := newRepo(t)
	newLinkedWorktree(t, repo, "feature")
	slug := memory.Slugify("repo")
	if _, err := h.app.Add(ctx, app.AddOptions{
		Scope: "project", Title: "A branch decision", Body: "The decision.",
		Project: slug, Worktree: "feature",
	}); err != nil {
		t.Fatal(err)
	}
	notes := filepath.Join(h.root, "projects")
	if err := os.MkdirAll(notes, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(notes, slug+"@feature.md"), []byte("branch note"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(repo, ".git", "worktrees", "feature")); err != nil {
		t.Fatal(err)
	}
	t.Chdir(repo)

	report, err := h.app.Doctor(ctx)
	if err != nil {
		t.Fatal(err)
	}
	problems := strings.Join(report.Problems, "\n")
	if !strings.Contains(problems, "stale worktree: "+slug+"@feature") {
		t.Fatalf("the stale entry was not reported: %v", report.Problems)
	}
	if !strings.Contains(problems, "stale worktree note: "+slug+"@feature") {
		t.Fatalf("the stale note was not reported: %v", report.Problems)
	}
}

func TestPruneWorktreesDryRunKeepsEverything(t *testing.T) {
	h := newHarness(t, "obsidian")
	ctx := context.Background()
	repo := newRepo(t)
	newLinkedWorktree(t, repo, "feature")
	slug := memory.Slugify("repo")
	if _, err := h.app.Add(ctx, app.AddOptions{
		Scope: "project", Title: "A branch decision", Body: "The decision.",
		Project: slug, Worktree: "feature",
	}); err != nil {
		t.Fatal(err)
	}
	note := filepath.Join(h.root, "projects", slug+"@feature.md")
	if err := os.MkdirAll(filepath.Dir(note), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(note, []byte("branch note"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(repo, ".git", "worktrees", "feature")); err != nil {
		t.Fatal(err)
	}

	report, err := h.app.PruneWorktrees(ctx, app.PruneWorktreesOptions{CWD: repo})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Entries) != 1 || len(report.Notes) != 1 {
		t.Fatalf("the dry run reported %+v", report)
	}
	if _, err := os.Stat(note); err != nil {
		t.Fatalf("the dry run removed the note: %v", err)
	}
	entries, err := h.app.Vault.Entries(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("the dry run removed the entry: %+v", entries)
	}
}

func TestPruneWorktreesApplyRemovesEntriesAndNotes(t *testing.T) {
	h := newHarness(t, "obsidian")
	ctx := context.Background()
	repo := newRepo(t)
	newLinkedWorktree(t, repo, "feature")
	slug := memory.Slugify("repo")
	if _, err := h.app.Add(ctx, app.AddOptions{
		Scope: "project", Title: "A branch decision", Body: "The decision.",
		Project: slug, Worktree: "feature",
	}); err != nil {
		t.Fatal(err)
	}
	note := filepath.Join(h.root, "projects", slug+"@feature.md")
	if err := os.MkdirAll(filepath.Dir(note), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(note, []byte("branch note"), 0o644); err != nil {
		t.Fatal(err)
	}
	topic := filepath.Join(h.root, "native", "minimax-agent", "topics", memory.ASCIISlug("A branch decision")+".md")
	if err := os.RemoveAll(filepath.Join(repo, ".git", "worktrees", "feature")); err != nil {
		t.Fatal(err)
	}

	report, err := h.app.PruneWorktrees(ctx, app.PruneWorktreesOptions{CWD: repo, Apply: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Entries) != 1 || len(report.Notes) != 1 {
		t.Fatalf("the prune reported %+v", report)
	}
	if _, err := os.Stat(note); !os.IsNotExist(err) {
		t.Fatal("the worktree note survived")
	}
	if _, err := os.Stat(topic); !os.IsNotExist(err) {
		t.Fatal("the projection survived")
	}
	entries, err := h.app.Vault.Entries(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("the entry survived or was reimported: %+v", entries)
	}
	lines, err := h.app.Log(ctx, 5)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(lines, "\n"), "prune-worktrees | entries=1 notes=1") {
		t.Fatalf("the prune was not logged: %v", lines)
	}
}

func TestPruneWorktreesNeverRemovesPinnedOrHot(t *testing.T) {
	h := newHarness(t, "obsidian")
	ctx := context.Background()
	repo := newRepo(t)
	newLinkedWorktree(t, repo, "feature")
	slug := memory.Slugify("repo")
	for _, entry := range []app.AddOptions{
		{Scope: "project", Title: "A pinned branch decision", Body: "The decision.", Project: slug, Worktree: "feature", Pinned: true},
		{Scope: "user", Title: "A hot branch decision", Body: "The decision.", Project: slug, Worktree: "feature", Tier: "hot"},
	} {
		if _, err := h.app.Add(ctx, entry); err != nil {
			t.Fatal(err)
		}
	}
	note := filepath.Join(h.root, "projects", slug+"@feature.md")
	if err := os.MkdirAll(filepath.Dir(note), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(note, []byte("branch note"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(repo, ".git", "worktrees", "feature")); err != nil {
		t.Fatal(err)
	}

	report, err := h.app.PruneWorktrees(ctx, app.PruneWorktreesOptions{CWD: repo, Apply: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Entries) != 0 || len(report.Protected) != 2 || len(report.ProtectedNotes) != 1 {
		t.Fatalf("the protected entries were not reported: %+v", report)
	}
	if _, err := os.Stat(note); err != nil {
		t.Fatalf("the note of a protected scope was removed: %v", err)
	}
	entries, err := h.app.Vault.Entries(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("a protected entry was removed: %+v", entries)
	}
}

func TestPruneWorktreesRequiresAGitRepository(t *testing.T) {
	h := newHarness(t, "obsidian")
	_, err := h.app.PruneWorktrees(context.Background(), app.PruneWorktreesOptions{
		CWD: t.TempDir(), Apply: true,
	})
	if err == nil || !strings.Contains(err.Error(), "git repository") {
		t.Fatalf("the error is %v", err)
	}
}

func TestPruneWorktreesRefusesWithoutRemovalCapabilities(t *testing.T) {
	h := newHarness(t, "obsidian")
	repo := newRepo(t)
	slug := memory.Slugify("repo")
	h.app.Vault = noRemovalVault{entries: []memory.Entry{{
		Title: "A branch decision", Type: memory.TypeProject, Project: slug, Worktree: "feature",
	}}}
	_, err := h.app.PruneWorktrees(context.Background(), app.PruneWorktreesOptions{
		CWD: repo, Apply: true,
	})
	if err == nil || !strings.Contains(err.Error(), "cannot remove entries") {
		t.Fatalf("the error is %v", err)
	}
}

func TestBriefCarriesTheWorktreeNotes(t *testing.T) {
	h := newHarness(t, "obsidian")
	ctx := context.Background()
	repo := newRepo(t)
	worktree := newLinkedWorktree(t, repo, "feature")
	slug := memory.Slugify("repo")
	dir := filepath.Join(h.root, "projects")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, slug+"@feature.md"), []byte("# Feature\n\nThe worktree notes.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	text, err := h.app.Brief(ctx, app.BriefOptions{CWD: worktree})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, "## Worktree: "+slug+"@feature") || !strings.Contains(text, "The worktree notes.") {
		t.Fatalf("the brief does not carry the worktree notes: %q", text)
	}
}

func TestBriefCarriesTheHotTier(t *testing.T) {
	h := newHarness(t, "obsidian")
	ctx := context.Background()

	hot := "---\ndescription: Hot tier\ntype: index\n---\n\n## Profile\n\nThe user writes English memory.\n"
	if err := os.WriteFile(filepath.Join(h.root, "MEMORY.md"), []byte(hot), 0o644); err != nil {
		t.Fatal(err)
	}
	text, err := h.app.Brief(ctx, app.BriefOptions{})
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
	text, err := h.app.Brief(ctx, app.BriefOptions{Project: "demo", ProjectExplicit: true})
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

func TestInstallCreatesAWholeStore(t *testing.T) {
	h := newHarness(t, "obsidian")
	ctx := context.Background()

	// A store that does not exist yet.
	if err := os.RemoveAll(filepath.Join(h.root, "corpus")); err != nil {
		t.Fatal(err)
	}

	result, err := h.app.Install(ctx)
	if err != nil {
		t.Fatalf("cannot install: %v", err)
	}
	if result.Store != h.root {
		t.Fatalf("the store is %q, want %q", result.Store, h.root)
	}
	for _, dir := range []string{"user", "lesson", "project", "reference"} {
		path := filepath.Join(h.root, "corpus", dir)
		info, err := os.Stat(path)
		if err != nil || !info.IsDir() {
			t.Fatalf("the folder %s is missing: %v", dir, err)
		}
	}
	for _, file := range []string{"MEMORY.md", "projects", "log"} {
		if _, err := os.Stat(filepath.Join(h.root, file)); err != nil {
			t.Fatalf("%s is missing: %v", file, err)
		}
	}
	// The store that was just created is whole: the doctor has nothing to
	// say about it.
	report, err := h.app.Doctor(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !report.OK() {
		t.Fatalf("the doctor found problems: %v", report.Problems)
	}
}

func TestAProjectionFollowsTheCorpus(t *testing.T) {
	h := newHarness(t, "obsidian")
	ctx := context.Background()
	if _, err := h.app.Add(ctx, app.AddOptions{
		Scope: "lesson", Title: "A durable lesson",
		Description: "Kept as words",
		Body:        "The first rule. Why it matters.",
	}); err != nil {
		t.Fatal(err)
	}
	topic := filepath.Join(h.root, "native", "minimax-agent", "topics", "a-durable-lesson.md")
	data, err := os.ReadFile(topic)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "A+durable") {
		t.Fatalf("the description was query-escaped:\n%s", data)
	}
	parsed, ok := native.ParseTopic(string(data), "a-durable-lesson.md")
	if !ok {
		t.Fatalf("the topic does not parse:\n%s", data)
	}
	if parsed.Description != "Kept as words" {
		t.Fatalf("the description is %q", parsed.Description)
	}
	if strings.Contains(parsed.Body, "smart-agents-memory:title") {
		t.Fatalf("the title marker leaked into the body: %q", parsed.Body)
	}

	if _, err := h.app.Add(ctx, app.AddOptions{
		Scope: "lesson", Title: "A durable lesson",
		Body: "The second rule. Why the first one was incomplete.",
	}); err != nil {
		t.Fatal(err)
	}
	data, err = os.ReadFile(topic)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "The second rule.") {
		t.Fatalf("the append did not reach the topic:\n%s", data)
	}

	note := filepath.Join(h.root, "corpus", "lesson", "a-durable-lesson.md")
	body, err := os.ReadFile(note)
	if err != nil {
		t.Fatal(err)
	}
	updated := strings.Replace(string(body), "The first rule. Why it matters.", "The edited rule. Why a projection must follow the corpus.", 1)
	if err := os.WriteFile(note, []byte(updated), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := h.app.Index(ctx); err != nil {
		t.Fatal(err)
	}
	data, err = os.ReadFile(topic)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "The edited rule.") {
		t.Fatalf("the corpus edit did not reach the topic:\n%s", data)
	}
	second, err := h.app.Index(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if second.Imported != 0 || second.Updated != 0 || second.Exported != 0 {
		t.Fatalf("the second run had work to do: %+v", second)
	}
}

func TestANewerTopicUpdatesTheCorpusAndKeepsItsType(t *testing.T) {
	h := newHarness(t, "obsidian")
	ctx := context.Background()
	if _, err := h.app.Add(ctx, app.AddOptions{
		Scope: "user", Title: "A preference", Tier: "cold",
		Body: "The first body. Why it was recorded.",
	}); err != nil {
		t.Fatal(err)
	}
	topic := filepath.Join(h.root, "native", "minimax-agent", "topics", "a-preference.md")
	data, err := os.ReadFile(topic)
	if err != nil {
		t.Fatal(err)
	}
	edited := strings.Replace(string(data), "The first body. Why it was recorded.", "The topic was edited. Why the corpus should follow.", 1)
	if err := os.WriteFile(topic, []byte(edited), 0o644); err != nil {
		t.Fatal(err)
	}
	future := time.Now().Add(2 * time.Second)
	if err := os.Chtimes(topic, future, future); err != nil {
		t.Fatal(err)
	}
	if _, err := h.app.Index(ctx); err != nil {
		t.Fatal(err)
	}
	entries, err := h.app.Vault.Entries(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("the corpus has %d entries", len(entries))
	}
	if entries[0].Type != memory.TypeUser {
		t.Fatalf("the type became %q", entries[0].Type)
	}
	if entries[0].Native != "minimax-user" {
		t.Fatalf("the native target became %q", entries[0].Native)
	}
	if !strings.Contains(entries[0].Body, "The topic was edited.") {
		t.Fatalf("the body is %q", entries[0].Body)
	}
}

func TestIndexRemovesAnOrphanTopic(t *testing.T) {
	h := newHarness(t, "obsidian")
	ctx := context.Background()
	if _, err := h.app.Add(ctx, app.AddOptions{
		Scope: "lesson", Title: "A durable lesson", Body: "The rule. Why it matters.",
	}); err != nil {
		t.Fatal(err)
	}
	orphan := filepath.Join(h.root, "native", "minimax-agent", "topics", "not-an-entry.md")
	if err := os.WriteFile(orphan, []byte("this is not a topic\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := h.app.Index(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(orphan); !os.IsNotExist(err) {
		t.Fatal("the orphan topic is still there")
	}
}

func TestHotTierComesFromTheCorpus(t *testing.T) {
	h := newHarness(t, "obsidian")
	ctx := context.Background()
	hot := "---\ndescription: Hot tier\ntype: index\nalwaysApply: true\n---\n\n" +
		native.HotProfileStart + "\nThe person writes in English because the models read it.\n" + native.HotProfileEnd + "\n"
	if err := os.WriteFile(filepath.Join(h.root, "MEMORY.md"), []byte(hot), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := h.app.Add(ctx, app.AddOptions{
		Scope: "user", Title: "Identity", Tier: "hot", Body: "The person.",
	}); err != nil {
		t.Fatal(err)
	}
	if countTitle(t, h, "Profile") != 1 {
		t.Fatal("the freeform hot block was not imported once")
	}
	second, err := h.app.Index(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if second.Imported != 0 || second.Updated != 0 || second.Exported != 0 {
		t.Fatalf("the second run had work to do: %+v", second)
	}
	if countTitle(t, h, "Profile") != 1 {
		t.Fatal("the second run created another Profile")
	}
	data, err := os.ReadFile(filepath.Join(h.root, "MEMORY.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "### Identity") || !strings.Contains(string(data), native.HotProfileStart) {
		t.Fatalf("the hot tier was not regenerated:\n%s", data)
	}
	user, err := os.ReadFile(filepath.Join(h.root, "native", "minimax-user", "user.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(user), "### Identity") || !strings.Contains(string(user), native.HotProfileStart) {
		t.Fatalf("the user store does not carry the hot tier:\n%s", user)
	}
}

func TestBriefListsTheCorpus(t *testing.T) {
	h := newHarness(t, "obsidian")
	ctx := context.Background()
	if _, err := h.app.Add(ctx, app.AddOptions{
		Scope: "lesson", Title: "A durable lesson", Description: "Kept as words",
		Body: "The rule. Why it matters.",
	}); err != nil {
		t.Fatal(err)
	}
	text, err := h.app.Brief(ctx, app.BriefOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, "## Index") || !strings.Contains(text, "A durable lesson") || !strings.Contains(text, "Kept as words") {
		t.Fatalf("the brief is missing the index:\n%s", text)
	}
	if strings.Contains(text, "The rule. Why it matters.") {
		t.Fatalf("the brief dumped a cold body:\n%s", text)
	}
}

func TestAGatewayWithoutProjectNotesSaysSo(t *testing.T) {
	h := newHarness(t, "obsidian")
	h.app.Vault = bareVault{}
	_, err := h.app.Brief(context.Background(), app.BriefOptions{Project: "my-app", ProjectExplicit: true})
	if err == nil || !strings.Contains(err.Error(), "does not keep project notes") {
		t.Fatalf("the error is %v", err)
	}
}

func TestSQLiteInstallSatisfiesTheDoctor(t *testing.T) {
	h := newHarness(t, "sqlite")
	ctx := context.Background()
	if _, err := h.app.Install(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(h.root, "corpus")); err == nil {
		t.Fatal("sqlite created a corpus directory")
	}
	report, err := h.app.Doctor(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !report.OK() {
		t.Fatalf("the doctor found problems: %v", report.Problems)
	}
}

func TestAnUnreadableNoteIsReported(t *testing.T) {
	h := newHarness(t, "obsidian")
	ctx := context.Background()
	if _, err := h.app.Install(ctx); err != nil {
		t.Fatal(err)
	}
	note := filepath.Join(h.root, "corpus", "lesson", "locked.md")
	if err := os.WriteFile(note, []byte("---\ntitle: \"Locked\"\ntype: lesson\n---\n\nBody.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(note, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(note, 0o644) })
	report, err := h.app.Doctor(ctx)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, problem := range report.Problems {
		if strings.Contains(problem, "locked.md") {
			found = true
		}
	}
	if !found {
		t.Fatalf("the unreadable note was not reported: %v", report.Problems)
	}
}

// newRepo builds a minimal standard checkout in a temporary directory.
func newRepo(t *testing.T) string {
	t.Helper()
	repo := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	return repo
}

// newLinkedWorktree registers a linked worktree under repo and builds the
// worktree's .git file.
func newLinkedWorktree(t *testing.T, repo, name string) string {
	t.Helper()
	worktree := filepath.Join(t.TempDir(), name)
	gitDir := filepath.Join(repo, ".git", "worktrees", name)
	if err := os.MkdirAll(gitDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(worktree, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(gitDir, "commondir"), []byte("../..\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(worktree, ".git"), []byte("gitdir: "+gitDir+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return worktree
}

func countTitle(t *testing.T, h *harness, title string) int {
	t.Helper()
	entries, err := h.app.Vault.Entries(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, e := range entries {
		if memory.NormTitle(e.Title) == memory.NormTitle(title) {
			n++
		}
	}
	return n
}

// bareVault is a gateway with no project notes, so brief can say so.
type bareVault struct{}

func (bareVault) Kind() string                                    { return "bare" }
func (bareVault) Root() string                                    { return "bare" }
func (bareVault) Entries(context.Context) ([]memory.Entry, error) { return nil, nil }
func (bareVault) Create(context.Context, memory.Entry) (memory.Entry, error) {
	return memory.Entry{}, nil
}
func (bareVault) Update(context.Context, memory.Entry) error { return nil }
func (bareVault) Search(context.Context, string, int) ([]vault.Hit, error) {
	return nil, nil
}
func (bareVault) AppendLog(context.Context, string) error         { return nil }
func (bareVault) LogLines(context.Context, int) ([]string, error) { return nil, nil }
func (bareVault) Ready(context.Context) error                     { return nil }

// noRemovalVault is a corpus without the optional removal capabilities.
type noRemovalVault struct {
	bareVault
	entries []memory.Entry
}

func (v noRemovalVault) Entries(context.Context) ([]memory.Entry, error) {
	return v.entries, nil
}
