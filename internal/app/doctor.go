package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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

	// The store itself. Ready is the gateway's own check: a file store is a
	// directory, a sqlite store is a database file.
	var corpus []memory.Entry
	if err := a.Vault.Ready(ctx); err != nil {
		report.Problems = append(report.Problems, err.Error())
	} else {
		report.Notes = append(report.Notes, "store: "+a.Vault.Root())
		entries, err := a.Vault.Entries(ctx)
		if err != nil {
			report.Problems = append(report.Problems, "cannot read the corpus: "+err.Error())
		} else {
			corpus = entries
			report.Notes = append(report.Notes, fmt.Sprintf("corpus: %d entries", len(corpus)))
		}
		if diag, ok := a.Vault.(vault.Diagnostics); ok {
			problems, err := diag.Problems(ctx)
			if err != nil {
				report.Problems = append(report.Problems, "cannot read the store: "+err.Error())
			}
			report.Problems = append(report.Problems, problems...)
		}
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

	// The index is a problem only when this gateway publishes one and the
	// file is missing. A gateway without an index is not broken.
	if _, publishes := a.Vault.(vault.IndexWriter); publishes {
		indexPath := ""
		if locator, ok := a.Vault.(vault.IndexLocator); ok {
			indexPath = locator.IndexPath()
		}
		if indexPath == "" {
			report.Problems = append(report.Problems, "the gateway publishes an index but did not say where")
		} else if info, err := os.Stat(indexPath); err != nil {
			report.Problems = append(report.Problems, "no index — run `smart-agents-memory index`")
		} else {
			age := time.Since(info.ModTime()).Hours()
			if age > 24 {
				report.Notes = append(report.Notes, fmt.Sprintf("index is stale (%.1fh) — run `smart-agents-memory index`", age))
			} else {
				report.Notes = append(report.Notes, "index: fresh")
			}
		}
	}

	// A worktree that no longer exists leaves corpus entries and project
	// notes behind. Report them; prune-worktrees removes them on request.
	scopes, err := a.staleWorktreeScopes(ctx, "")
	if err != nil {
		report.Problems = append(report.Problems, "cannot check worktrees: "+err.Error())
	} else {
		for _, e := range scopes.Entries {
			report.Problems = append(report.Problems,
				fmt.Sprintf("stale worktree: %s@%s (%s)", e.Project, e.Worktree, e.Title))
		}
		for _, name := range scopes.Notes {
			report.Problems = append(report.Problems, "stale worktree note: "+name)
		}
	}

	// A symlinked team note is skipped on read. Report it so a human knows
	// the note was ignored instead of silently losing it.
	if info, ok := resolveScopeCWD(""); ok {
		for _, path := range symlinkedTeamNotes(info.WorktreeRoot) {
			report.Problems = append(report.Problems, "symlinked team note ignored: "+path)
		}
	}

	// The skill must be installed.
	for _, dir := range skillDirs(homeDir()) {
		path := filepath.Join(dir, "smart-agents-memory")
		if _, err := os.Stat(path); err != nil {
			report.Problems = append(report.Problems, "skill not installed: "+path+" (run `smart-agents-memory install`)")
		}
	}

	// Capture sources are reported, not failed: a harness may not be
	// installed on this machine.
	for _, status := range a.SourceStatuses() {
		line := "source " + status.ID + ": "
		switch {
		case !status.Enabled:
			line += "disabled"
		case status.Exists:
			line += "ok"
		default:
			line += "missing"
		}
		if status.Seeded {
			line += ", seed: done"
		} else {
			line += ", seed: pending"
		}
		if status.Backfilled {
			line += ", backfill: done"
		} else {
			line += ", backfill: not run"
		}
		if !status.LastSync.IsZero() {
			line += fmt.Sprintf(", last sync %s ago", time.Since(status.LastSync).Round(time.Second))
		}
		if len(status.Errors) > 0 {
			line += ", errors: " + strings.Join(status.Errors, "; ")
		}
		report.Notes = append(report.Notes, line)
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

// symlinkedTeamNotes lists the .md symlinks inside the team stores of a
// worktree. The reader skips them on purpose; the doctor reports them so a
// human knows the note was ignored.
func symlinkedTeamNotes(worktreeRoot string) []string {
	var out []string
	for _, rel := range []string{".agents/memory", ".qwen/team-memory"} {
		root := filepath.Join(worktreeRoot, filepath.FromSlash(rel))
		_ = filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if entry.Type()&os.ModeSymlink != 0 && strings.HasSuffix(entry.Name(), ".md") {
				out = append(out, path)
			}
			return nil
		})
	}
	return out
}

// homeDir returns the home directory of the user.
func homeDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return os.Getenv("HOME")
	}
	return home
}
