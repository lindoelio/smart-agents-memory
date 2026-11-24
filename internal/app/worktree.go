package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/lindoelio/smart-agents-memory/internal/gitscope"
	"github.com/lindoelio/smart-agents-memory/internal/memory"
	"github.com/lindoelio/smart-agents-memory/internal/vault"
)

// resolveScopeCWD resolves the repository and worktree that contain cwd.
// An empty cwd means the process working directory.
func resolveScopeCWD(cwd string) (gitscope.Info, bool) {
	if strings.TrimSpace(cwd) == "" {
		dir, err := os.Getwd()
		if err != nil {
			return gitscope.Info{}, false
		}
		cwd = dir
	}
	return gitscope.Resolve(cwd)
}

// validScopeName keeps a project or worktree name from escaping the corpus
// or colliding with the <project>@<worktree> note convention.
func validScopeName(name string) bool {
	if name == "" || name == "." || name == ".." {
		return false
	}
	if name != filepath.Base(name) {
		return false
	}
	for _, r := range name {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			continue
		}
		switch r {
		case '-', '_', '.':
			continue
		default:
			return false
		}
	}
	return true
}

// resolveAddScope fills the project and worktree from the working directory
// when the caller did not name them. A worktree is opt-in: a project
// decision is repository-wide unless --worktree auto is requested.
func (a *App) resolveAddScope(opts *AddOptions, scope memory.Type) error {
	if opts.Project == "" && scope == memory.TypeProject {
		info, ok := resolveScopeCWD(opts.CWD)
		if !ok {
			return fmt.Errorf("not inside a git repository; pass --project")
		}
		opts.Project = info.RepoSlug
		if opts.WorktreeAuto {
			opts.Worktree = info.WorktreeID
		}
	}
	if opts.WorktreeAuto {
		if opts.Project == "" && scope != memory.TypeProject {
			return fmt.Errorf("--worktree auto needs a project; pass --project or --scope project")
		}
		info, ok := resolveScopeCWD(opts.CWD)
		if !ok {
			return fmt.Errorf("not inside a git repository; cannot resolve --worktree auto")
		}
		if opts.Project != "" && info.RepoSlug != opts.Project {
			return fmt.Errorf("--worktree auto resolved %q, not %q", info.RepoSlug, opts.Project)
		}
		if opts.Project == "" {
			opts.Project = info.RepoSlug
		}
		opts.Worktree = info.WorktreeID
	}
	if opts.Project != "" && !validScopeName(opts.Project) {
		return fmt.Errorf("invalid project slug %q", opts.Project)
	}
	if opts.Worktree != "" && !validScopeName(opts.Worktree) {
		return fmt.Errorf("invalid worktree id %q", opts.Worktree)
	}
	if opts.Worktree != "" && opts.Project == "" {
		return fmt.Errorf("a worktree needs a project; pass --project or --scope project")
	}
	return nil
}

// StaleWorktreeScopes lists the memories scoped to a worktree that is no
// longer registered in the active repository.
type StaleWorktreeScopes struct {
	Repo    gitscope.Info
	InRepo  bool
	Entries []memory.Entry
	Notes   []string
}

// staleWorktreeScopes discovers the scopes for doctor and prune-worktrees.
func (a *App) staleWorktreeScopes(ctx context.Context, cwd string) (StaleWorktreeScopes, error) {
	var report StaleWorktreeScopes
	info, ok := resolveScopeCWD(cwd)
	if !ok {
		return report, nil
	}
	report.Repo = info
	report.InRepo = true
	active, err := gitscope.ActiveWorktrees(info.RepoRoot)
	if err != nil {
		return report, err
	}
	corpus, err := a.Vault.Entries(ctx)
	if err != nil {
		return report, err
	}
	for _, e := range corpus {
		if e.Project != info.RepoSlug || e.Worktree == "" {
			continue
		}
		if !active[e.Worktree] {
			report.Entries = append(report.Entries, e)
		}
	}
	if lister, ok := a.Vault.(vault.ProjectNoteLister); ok {
		names, err := lister.ProjectNoteNames(ctx)
		if err != nil {
			return report, err
		}
		for _, name := range names {
			project, worktree, found := strings.Cut(name, "@")
			if !found || worktree == "" || project != info.RepoSlug {
				continue
			}
			if !active[worktree] {
				report.Notes = append(report.Notes, name)
			}
		}
	}
	return report, nil
}

// PruneWorktreesOptions selects a dry run or a real cleanup.
type PruneWorktreesOptions struct {
	CWD   string
	Apply bool
}

// PruneWorktreesReport is what prune-worktrees found and, with Apply, did.
type PruneWorktreesReport struct {
	Entries        []memory.Entry
	Notes          []string
	Protected      []memory.Entry
	ProtectedNotes []string
}

// PruneWorktrees removes the unpinned and cold memories scoped to a worktree
// that no longer exists. It never removes a pinned or hot entry.
func (a *App) PruneWorktrees(ctx context.Context, opts PruneWorktreesOptions) (PruneWorktreesReport, error) {
	report := PruneWorktreesReport{}
	stale, err := a.staleWorktreeScopes(ctx, opts.CWD)
	if err != nil {
		return report, err
	}
	if !stale.InRepo {
		return report, fmt.Errorf("not inside a git repository")
	}
	for _, e := range stale.Entries {
		if e.Pinned || e.EffectiveTier() == memory.TierHot {
			report.Protected = append(report.Protected, e)
			continue
		}
		report.Entries = append(report.Entries, e)
	}
	protectedScopes := map[string]bool{}
	for _, e := range report.Protected {
		protectedScopes[e.Project+"@"+e.Worktree] = true
	}
	for _, name := range stale.Notes {
		if protectedScopes[name] {
			// The note belongs to a worktree whose entry is pinned or
			// hot. Keep the context until the entry is released.
			report.ProtectedNotes = append(report.ProtectedNotes, name)
			continue
		}
		report.Notes = append(report.Notes, name)
	}

	if !opts.Apply {
		return report, nil
	}
	if len(report.Entries) == 0 && len(report.Notes) == 0 {
		return report, nil
	}
	if len(report.Entries) > 0 {
		remover, ok := a.Vault.(vault.Remover)
		if !ok {
			return report, fmt.Errorf("the %s gateway cannot remove entries", a.Vault.Kind())
		}
		for _, e := range report.Entries {
			if err := remover.Remove(ctx, e); err != nil {
				return report, fmt.Errorf("cannot remove %q: %w", e.Title, err)
			}
		}
	}
	if len(report.Notes) > 0 {
		noteRemover, ok := a.Vault.(vault.ProjectNoteRemover)
		if !ok {
			return report, fmt.Errorf("the %s gateway cannot remove project notes", a.Vault.Kind())
		}
		for _, name := range report.Notes {
			if err := noteRemover.RemoveProjectNote(ctx, name); err != nil {
				return report, fmt.Errorf("cannot remove note %q: %w", name, err)
			}
		}
	}
	line := fmt.Sprintf("prune-worktrees | entries=%d notes=%d", len(report.Entries), len(report.Notes))
	if err := a.Vault.AppendLog(ctx, line); err != nil {
		return report, err
	}
	if _, err := a.index(ctx, indexOptions{skipNativeImport: true}); err != nil {
		return report, err
	}
	return report, nil
}
