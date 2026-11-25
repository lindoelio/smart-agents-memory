// Package app holds the use cases of Smart Agents Memory.
//
// The use cases sit between the entrypoints and the gateways: the CLI and
// the future MCP server call the same functions, and the functions only
// know the vault port.
package app

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/lindoelio/smart-agents-memory/internal/config"
	"github.com/lindoelio/smart-agents-memory/internal/llm"
	"github.com/lindoelio/smart-agents-memory/internal/memory"
	"github.com/lindoelio/smart-agents-memory/internal/projection"
	"github.com/lindoelio/smart-agents-memory/internal/reconcile"
	"github.com/lindoelio/smart-agents-memory/internal/secretguard"
	"github.com/lindoelio/smart-agents-memory/internal/vault"
)

// App is the application of one run.
type App struct {
	// Cfg is the resolved configuration.
	Cfg vault.Config
	// Vault is the gateway holding the corpus.
	Vault vault.Vault
	// Projections is the local native store.
	Projections *projection.Store
	// LocalRoot is where the hot tier and the projections live.
	LocalRoot string
	// ConfigPath is the configuration file in use.
	ConfigPath string
	// Out and Err are where the output goes.
	Out io.Writer
	Err io.Writer
	// LLM is the optional JSON completion client. It is nil when the
	// deployment did not configure one.
	LLM llm.Client
}

// New builds the application for a configuration.
//
// The configuration already names the store. Choosing a default path is
// the configuration's job, so a use case never branches on a gateway name.
func New(cfg vault.Config, configPath string, out, errOut io.Writer) (*App, error) {
	store, err := vault.Open(cfg)
	if err != nil {
		return nil, err
	}
	root := config.LocalRoot(cfg)
	return &App{
		Cfg:         cfg,
		Vault:       store,
		Projections: projection.New(root),
		LocalRoot:   root,
		ConfigPath:  configPath,
		Out:         out,
		Err:         errOut,
	}, nil
}

// RequireStore returns an error when there is no store yet.
func (a *App) RequireStore(ctx context.Context) error {
	return a.Vault.Ready(ctx)
}

// Reconciler builds the reconciler of this application.
func (a *App) Reconciler() *reconcile.Reconciler {
	return &reconcile.Reconciler{Vault: a.Vault, Projections: a.Projections, Warn: a.Err}
}

// Path is the root of the store.
func (a *App) Path() string { return a.Vault.Root() }

// HotPath is the file with the hot tier.
func (a *App) HotPath() string { return filepath.Join(a.LocalRoot, "MEMORY.md") }

// HotContent reads the hot tier.
func (a *App) HotContent() string {
	data, err := os.ReadFile(a.HotPath())
	if err != nil {
		return ""
	}
	return string(data)
}

// Search looks for the query in the private store and in team memory.
func (a *App) Search(ctx context.Context, query string, limit int) ([]vault.Hit, error) {
	if strings.TrimSpace(query) == "" {
		return nil, fmt.Errorf("empty query")
	}
	var hits []vault.Hit
	if a.Vault != nil && a.Vault.Ready(ctx) == nil {
		private, err := a.Vault.Search(ctx, query, limit)
		if err != nil {
			return nil, err
		}
		hits = append(hits, private...)
	}
	team, err := a.teamSearch(ctx, query, limit)
	if err != nil {
		return nil, err
	}
	hits = append(hits, team...)
	if limit > 0 && len(hits) > limit {
		hits = hits[:limit]
	}
	return hits, nil
}

// BriefOptions selects the context package.
type BriefOptions struct {
	// Project is the repository or project slug.
	Project string
	// Worktree is the linked worktree identifier.
	Worktree string
	// Global disables repository auto-detection.
	Global bool
	// ProjectExplicit reports whether --project was passed by the user.
	ProjectExplicit bool
	// CWD is where the scope is resolved; empty means the process directory.
	CWD string
}

// Brief builds the context package: the hot tier, the index of the corpus,
// the project scope and the worktree scope. Cold bodies only enter the text
// for the entries explicitly scoped to the active project or worktree.
func (a *App) Brief(ctx context.Context, opts BriefOptions) (string, error) {
	project := strings.TrimSpace(opts.Project)
	worktree := strings.TrimSpace(opts.Worktree)
	explicit := opts.ProjectExplicit || project != ""

	if project == "" && !opts.Global {
		if info, ok := resolveScopeCWD(opts.CWD); ok {
			project = info.RepoSlug
			if worktree == "" {
				worktree = info.WorktreeID
			}
		}
	}
	if project != "" && !validScopeName(project) {
		return "", fmt.Errorf("invalid project slug %q", project)
	}
	if worktree != "" && !validScopeName(worktree) {
		return "", fmt.Errorf("invalid worktree id %q", worktree)
	}
	if worktree != "" && project == "" {
		return "", fmt.Errorf("a worktree needs a project; pass --project or run inside a git repository")
	}

	var parts []string
	if hot := stripFrontmatter(a.HotContent()); strings.TrimSpace(hot) != "" {
		parts = append(parts, strings.TrimRight(hot, "\n"))
	}
	vaultReady := a.Vault != nil && a.Vault.Ready(ctx) == nil
	var corpus []memory.Entry
	if vaultReady {
		var err error
		corpus, err = a.Vault.Entries(ctx)
		if err != nil {
			return "", err
		}
		if index := renderBriefIndex(corpus); index != "" {
			parts = append(parts, index)
		}
	}

	if vaultReady && project != "" {
		holder, ok := a.Vault.(vault.ProjectNotes)
		if !ok {
			return "", fmt.Errorf("the %s gateway does not keep project notes", a.Vault.Kind())
		}
		notes, found, err := holder.ProjectNote(ctx, project)
		if err != nil {
			return "", err
		}
		entries := scopedEntries(corpus, project, "")
		if found || len(entries) > 0 {
			section := "\n## Project: " + project
			if found && strings.TrimSpace(notes) != "" {
				section += "\n\n" + strings.TrimSpace(notes)
			}
			section += renderScopedEntries(entries)
			parts = append(parts, section)
		} else if explicit {
			fmt.Fprintf(a.Err, "project '%s' not found\n", project)
		}
	}

	if vaultReady && worktree != "" {
		holder, ok := a.Vault.(vault.WorktreeNotes)
		if !ok {
			return "", fmt.Errorf("the %s gateway does not keep worktree notes", a.Vault.Kind())
		}
		notes, found, err := holder.WorktreeNote(ctx, project, worktree)
		if err != nil {
			return "", err
		}
		entries := scopedEntries(corpus, project, worktree)
		if found || len(entries) > 0 {
			section := "\n## Worktree: " + project + "@" + worktree
			if found && strings.TrimSpace(notes) != "" {
				section += "\n\n" + strings.TrimSpace(notes)
			}
			section += renderScopedEntries(entries)
			parts = append(parts, section)
		}
	}

	team, info, ok, err := a.teamEntries(ctx, opts.CWD)
	if err != nil {
		return "", err
	}
	if ok && len(team) > 0 {
		parts = append(parts, "\n## Team Memory ("+info+")"+renderEntries(team))
	}

	return strings.Join(parts, "\n"), nil
}

// mergeUnique appends values that are not already present.
func mergeUnique(existing, added []string) []string {
	seen := map[string]bool{}
	for _, value := range existing {
		seen[value] = true
	}
	out := append([]string(nil), existing...)
	for _, value := range added {
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}

// scopedEntries returns the cold entries of one project or worktree.
func scopedEntries(corpus []memory.Entry, project, worktree string) []memory.Entry {
	var out []memory.Entry
	for _, e := range corpus {
		if e.EffectiveTier() == memory.TierHot {
			continue
		}
		if e.Project != project || e.Worktree != worktree {
			continue
		}
		out = append(out, e)
	}
	return out
}

// renderScopedEntries writes the title and body of each scoped entry.
func renderScopedEntries(entries []memory.Entry) string {
	var b strings.Builder
	for _, e := range entries {
		b.WriteString("\n### " + e.Title + "\n\n")
		b.WriteString(strings.TrimSpace(e.Body))
		b.WriteString("\n")
	}
	return b.String()
}

// renderEntries writes the title and body of each team entry.
func renderEntries(entries []memory.Entry) string {
	var b strings.Builder
	for _, e := range entries {
		b.WriteString("\n### " + e.Title + "\n\n")
		b.WriteString(strings.TrimSpace(e.Body))
		b.WriteString("\n")
	}
	return b.String()
}

// renderBriefIndex lists the corpus without the bodies. The hot tier is
// already in the context; the rest is a line each.
func renderBriefIndex(corpus []memory.Entry) string {
	var b strings.Builder
	b.WriteString("\n## Index\n")
	any := false
	for _, typ := range memory.Types {
		var lines []string
		for _, e := range corpus {
			if e.Type != typ {
				continue
			}
			line := "- " + e.Title
			if e.Description != "" {
				line += " — " + e.Description
			}
			if e.EffectiveTier() == memory.TierHot {
				line += " (hot)"
			}
			lines = append(lines, line)
		}
		if len(lines) == 0 {
			continue
		}
		any = true
		b.WriteString("\n### " + string(typ) + "\n\n")
		b.WriteString(strings.Join(lines, "\n"))
		b.WriteString("\n")
	}
	if !any {
		return ""
	}
	return strings.TrimRight(b.String(), "\n")
}

// warnEnglish keeps the corpus in English by default. It only warns; the
// write is never blocked.
func (a *App) warnEnglish(body string, allow bool) {
	if allow || memory.IsEnglish(body) {
		return
	}
	fmt.Fprintln(a.Err,
		"warning: the corpus is written in English by default. "+
			"Pass --allow-other-language if this entry must keep another language.")
}

// AddOptions are the options of a write.
type AddOptions struct {
	// Scope is the type of the entry.
	Scope string
	// Title is the identity of the entry.
	Title string
	// Body is the content.
	Body string
	// Source is where the entry came from.
	Source string
	// Native is the projection to write into.
	Native string
	// Tier is hot for what is always true, cold for the rest.
	Tier string
	// Pinned protects the entry from automated edits and imports.
	Pinned bool
	// Project is the repository or project slug for the entry.
	Project string
	// Worktree is the linked worktree identifier for the entry.
	Worktree string
	// WorktreeAuto resolves the worktree from CWD.
	WorktreeAuto bool
	// CWD is where the scope is resolved; empty means the process directory.
	CWD string
	// Team writes to the Git-backed team store instead of the private vault.
	Team bool
	// Category is the semantic taxonomy bucket.
	Category string
	// UsageScenarios are the situations in which the entry applies.
	UsageScenarios []string
	// Description is the one line a runtime injects for a cold entry.
	Description string
	// Reason is why an entry was appended to the profile.
	Reason string
	// Tags are keywords.
	Tags []string
	// Force writes even when the title already exists.
	Force bool
	// AllowOtherLanguage skips the English-default warning.
	AllowOtherLanguage bool
}

// Add records one durable memory.
func (a *App) Add(ctx context.Context, opts AddOptions) (string, error) {
	title := strings.TrimSpace(opts.Title)
	if title == "" {
		return "", fmt.Errorf("empty title")
	}
	opts.Title = title
	scope := memory.Type(opts.Scope)
	if scope == "" {
		scope = memory.TypeLesson
	}
	if !scope.Valid() {
		return "", fmt.Errorf("invalid scope %q (use %s)", scope, typeList())
	}
	if opts.Category != "" && !memory.ValidCategory(opts.Category) {
		return "", fmt.Errorf("invalid category %q", opts.Category)
	}
	body := strings.TrimSpace(opts.Body)
	if body == "" {
		return "", fmt.Errorf("empty body")
	}
	if err := secretguard.Check(opts.Title, body); err != nil {
		return "", err
	}
	source := opts.Source
	if source == "" {
		source = os.Getenv(config.EnvSource)
	}
	if source == "" {
		source = memory.DefaultSource
	}
	native := opts.Native
	if native == "" {
		native = scope.NativeTarget()
	}
	if native != "minimax-user" && native != "minimax-agent" && native != "qwen" {
		return "", fmt.Errorf("invalid native projection %q (use minimax-user, minimax-agent, qwen)", native)
	}
	tier := memory.Tier(opts.Tier)
	if tier == "" {
		tier = memory.TierCold
	}
	if tier != memory.TierHot && tier != memory.TierCold {
		return "", fmt.Errorf("invalid tier %q (use hot, cold)", opts.Tier)
	}
	if opts.Team {
		return a.addTeam(ctx, opts, scope, source, native, tier)
	}
	if err := a.resolveAddScope(&opts, scope); err != nil {
		return "", err
	}

	// The corpus is written in English by default. A body in another
	// language is only right when the user asked for it, or when the entry
	// quotes something that has to stay verbatim.
	a.warnEnglish(body, opts.AllowOtherLanguage)

	corpus, err := a.Vault.Entries(ctx)
	if err != nil {
		return "", err
	}
	for _, e := range corpus {
		if memory.NormTitle(e.Title) != memory.NormTitle(opts.Title) {
			continue
		}
		if e.Pinned && !opts.Force {
			return "", fmt.Errorf("entry %q is pinned; pass --force to modify it", e.Title)
		}
		wasPinned := e.Pinned
		if strings.Contains(e.Body, body) && !opts.Force {
			if opts.Pinned && !e.Pinned {
				e.Pinned = true
				if err := a.Vault.Update(ctx, e); err != nil {
					return "", err
				}
				a.bumpDirty()
				if err := a.Vault.AppendLog(ctx, fmt.Sprintf("add | %s | %s | %s (pinned)", scope, e.Title, source)); err != nil {
					return "", err
				}
				if _, err := a.Index(ctx); err != nil {
					return "", err
				}
				return "pinned: " + e.Path, nil
			}
			return "already recorded (no change): " + e.Title, nil
		}
		e.Body = strings.TrimRight(e.Body, "\n") + "\n\n" + body
		if opts.Description != "" {
			e.Description = opts.Description
		}
		if opts.Reason != "" {
			e.Reason = opts.Reason
		}
		if opts.Tier != "" {
			e.Tier = tier
		}
		if opts.Category != "" {
			e.Category = opts.Category
		}
		if len(opts.UsageScenarios) > 0 {
			e.UsageScenarios = mergeUnique(e.UsageScenarios, opts.UsageScenarios)
		}
		// A pinned entry stays pinned unless an explicit command unpins
		// it. --pinned can add protection, never remove it.
		if opts.Pinned {
			e.Pinned = true
		}
		if opts.Project != "" && e.Project == "" {
			e.Project = opts.Project
		}
		if opts.Worktree != "" && e.Worktree == "" {
			e.Worktree = opts.Worktree
		}
		if err := a.Vault.Update(ctx, e); err != nil {
			return "", err
		}
		a.bumpDirty()
		suffix := " (append)"
		if wasPinned {
			suffix = " (append, forced on pinned)"
		}
		if err := a.Vault.AppendLog(ctx, fmt.Sprintf("add | %s | %s | %s%s", scope, e.Title, source, suffix)); err != nil {
			return "", err
		}
		if _, err := a.Index(ctx); err != nil {
			return "", err
		}
		return "appended to: " + e.Path, nil
	}

	entry := memory.Entry{
		Title:          opts.Title,
		Type:           scope,
		Date:           time.Now().Format("2006-01-02"),
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
		Body:           body,
	}
	stored, err := a.Vault.Create(ctx, entry)
	if err != nil {
		return "", err
	}
	a.bumpDirty()
	if err := a.Vault.AppendLog(ctx, fmt.Sprintf("add | %s | %s | %s", scope, stored.Title, source)); err != nil {
		return "", err
	}
	if _, err := a.Index(ctx); err != nil {
		return "", err
	}
	return "recorded: " + stored.Path, nil
}

// SetPinned protects or releases one entry. The corpus owns the state; the
// projections follow it on the next index.
func (a *App) SetPinned(ctx context.Context, title string, pinned bool) (string, error) {
	title = strings.TrimSpace(title)
	if title == "" {
		return "", fmt.Errorf("empty title")
	}
	corpus, err := a.Vault.Entries(ctx)
	if err != nil {
		return "", err
	}
	key := memory.NormTitle(title)
	var matches []memory.Entry
	for _, e := range corpus {
		if memory.NormTitle(e.Title) == key {
			matches = append(matches, e)
		}
	}
	if len(matches) == 0 {
		return "", fmt.Errorf("entry %q not found", title)
	}
	if len(matches) > 1 {
		return "", fmt.Errorf("more than one entry is titled %q", title)
	}
	entry := matches[0]
	if !pinned && hasPinnedPath(entry.Path) {
		return "", fmt.Errorf("entry %q is pinned by its path; move it out of pinned/ before unpinning", entry.Title)
	}
	state := "pinned"
	if !pinned {
		state = "unpinned"
	}
	if entry.Pinned == pinned {
		return "already " + state + ": " + entry.Path, nil
	}
	entry.Pinned = pinned
	if err := a.Vault.Update(ctx, entry); err != nil {
		return "", err
	}
	action := "pin"
	if !pinned {
		action = "unpin"
	}
	if err := a.Vault.AppendLog(ctx, fmt.Sprintf("%s | %s | %s", action, entry.Type, entry.Title)); err != nil {
		return "", err
	}
	if _, err := a.Index(ctx); err != nil {
		return "", err
	}
	return state + ": " + entry.Path, nil
}

// hasPinnedPath reports whether a corpus path is physically under a pinned
// directory. Such a note is pinned by location, so unpinning it requires
// moving it first.
func hasPinnedPath(path string) bool {
	for _, segment := range strings.Split(filepath.ToSlash(path), "/") {
		if strings.EqualFold(segment, "pinned") {
			return true
		}
	}
	return false
}

// Log returns the write log, newest first.
func (a *App) Log(ctx context.Context, limit int) ([]string, error) {
	return a.Vault.LogLines(ctx, limit)
}

// stripFrontmatter removes the YAML block from the top of a note.
func stripFrontmatter(text string) string {
	if !strings.HasPrefix(text, "---\n") {
		return text
	}
	end := strings.Index(text[4:], "\n---\n")
	if end < 0 {
		return text
	}
	return text[4+end+5:]
}

// typeList renders the valid types for an error message.
func typeList() string {
	parts := make([]string, 0, len(memory.Types))
	for _, t := range memory.Types {
		parts = append(parts, string(t))
	}
	return strings.Join(parts, ", ")
}
