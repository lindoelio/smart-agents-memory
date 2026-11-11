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
	"github.com/lindoelio/smart-agents-memory/internal/memory"
	"github.com/lindoelio/smart-agents-memory/internal/projection"
	"github.com/lindoelio/smart-agents-memory/internal/reconcile"
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
	return &reconcile.Reconciler{Vault: a.Vault, Projections: a.Projections}
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

// Search looks for the query in the store.
func (a *App) Search(ctx context.Context, query string, limit int) ([]vault.Hit, error) {
	if strings.TrimSpace(query) == "" {
		return nil, fmt.Errorf("empty query")
	}
	return a.Vault.Search(ctx, query, limit)
}

// Brief builds the context package: the hot tier, the index of the corpus,
// and the notes of a project when asked. A query is searched by the command,
// so the bodies of cold entries stay out of this text.
func (a *App) Brief(ctx context.Context, project, _ string, _ int) (string, error) {
	var parts []string
	if hot := stripFrontmatter(a.HotContent()); strings.TrimSpace(hot) != "" {
		parts = append(parts, strings.TrimRight(hot, "\n"))
	}
	corpus, err := a.Vault.Entries(ctx)
	if err != nil {
		return "", err
	}
	if index := renderBriefIndex(corpus); index != "" {
		parts = append(parts, index)
	}
	if project != "" {
		holder, ok := a.Vault.(vault.ProjectNotes)
		if !ok {
			return "", fmt.Errorf("the %s gateway does not keep project notes", a.Vault.Kind())
		}
		notes, found, err := holder.ProjectNote(ctx, project)
		if err != nil {
			return "", err
		}
		if found {
			parts = append(parts, fmt.Sprintf("\n## Project: %s\n\n%s", project, strings.TrimSpace(notes)))
		} else {
			fmt.Fprintf(a.Err, "project '%s' not found\n", project)
		}
	}
	return strings.Join(parts, "\n"), nil
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
	scope := memory.Type(opts.Scope)
	if scope == "" {
		scope = memory.TypeLesson
	}
	if !scope.Valid() {
		return "", fmt.Errorf("invalid scope %q (use %s)", scope, typeList())
	}
	body := strings.TrimSpace(opts.Body)
	if body == "" {
		return "", fmt.Errorf("empty body")
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

	// The corpus is written in English by default. A body in another
	// language is only right when the user asked for it, or when the entry
	// quotes something that has to stay verbatim.
	if !opts.AllowOtherLanguage && !memory.IsEnglish(body) {
		fmt.Fprintln(a.Err,
			"warning: the corpus is written in English by default. "+
				"Pass --allow-other-language if this entry must keep another language.")
	}

	corpus, err := a.Vault.Entries(ctx)
	if err != nil {
		return "", err
	}
	for _, e := range corpus {
		if memory.NormTitle(e.Title) != memory.NormTitle(opts.Title) {
			continue
		}
		if strings.Contains(e.Body, body) && !opts.Force {
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
		if err := a.Vault.Update(ctx, e); err != nil {
			return "", err
		}
		if err := a.Vault.AppendLog(ctx, fmt.Sprintf("add | %s | %s | %s (append)", scope, e.Title, source)); err != nil {
			return "", err
		}
		if _, err := a.Index(ctx); err != nil {
			return "", err
		}
		return "appended to: " + e.Path, nil
	}

	entry := memory.Entry{
		Title:       opts.Title,
		Type:        scope,
		Date:        time.Now().Format("2006-01-02"),
		Source:      source,
		Native:      native,
		NativeType:  scope.NativeType(),
		Tier:        tier,
		Description: opts.Description,
		Reason:      opts.Reason,
		Keywords:    opts.Tags,
		Body:        body,
	}
	stored, err := a.Vault.Create(ctx, entry)
	if err != nil {
		return "", err
	}
	if err := a.Vault.AppendLog(ctx, fmt.Sprintf("add | %s | %s | %s", scope, stored.Title, source)); err != nil {
		return "", err
	}
	if _, err := a.Index(ctx); err != nil {
		return "", err
	}
	return "recorded: " + stored.Path, nil
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
