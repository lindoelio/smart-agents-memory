// Package tree builds the deterministic, budgeted memory tree used by
// structured recall. It is a metadata view of the corpus, not another store.
package tree

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/lindoelio/smart-agents-memory/internal/memory"
)

const (
	// RouterCharBudget bounds the global router prompt.
	RouterCharBudget = 6000
	// RouterTruncationReserve leaves room for the truncation notice.
	RouterTruncationReserve = 96
	// CategoryKeywordLimit bounds the keywords shown for one category.
	CategoryKeywordLimit = 12
)

// Leaf is one memory in the tree.
type Leaf struct {
	Ref            string   `json:"ref"`
	Scope          string   `json:"scope"`
	Category       string   `json:"category"`
	Title          string   `json:"title"`
	Description    string   `json:"description"`
	Keywords       []string `json:"keywords"`
	UsageScenarios []string `json:"usage_scenarios,omitempty"`
	Pinned         bool     `json:"pinned,omitempty"`
}

// CategoryNode groups leaves under one semantic category.
type CategoryNode struct {
	Category           string   `json:"category"`
	Total              int      `json:"total"`
	Leaves             []Leaf   `json:"leaves"`
	Keywords           []string `json:"keywords"`
	HiddenKeywordCount int      `json:"hidden_keyword_count,omitempty"`
}

// Snapshot is a deterministic revision of the memory tree.
type Snapshot struct {
	Revision     string         `json:"revision"`
	Categories   []CategoryNode `json:"categories"`
	RouterPrompt string         `json:"router_prompt"`
}

// BuildSnapshot builds the tree from the private corpus and the team store.
func BuildSnapshot(entries, teamEntries []memory.Entry) Snapshot {
	groups := map[string][]Leaf{}
	for _, e := range entries {
		leaf := leafOf(e, string(e.Type))
		groups[leaf.Category] = append(groups[leaf.Category], leaf)
	}
	for _, e := range teamEntries {
		leaf := leafOf(e, "team")
		groups[leaf.Category] = append(groups[leaf.Category], leaf)
	}

	order := append([]string(nil), memory.TreeCategories...)
	order = append(order, memory.Uncategorized)
	var categories []CategoryNode
	for _, category := range order {
		leaves := groups[category]
		if len(leaves) == 0 {
			continue
		}
		sort.SliceStable(leaves, func(i, j int) bool { return leaves[i].Ref < leaves[j].Ref })
		keywords, hidden := aggregateKeywords(leaves)
		categories = append(categories, CategoryNode{
			Category:           category,
			Total:              len(leaves),
			Leaves:             leaves,
			Keywords:           keywords,
			HiddenKeywordCount: hidden,
		})
	}
	snapshot := Snapshot{
		Revision:   revisionOf(categories),
		Categories: categories,
	}
	snapshot.RouterPrompt = RenderRouter(categories)
	return snapshot
}

func leafOf(e memory.Entry, scope string) Leaf {
	category := memory.InferCategory(e)
	return Leaf{
		Ref:            Ref(e, scope == "team"),
		Scope:          scope,
		Category:       category,
		Title:          e.Title,
		Description:    e.Description,
		Keywords:       append([]string(nil), e.Keywords...),
		UsageScenarios: append([]string(nil), e.UsageScenarios...),
		Pinned:         e.Pinned,
	}
}

// Ref is the stable address of an entry in recall.
func Ref(e memory.Entry, team bool) string {
	if team {
		path := filepath.ToSlash(strings.TrimSuffix(e.Path, ".md"))
		path = strings.TrimPrefix(path, ".agents/memory/")
		path = strings.TrimPrefix(path, ".qwen/team-memory/")
		if e.Path != "" {
			return "team:" + path
		}
		return "team:" + memory.Slugify(e.Title)
	}
	return string(e.Type) + ":" + memory.Slugify(e.Title)
}

func revisionOf(categories []CategoryNode) string {
	var leaves []Leaf
	for _, category := range categories {
		leaves = append(leaves, category.Leaves...)
	}
	sort.SliceStable(leaves, func(i, j int) bool { return leaves[i].Ref < leaves[j].Ref })
	data, err := json.Marshal(leaves)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func aggregateKeywords(leaves []Leaf) ([]string, int) {
	type stat struct {
		value string
		count int
		first int
	}
	stats := map[string]stat{}
	first := 0
	for _, leaf := range leaves {
		seen := map[string]bool{}
		for _, keyword := range leaf.Keywords {
			key := strings.ToLower(keyword)
			if seen[key] {
				continue
			}
			seen[key] = true
			if existing, ok := stats[key]; ok {
				existing.count++
				stats[key] = existing
				continue
			}
			stats[key] = stat{value: keyword, count: 1, first: first}
			first++
		}
	}
	values := make([]stat, 0, len(stats))
	for _, item := range stats {
		values = append(values, item)
	}
	sort.SliceStable(values, func(i, j int) bool {
		if values[i].count != values[j].count {
			return values[i].count > values[j].count
		}
		return values[i].first < values[j].first
	})
	limit := CategoryKeywordLimit
	if len(values) < limit {
		limit = len(values)
	}
	out := make([]string, 0, limit)
	for _, item := range values[:limit] {
		out = append(out, item.value)
	}
	return out, len(values) - limit
}

// RenderRouter renders a compact tree within RouterCharBudget.
func RenderRouter(categories []CategoryNode) string {
	lines := []string{
		"## Memory tree",
		"",
		"Use a ref with recall --ref, or a category with recall --category.",
		"",
	}
	if len(categories) == 0 {
		lines = append(lines, "No memories are available.")
		return strings.Join(lines, "\n")
	}
	budget := RouterCharBudget - RouterTruncationReserve
	size := len(strings.Join(lines, "\n"))
	appendLine := func(line string) bool {
		next := size + 1 + len(line)
		if next > budget {
			return false
		}
		lines = append(lines, line)
		size = next
		return true
	}
	truncated := false
	for _, category := range categories {
		header := category.Category
		if category.Total > 0 {
			header += fmt.Sprintf(" (%d)", category.Total)
		}
		if !appendLine(header) {
			truncated = true
			break
		}
		if len(category.Keywords) > 0 {
			if !appendLine("  keywords: " + strings.Join(category.Keywords, ", ")) {
				truncated = true
				break
			}
		}
		for _, leaf := range category.Leaves {
			if !appendLine("  - [" + leaf.Ref + "] " + leaf.Title) {
				truncated = true
				break
			}
		}
		if truncated || !appendLine("") {
			truncated = true
			break
		}
	}
	if truncated {
		lines = append(lines, "  … more memories not shown; use recall --query.")
	}
	text := strings.TrimRight(strings.Join(lines, "\n"), "\n")
	if len(text) > RouterCharBudget {
		text = text[:RouterCharBudget]
	}
	return text
}

// RenderSubtree renders a focused set of categories, optionally filtered by
// a query. It includes descriptions and usage scenarios.
func RenderSubtree(snap Snapshot, categories []string, query string, maxChars int) string {
	if maxChars <= 0 {
		maxChars = RouterCharBudget
	}
	wanted := map[string]bool{}
	for _, category := range categories {
		wanted[category] = true
	}
	needle := strings.ToLower(strings.TrimSpace(query))
	var lines []string
	lines = append(lines, "## Memory focus", "")
	for _, category := range snap.Categories {
		if len(wanted) > 0 && !wanted[category.Category] {
			continue
		}
		var selected []Leaf
		for _, leaf := range category.Leaves {
			if needle == "" || leafMatches(leaf, needle) {
				selected = append(selected, leaf)
			}
		}
		if len(selected) == 0 {
			continue
		}
		lines = append(lines, category.Category)
		for _, leaf := range selected {
			lines = append(lines, "  - ["+leaf.Ref+"] "+leaf.Title)
			if leaf.Description != "" {
				lines = append(lines, "    "+leaf.Description)
			}
			if len(leaf.UsageScenarios) > 0 {
				lines = append(lines, "    when: "+strings.Join(leaf.UsageScenarios, "; "))
			}
		}
		lines = append(lines, "")
	}
	text := strings.TrimRight(strings.Join(lines, "\n"), "\n")
	if text == "## Memory focus" {
		return ""
	}
	if len(text) > maxChars {
		text = text[:maxChars]
	}
	return text
}

func leafMatches(leaf Leaf, needle string) bool {
	fields := []string{leaf.Title, leaf.Description, leaf.Category}
	fields = append(fields, leaf.Keywords...)
	fields = append(fields, leaf.UsageScenarios...)
	for _, field := range fields {
		if strings.Contains(strings.ToLower(field), needle) {
			return true
		}
	}
	return false
}
