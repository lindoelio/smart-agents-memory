package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/lindoelio/smart-agents-memory/internal/memory"
	"github.com/lindoelio/smart-agents-memory/internal/native"
	"github.com/lindoelio/smart-agents-memory/internal/vault"
)

// IndexReport is the outcome of a reconciliation.
type IndexReport struct {
	// Entries is the size of the corpus.
	Entries int
	// Imported is how many entries came from the native stores.
	Imported int
	// Updated is how many entries the native stores changed.
	Updated int
	// Exported is how many projections were written.
	Exported int
	// IndexPath is where the index was published, when it was published.
	IndexPath string
}

// Index reconciles the corpus with the native projections and regenerates
// the index.
func (a *App) Index(ctx context.Context) (IndexReport, error) {
	report := IndexReport{}
	if err := a.Projections.EnsureDirs(); err != nil {
		return report, err
	}
	if err := a.ensureSkeleton(); err != nil {
		return report, err
	}

	corpus, err := a.Vault.Entries(ctx)
	if err != nil {
		return report, err
	}
	report.Entries = len(corpus)

	reconciler := a.Reconciler()
	imported, updated, err := reconciler.Import(ctx, corpus)
	if err != nil {
		return report, err
	}
	report.Imported = imported
	report.Updated = updated

	exported, err := reconciler.Export(ctx, corpus)
	if err != nil {
		return report, err
	}
	report.Exported = exported

	if err := reconciler.SyncProfile(a.HotContent()); err != nil {
		return report, err
	}

	if writer, ok := a.Vault.(vault.IndexWriter); ok {
		path, err := writer.WriteIndex(ctx, corpus)
		if err != nil {
			return report, err
		}
		report.IndexPath = path
	}

	return report, nil
}

// ensureSkeleton materializes the files every installation needs: the
// projection stores, and the hot tier.
func (a *App) ensureSkeleton() error {
	for _, file := range []struct {
		target native.Target
		rel    string
	}{
		{native.TargetMiniMaxUser, "user.md"},
		{native.TargetMiniMaxAgent, "MEMORY.md"},
		{native.TargetQwen, "MEMORY.md"},
	} {
		if a.Projections.Exists(file.target, file.rel) {
			continue
		}
		if err := a.Projections.Write(file.target, file.rel, ""); err != nil {
			return err
		}
	}
	if _, err := os.Stat(a.HotPath()); err == nil {
		return nil
	}
	hot := "---\n" +
		"description: Shared memory — the profile and the permanent rules. Read before working.\n" +
		"type: index\n" +
		"alwaysApply: true\n" +
		"---\n\n" + native.HotProfileStart + "\n" + native.HotProfileEnd + "\n"
	return os.WriteFile(a.HotPath(), []byte(hot), 0o644)
}

// Migration is one change the migrate command makes.
type Migration struct {
	// Path is what changes.
	Path string
	// Action is what happens to it.
	Action string
}

// Migrate brings a legacy store to the current layout and vocabulary.
//
// Without apply it only reports. The migration is idempotent: running it
// again finds nothing to do.
func (a *App) Migrate(ctx context.Context, apply bool) ([]Migration, error) {
	var changes []Migration
	record := func(path, action string) {
		changes = append(changes, Migration{Path: path, Action: action})
	}

	corpus, err := a.Vault.Entries(ctx)
	if err != nil {
		return nil, err
	}

	// Every entry is migrated in one pass, and written once: a second
	// write of the same entry would carry the values of the first pass
	// back over the new ones.
	kept := make([]memory.Entry, 0, len(corpus))
	for i, e := range corpus {
		changed := false
		rename := ""

		if !e.Type.Valid() {
			target := legacyType(string(e.Type))
			if target != "" {
				record(e.Path, fmt.Sprintf("type %s -> %s", e.Type, target))
				e.Type = target
				if e.Native == "" {
					e.Native = target.NativeTarget()
				}
				if e.NativeType == "" {
					e.NativeType = target.NativeType()
				}
				changed = true
			}
		}

		if e.Source == "memoria-cli" {
			record(e.Path, "source memoria-cli -> "+memory.DefaultSource)
			e.Source = memory.DefaultSource
			changed = true
		}

		// The entry duplicates another one: the corpus keeps one note
		// per fact.
		if target, ok := mergedEntries[memory.NormTitle(e.Title)]; ok {
			record(e.Path, fmt.Sprintf("merged into %q and removed", target))
			if apply {
				remover, ok := a.Vault.(vault.Remover)
				if !ok {
					continue
				}
				if err := remover.Remove(ctx, e); err != nil {
					return changes, err
				}
			}
			continue
		}

		// The title stayed in the previous vocabulary.
		if current, ok := legacyTitles[memory.NormTitle(e.Title)]; ok {
			record(e.Path, fmt.Sprintf("title %q -> %q", e.Title, current))
			rename = current
			changed = true
		}

		if changed && apply {
			if rename != "" {
				renamer, ok := a.Vault.(vault.Renamer)
				if !ok {
					continue
				}
				renamed, err := renamer.Rename(ctx, e, rename)
				if err != nil {
					return changes, err
				}
				e = renamed
			} else if err := a.Vault.Update(ctx, e); err != nil {
				return changes, err
			}
			corpus[i] = e
		}

		kept = append(kept, e)
	}
	// The corpus that survived the migration is the one the projections
	// are checked against.
	corpus = kept

	// The projections that no entry backs are removed: the runtime would
	// inject them without a counterpart.
	orphans, err := a.Reconciler().Orphans(corpus)
	if err != nil {
		return changes, err
	}
	for _, rel := range orphans {
		record(rel, "orphan projection removed")
		if !apply {
			continue
		}
		if err := a.Projections.Delete("minimax-agent", rel); err != nil {
			return changes, err
		}
	}

	// The legacy markers in the hot tier follow the product name.
	hot := a.HotContent()
	if strings.Contains(hot, "<!-- memoria:hot:start -->") {
		record("MEMORY.md", "hot tier markers renamed")
		if apply {
			updated := strings.ReplaceAll(hot, "<!-- memoria:hot:start -->", "<!-- smart-agents-memory:hot:start -->")
			updated = strings.ReplaceAll(updated, "<!-- memoria:hot:end -->", "<!-- smart-agents-memory:hot:end -->")
			if err := os.WriteFile(a.HotPath(), []byte(updated), 0o644); err != nil {
				return changes, err
			}
		}
	}

	sort.SliceStable(changes, func(i, j int) bool { return changes[i].Path < changes[j].Path })
	return changes, nil
}

// legacyTitles maps the entries whose title stayed in the previous
// vocabulary to their current title, by normalized title.
var legacyTitles = map[string]string{
	memory.NormTitle("Idioma das memórias: inglês por padrão (2026-11-07)"): "Memory language: English by default (2026-11-07)",
	memory.NormTitle("Reconciliação do corpus: mtime decide quem manda"):    "Corpus reconciliation: mtime decides the winner",
}

// mergedEntries maps the entries that duplicate another one, by normalized
// title, to the entry that keeps the fact.
var mergedEntries = map[string]string{
	memory.NormTitle("Nome"): "Name",
}

// legacyType maps a legacy type to the current vocabulary.
func legacyType(legacy string) memory.Type {
	switch legacy {
	case "usuario":
		return memory.TypeUser
	case "licao":
		return memory.TypeLesson
	case "projeto":
		return memory.TypeProject
	case "referencia":
		return memory.TypeReference
	}
	return ""
}

// StoreLayout describes the folders of a file store, for the doctor.
type StoreLayout struct {
	Corpus string
	Native string
	Log    string
	Hot    string
}

// Layout returns the layout of the local store.
func (a *App) Layout() StoreLayout {
	return StoreLayout{
		Corpus: filepath.Join(a.LocalRoot, "corpus"),
		Native: filepath.Join(a.LocalRoot, "native"),
		Log:    filepath.Join(a.LocalRoot, "log"),
		Hot:    a.HotPath(),
	}
}
