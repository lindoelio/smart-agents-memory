package tree

import (
	"strings"
	"testing"

	"github.com/lindoelio/smart-agents-memory/internal/memory"
)

func TestBuildSnapshotGroupsAndHashes(t *testing.T) {
	entries := []memory.Entry{
		{Title: "A testing lesson", Type: memory.TypeLesson, Description: "A lesson", Keywords: []string{"test", "flaky"}, Category: "testing_standard"},
		{Title: "A project decision", Type: memory.TypeProject, Description: "A decision", Category: "important_decision"},
		{Title: "A user preference", Type: memory.TypeUser, Description: "A preference"},
	}
	team := []memory.Entry{{Title: "A shared rule", Type: memory.TypeLesson, Description: "Shared"}}
	snap := BuildSnapshot(entries, team)
	if snap.Revision == "" {
		t.Fatal("the revision is empty")
	}
	if got := BuildSnapshot(entries, team).Revision; got != snap.Revision {
		t.Fatalf("the revision is not deterministic: %q != %q", got, snap.Revision)
	}
	foundTeam := false
	for _, category := range snap.Categories {
		for _, leaf := range category.Leaves {
			if leaf.Scope == "team" && leaf.Title == "A shared rule" {
				foundTeam = true
			}
		}
	}
	if !foundTeam {
		t.Fatal("the team leaf is missing")
	}
}

func TestRouterStaysWithinTheBudget(t *testing.T) {
	var entries []memory.Entry
	for i := 0; i < 400; i++ {
		entries = append(entries, memory.Entry{
			Title: "A very long title for memory number " + strings.Repeat("x", 20), Type: memory.TypeLesson,
			Description: "A description", Keywords: []string{"keyword"},
		})
	}
	snap := BuildSnapshot(entries, nil)
	if len(snap.RouterPrompt) > RouterCharBudget {
		t.Fatalf("the router is %d chars, budget %d", len(snap.RouterPrompt), RouterCharBudget)
	}
	if !strings.Contains(snap.RouterPrompt, "more memories not shown") {
		t.Fatalf("the router has no truncation notice:\n%s", snap.RouterPrompt)
	}
}

func TestCategoryKeywordsAreLimited(t *testing.T) {
	var keywords []string
	for i := 0; i < 20; i++ {
		keywords = append(keywords, string(rune('a'+i)))
	}
	snap := BuildSnapshot([]memory.Entry{{
		Title: "A lesson", Type: memory.TypeLesson, Category: "practice_standard", Keywords: keywords,
	}}, nil)
	if len(snap.Categories) != 1 {
		t.Fatalf("the categories are %+v", snap.Categories)
	}
	if len(snap.Categories[0].Keywords) != CategoryKeywordLimit {
		t.Fatalf("the keywords are %v", snap.Categories[0].Keywords)
	}
	if snap.Categories[0].HiddenKeywordCount != 8 {
		t.Fatalf("the hidden count is %d", snap.Categories[0].HiddenKeywordCount)
	}
}

func TestRenderSubtreeFiltersAndShowsScenarios(t *testing.T) {
	snap := BuildSnapshot([]memory.Entry{{
		Title: "Flaky test", Type: memory.TypeLesson, Description: "A flaky test",
		Category: "testing_standard", UsageScenarios: []string{"before a release"},
	}}, nil)
	text := RenderSubtree(snap, []string{"testing_standard"}, "flaky", 2000)
	if !strings.Contains(text, "Flaky test") || !strings.Contains(text, "before a release") {
		t.Fatalf("the subtree is %q", text)
	}
	if got := RenderSubtree(snap, []string{"hobbies"}, "", 2000); got != "" {
		t.Fatalf("an unrelated category rendered %q", got)
	}
}
