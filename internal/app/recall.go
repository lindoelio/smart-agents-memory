package app

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/lindoelio/smart-agents-memory/internal/llm"
	"github.com/lindoelio/smart-agents-memory/internal/memory"
	"github.com/lindoelio/smart-agents-memory/internal/tree"
)

// MaxModelManifestBytes bounds the manifest sent to an LLM selector.
const MaxModelManifestBytes = 25000

// RecallOptions selects the structured recall mode.
type RecallOptions struct {
	// Tree returns the router, or the focused subtree when Categories is set.
	Tree bool
	// Categories focuses the subtree or filters the ranked query.
	Categories []string
	// Refs fetches exact entries by ref.
	Refs []string
	// Query ranks cold and team entries by relevance.
	Query string
	// Limit bounds the ranked entries.
	Limit int
	// MaxChars bounds the rendered result.
	MaxChars int
	// CWD is where the active scope is resolved.
	CWD string
	// LLM lets the configured model select the ranked refs.
	LLM bool
}

// RecallResult is the rendered recall and its tree revision.
type RecallResult struct {
	Text     string
	Revision string
	Refs     []string
}

type recallItem struct {
	entry memory.Entry
	team  bool
}

// Recall builds the snapshot and returns the mode requested by the caller.
func (a *App) Recall(ctx context.Context, opts RecallOptions) (RecallResult, error) {
	if opts.Limit <= 0 {
		opts.Limit = 5
	}
	if opts.MaxChars <= 0 {
		opts.MaxChars = 12000
	}
	items, err := a.recallItems(ctx, opts.CWD)
	if err != nil {
		return RecallResult{}, err
	}
	var owned, team []memory.Entry
	for _, item := range items {
		if item.team {
			team = append(team, item.entry)
		} else {
			owned = append(owned, item.entry)
		}
	}
	snapshot := tree.BuildSnapshot(owned, team)
	result := RecallResult{Revision: snapshot.Revision}

	switch {
	case len(opts.Refs) > 0:
		byRef := map[string]recallItem{}
		for _, item := range items {
			byRef[tree.Ref(item.entry, item.team)] = item
		}
		var selected []recallItem
		for _, ref := range opts.Refs {
			item, ok := byRef[strings.TrimSpace(ref)]
			if !ok {
				return result, fmt.Errorf("memory ref %q not found", ref)
			}
			selected = append(selected, item)
			result.Refs = append(result.Refs, ref)
		}
		result.Text = renderRecallItems(selected, opts.MaxChars)
		return result, nil
	case opts.Tree || (len(opts.Categories) > 0 && strings.TrimSpace(opts.Query) == ""):
		if len(opts.Categories) > 0 {
			result.Text = tree.RenderSubtree(snapshot, opts.Categories, opts.Query, opts.MaxChars)
		} else {
			result.Text = snapshot.RouterPrompt
		}
		return result, nil
	case strings.TrimSpace(opts.Query) != "":
		var ranked []recallItem
		if opts.LLM {
			ranked, err = a.selectWithLLM(ctx, filterRecallItems(items, opts.Categories), opts.Query, opts.Limit)
			if err != nil {
				return result, err
			}
		} else {
			ranked = rankRecall(items, opts.Query, opts.Categories, opts.CWD)
			if len(ranked) > opts.Limit {
				ranked = ranked[:opts.Limit]
			}
		}
		for _, item := range ranked {
			result.Refs = append(result.Refs, tree.Ref(item.entry, item.team))
		}
		result.Text = renderRecallItems(ranked, opts.MaxChars)
		return result, nil
	default:
		result.Text = snapshot.RouterPrompt
		return result, nil
	}
}

func (a *App) recallItems(ctx context.Context, cwd string) ([]recallItem, error) {
	var items []recallItem
	if a.Vault != nil && a.Vault.Ready(ctx) == nil {
		entries, err := a.Vault.Entries(ctx)
		if err != nil {
			return nil, err
		}
		for _, entry := range entries {
			items = append(items, recallItem{entry: entry})
		}
	}
	team, _, ok, err := a.teamEntries(ctx, cwd)
	if err != nil {
		return nil, err
	}
	if ok {
		for _, entry := range team {
			items = append(items, recallItem{entry: entry, team: true})
		}
	}
	return items, nil
}

func filterRecallItems(items []recallItem, categories []string) []recallItem {
	if len(categories) == 0 {
		return items
	}
	wanted := map[string]bool{}
	for _, category := range categories {
		wanted[category] = true
	}
	var out []recallItem
	for _, item := range items {
		if wanted[memory.InferCategory(item.entry)] {
			out = append(out, item)
		}
	}
	return out
}

type manifestItem struct {
	Ref            string   `json:"ref"`
	Title          string   `json:"title"`
	Description    string   `json:"description,omitempty"`
	Category       string   `json:"category"`
	Keywords       []string `json:"keywords,omitempty"`
	UsageScenarios []string `json:"usage_scenarios,omitempty"`
}

// selectWithLLM asks the configured model to choose refs from a bounded
// manifest. Every returned ref is validated against that manifest.
func (a *App) selectWithLLM(ctx context.Context, items []recallItem, query string, limit int) ([]recallItem, error) {
	if a.LLM == nil {
		return nil, fmt.Errorf("LLM recall is not configured")
	}
	if len(items) == 0 {
		return nil, nil
	}
	manifest := make([]manifestItem, 0, len(items))
	byRef := map[string]recallItem{}
	for _, item := range items {
		ref := tree.Ref(item.entry, item.team)
		byRef[ref] = item
		manifest = append(manifest, manifestItem{
			Ref: ref, Title: item.entry.Title, Description: item.entry.Description,
			Category: memory.InferCategory(item.entry), Keywords: item.entry.Keywords,
			UsageScenarios: item.entry.UsageScenarios,
		})
	}
	// Drop tail entries until the manifest fits the model budget.
	for len(manifest) > 0 {
		data, err := json.Marshal(manifest)
		if err != nil {
			return nil, err
		}
		if len(data) <= MaxModelManifestBytes {
			break
		}
		manifest = manifest[:len(manifest)-1]
	}
	if len(manifest) == 0 {
		return nil, fmt.Errorf("the recall manifest does not fit the model budget")
	}
	data, err := json.Marshal(manifest)
	if err != nil {
		return nil, err
	}
	var selected struct {
		Refs []string `json:"refs"`
	}
	err = a.LLM.CompleteJSON(ctx, llm.Request{
		SystemPrompt: "Select the memories relevant to the user's query. Return exactly {\"refs\":[\"...\"]} using only refs from the manifest. Do not invent a ref.",
		UserPrompt:   "Query: " + query + "\nManifest:\n" + string(data),
		Temperature:  0,
	}, &selected)
	if err != nil {
		return nil, err
	}
	var out []recallItem
	seen := map[string]bool{}
	for _, ref := range selected.Refs {
		if seen[ref] {
			continue
		}
		item, ok := byRef[ref]
		if !ok {
			return nil, fmt.Errorf("the model returned an unknown memory ref %q", ref)
		}
		seen[ref] = true
		out = append(out, item)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out, nil
}

func rankRecall(items []recallItem, query string, categories []string, cwd string) []recallItem {
	terms := strings.Fields(strings.ToLower(query))
	if len(terms) == 0 {
		return nil
	}
	activeProject, activeWorktree := "", ""
	if info, ok := resolveScopeCWD(cwd); ok {
		activeProject = info.RepoSlug
		activeWorktree = info.WorktreeID
	}
	categoryFilter := map[string]bool{}
	for _, category := range categories {
		categoryFilter[category] = true
	}
	type scored struct {
		item  recallItem
		score int
	}
	var ranked []scored
	for _, item := range items {
		entry := item.entry
		category := memory.InferCategory(entry)
		if len(categoryFilter) > 0 && !categoryFilter[category] {
			continue
		}
		score := 0
		for _, term := range terms {
			score += 5 * strings.Count(strings.ToLower(entry.Title), term)
			for _, keyword := range entry.Keywords {
				score += 4 * strings.Count(strings.ToLower(keyword), term)
			}
			score += 3 * strings.Count(strings.ToLower(category), term)
			for _, scenario := range entry.UsageScenarios {
				score += 3 * strings.Count(strings.ToLower(scenario), term)
			}
			score += 2 * strings.Count(strings.ToLower(entry.Description), term)
			score += strings.Count(strings.ToLower(entry.Body), term)
		}
		if activeProject != "" && entry.Project == activeProject {
			score += 2
		}
		if activeWorktree != "" && entry.Worktree == activeWorktree {
			score += 3
		}
		if score > 0 {
			ranked = append(ranked, scored{item: item, score: score})
		}
	}
	sort.SliceStable(ranked, func(i, j int) bool {
		if ranked[i].score != ranked[j].score {
			return ranked[i].score > ranked[j].score
		}
		return strings.ToLower(ranked[i].item.entry.Title) < strings.ToLower(ranked[j].item.entry.Title)
	})
	out := make([]recallItem, 0, len(ranked))
	for _, item := range ranked {
		out = append(out, item.item)
	}
	return out
}

func renderRecallItems(items []recallItem, maxChars int) string {
	if len(items) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("## Recall")
	for _, item := range items {
		ref := tree.Ref(item.entry, item.team)
		b.WriteString("\n\n### [" + ref + "] " + item.entry.Title + "\n\n")
		if item.entry.Description != "" {
			b.WriteString(item.entry.Description + "\n\n")
		}
		b.WriteString(strings.TrimSpace(item.entry.Body))
	}
	text := strings.TrimSpace(b.String())
	if len(text) > maxChars {
		text = text[:maxChars]
	}
	return text
}
