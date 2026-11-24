package app_test

import (
	"context"
	"strings"
	"testing"

	"github.com/lindoelio/smart-agents-memory/internal/app"
)

func TestRecallTreeAndCategory(t *testing.T) {
	h := newHarness(t, "obsidian")
	ctx := context.Background()
	if _, err := h.app.Add(ctx, app.AddOptions{
		Scope: "lesson", Title: "A flaky test", Body: "The flaky test stops the release.",
		Category: "testing_standard", Tags: []string{"test", "flaky"},
	}); err != nil {
		t.Fatal(err)
	}
	result, err := h.app.Recall(ctx, app.RecallOptions{Tree: true})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Text, "testing_standard") || !strings.Contains(result.Text, "lesson:a-flaky-test") {
		t.Fatalf("the tree is %q", result.Text)
	}
	if result.Revision == "" {
		t.Fatal("the revision is empty")
	}
	subtree, err := h.app.Recall(ctx, app.RecallOptions{Categories: []string{"testing_standard"}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(subtree.Text, "A flaky test") {
		t.Fatalf("the subtree is %q", subtree.Text)
	}
}

func TestRecallByRefAndQuery(t *testing.T) {
	h := newHarness(t, "obsidian")
	ctx := context.Background()
	if _, err := h.app.Add(ctx, app.AddOptions{
		Scope: "lesson", Title: "A flaky test", Body: "The flaky test stops the release.",
		Category: "testing_standard", Tags: []string{"test", "flaky"},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := h.app.Add(ctx, app.AddOptions{
		Scope: "lesson", Title: "A build cache", Body: "The build cache is local.",
		Category: "build_configuration",
	}); err != nil {
		t.Fatal(err)
	}

	byRef, err := h.app.Recall(ctx, app.RecallOptions{Refs: []string{"lesson:a-flaky-test"}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(byRef.Text, "The flaky test stops the release.") {
		t.Fatalf("the ref result is %q", byRef.Text)
	}

	ranked, err := h.app.Recall(ctx, app.RecallOptions{Query: "flaky", Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(ranked.Text, "A flaky test") || strings.Contains(ranked.Text, "A build cache") {
		t.Fatalf("the ranking is %q", ranked.Text)
	}
	if len(ranked.Refs) != 1 || ranked.Refs[0] != "lesson:a-flaky-test" {
		t.Fatalf("the ranked refs are %v", ranked.Refs)
	}
}

func TestRecallIncludesTeamMemoryWithoutAPrivateStore(t *testing.T) {
	h := newHarness(t, "obsidian")
	ctx := context.Background()
	repo := newRepo(t)
	if _, err := h.app.Add(ctx, app.AddOptions{
		Scope: "lesson", Title: "A shared rule", Body: "The shared rule.",
		Category: "practice_standard", Team: true, CWD: repo,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := h.app.Add(ctx, app.AddOptions{
		Scope: "lesson", Title: "A private rule", Body: "The private rule.",
		Category: "practice_standard",
	}); err != nil {
		t.Fatal(err)
	}
	result, err := h.app.Recall(ctx, app.RecallOptions{Query: "shared", CWD: repo})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Text, "A shared rule") || strings.Contains(result.Text, "A private rule") {
		t.Fatalf("the team recall is %q", result.Text)
	}
	if len(result.Refs) != 1 || !strings.HasPrefix(result.Refs[0], "team:") {
		t.Fatalf("the team ref is %v", result.Refs)
	}
}
