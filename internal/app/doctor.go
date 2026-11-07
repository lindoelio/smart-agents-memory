package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/lindoelio/smart-agents-memory/internal/link"
	"github.com/lindoelio/smart-agents-memory/internal/memory"
	"github.com/lindoelio/smart-agents-memory/internal/native"
	"github.com/lindoelio/smart-agents-memory/internal/vault"
)

// DoctorReport is the outcome of the doctor command.
type DoctorReport struct {
	// Problems are things that are wrong.
	Problems []string
	// Notes are things worth knowing.
	Notes []string
}

// OK reports whether the installation is healthy.
func (r DoctorReport) OK() bool { return len(r.Problems) == 0 }

// Doctor checks the links, the projections, the index and the store.
func (a *App) Doctor(ctx context.Context) (DoctorReport, error) {
	report := DoctorReport{}

	// The links of every tool.
	for _, target := range a.LinkTargets() {
		if ok, reason := link.Check(target.Path, target.Source); !ok {
			report.Problems = append(report.Problems, reason)
		}
	}

	// The store itself.
	root := a.LocalRoot
	if info, err := os.Stat(root); err != nil || !info.IsDir() {
		report.Problems = append(report.Problems, "missing: "+root)
	} else {
		report.Notes = append(report.Notes, "store: "+root)
	}

	corpus, err := a.Vault.Entries(ctx)
	if err != nil {
		report.Problems = append(report.Problems, "cannot read the corpus: "+err.Error())
	} else {
		report.Notes = append(report.Notes, fmt.Sprintf("corpus: %d entries", len(corpus)))
	}

	// Every entry must have a type and a title.
	known := map[string]bool{}
	for _, e := range corpus {
		key := memory.NormTitle(e.Title)
		if known[key] {
			report.Problems = append(report.Problems, "duplicate title: "+e.Title)
		}
		known[key] = true
		if !e.Type.Valid() {
			report.Problems = append(report.Problems, "invalid type "+string(e.Type)+": "+e.Path)
		}
	}

	// The projections must have a counterpart in the corpus.
	for _, rel := range a.projectionFiles() {
		content, err := a.Projections.Read(native.TargetMiniMaxAgent, rel)
		if err != nil {
			continue
		}
		entry, ok := native.ParseTopic(content, filepath.Base(rel))
		if !ok {
			report.Problems = append(report.Problems, "invalid topic file (runtime format): "+rel)
			continue
		}
		if !known[memory.NormTitle(entry.Title)] {
			report.Problems = append(report.Problems,
				"topic with no corpus pair (run `smart-agents-memory index`): "+rel+" :: "+entry.Title)
		}
	}

	// The content of the native stores must be there.
	for _, check := range []struct {
		target native.Target
		rel    string
		label  string
	}{
		{native.TargetMiniMaxUser, "user.md", "user profile"},
		{native.TargetMiniMaxAgent, "MEMORY.md", "agent memory"},
	} {
		if !a.Projections.Exists(check.target, check.rel) {
			report.Problems = append(report.Problems, "content missing ("+check.label+"): "+check.rel)
		}
	}

	// The hot tier must be there.
	if _, err := os.Stat(a.HotPath()); err != nil {
		report.Problems = append(report.Problems, "content missing (hot tier): "+a.HotPath())
	}

	// The index must be there and fresh.
	indexPath := filepath.Join(a.LocalRoot, "corpus", "INDEX.md")
	if locator, ok := a.Vault.(vault.IndexLocator); ok {
		indexPath = locator.IndexPath()
	}
	if info, err := os.Stat(indexPath); err != nil {
		report.Problems = append(report.Problems, "no index — run `smart-agents-memory index`")
	} else {
		age := time.Since(info.ModTime()).Hours()
		if age > 24 {
			report.Notes = append(report.Notes, fmt.Sprintf("index is stale (%.1fh) — run `smart-agents-memory index`", age))
		} else {
			report.Notes = append(report.Notes, "index: fresh")
		}
	}

	// The skill must be installed.
	for _, dir := range skillDirs(homeDir()) {
		path := filepath.Join(dir, "smart-agents-memory")
		if _, err := os.Stat(path); err != nil {
			report.Problems = append(report.Problems, "skill not installed: "+path+" (run `smart-agents-memory install`)")
		}
	}

	return report, nil
}

// projectionFiles lists the topic files of the MiniMax agent store.
func (a *App) projectionFiles() []string {
	files, err := a.Projections.List(native.TargetMiniMaxAgent, "topics")
	if err != nil {
		return nil
	}
	return files
}

// homeDir returns the home directory of the user.
func homeDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return os.Getenv("HOME")
	}
	return home
}
