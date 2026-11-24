package app

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/lindoelio/smart-agents-memory/internal/memory"
	"github.com/lindoelio/smart-agents-memory/internal/teammemory"
	"github.com/lindoelio/smart-agents-memory/internal/vault"
)

// addTeam writes one entry to the Git-backed team store instead of the vault.
func (a *App) addTeam(ctx context.Context, opts AddOptions, scope memory.Type, source, native string, tier memory.Tier) (string, error) {
	if scope == memory.TypeUser {
		return "", fmt.Errorf("team memory cannot store personal user profile entries; use lesson, project, or reference")
	}
	a.warnEnglish(opts.Body, opts.AllowOtherLanguage)
	info, ok := resolveScopeCWD(opts.CWD)
	if !ok {
		return "", fmt.Errorf("not inside a git repository; team memory needs a repository")
	}
	if opts.WorktreeAuto {
		if opts.Project != "" && opts.Project != info.RepoSlug {
			return "", fmt.Errorf("--worktree auto resolved %q, not %q", info.RepoSlug, opts.Project)
		}
		opts.Project = info.RepoSlug
		opts.Worktree = info.WorktreeID
	}
	if opts.Project == "" {
		opts.Project = info.RepoSlug
	}
	if opts.Project != info.RepoSlug {
		return "", fmt.Errorf("team memory belongs to %q, not %q", info.RepoSlug, opts.Project)
	}
	if !validScopeName(opts.Project) {
		return "", fmt.Errorf("invalid project slug %q", opts.Project)
	}
	if opts.Worktree != "" && !validScopeName(opts.Worktree) {
		return "", fmt.Errorf("invalid worktree id %q", opts.Worktree)
	}

	entry := memory.Entry{
		Title:          opts.Title,
		Type:           scope,
		Date:           time.Now().Format("2006-01-02"),
		Body:           strings.TrimSpace(opts.Body),
		Source:         source,
		Native:         native,
		NativeType:     scope.NativeType(),
		Tier:           tier,
		Pinned:         opts.Pinned,
		Project:        opts.Project,
		Worktree:       opts.Worktree,
		Description:    opts.Description,
		Reason:         opts.Reason,
		Keywords:       opts.Tags,
		Category:       opts.Category,
		UsageScenarios: opts.UsageScenarios,
	}
	result, err := teammemory.Open(info.WorktreeRoot).Add(ctx, entry, teammemory.AddOptions{
		Force:       opts.Force,
		ProjectQwen: opts.Native == "qwen",
	})
	if err != nil {
		return "", err
	}
	switch {
	case result.Created:
		return "recorded (team): " + result.Entry.Path, nil
	case result.Appended:
		return "appended (team): " + result.Entry.Path, nil
	default:
		return "already recorded (team): " + result.Entry.Path, nil
	}
}

// teamEntries reads the team store of the repository that contains cwd.
func (a *App) teamEntries(ctx context.Context, cwd string) ([]memory.Entry, string, bool, error) {
	info, ok := resolveScopeCWD(cwd)
	if !ok {
		return nil, "", false, nil
	}
	entries, err := teammemory.Open(info.WorktreeRoot).Entries(ctx)
	if err != nil {
		return nil, info.RepoSlug, true, err
	}
	return entries, info.RepoSlug, true, nil
}

// teamSearch returns the line matches of the team store, marked [team] so a
// caller can tell them apart from the private corpus.
func (a *App) teamSearch(ctx context.Context, query string, limit int) ([]vault.Hit, error) {
	entries, _, ok, err := a.teamEntries(ctx, "")
	if err != nil || !ok {
		return nil, err
	}
	needle := strings.ToLower(strings.TrimSpace(query))
	if needle == "" {
		return nil, nil
	}
	var hits []vault.Hit
	for _, e := range entries {
		if strings.Contains(strings.ToLower(e.Title), needle) {
			hits = append(hits, vault.Hit{
				Path:  "[team] " + e.Path,
				Text:  e.Title,
				Title: e.Title,
			})
		}
		for i, line := range strings.Split(e.Body, "\n") {
			if strings.Contains(strings.ToLower(line), needle) {
				hits = append(hits, vault.Hit{
					Path:  "[team] " + e.Path,
					Line:  i + 1,
					Text:  strings.TrimRight(line, "\r"),
					Title: e.Title,
				})
			}
		}
		if limit > 0 && len(hits) >= limit {
			break
		}
	}
	if limit > 0 && len(hits) > limit {
		hits = hits[:limit]
	}
	return hits, nil
}
