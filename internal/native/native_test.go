package native

import (
	"strings"
	"testing"

	"github.com/lindoelio/smart-agents-memory/internal/memory"
)

func TestTopicRoundTripKeepsSpacesAndDropsTheMarker(t *testing.T) {
	entry := memory.Entry{
		Title:       "A durable lesson",
		Type:        memory.TypeLesson,
		Body:        "The rule. Why it matters.",
		Description: "Kept as words",
		Native:      string(TargetMiniMaxAgent),
	}
	rendered := RenderTopic(entry)
	if strings.Contains(rendered, "A+durable") || strings.Contains(rendered, "Kept+as") {
		t.Fatalf("the description was escaped:\n%s", rendered)
	}
	parsed, ok := ParseTopic(rendered, "a-durable-lesson.md")
	if !ok {
		t.Fatal("the topic does not parse")
	}
	if parsed.Title != entry.Title {
		t.Fatalf("the title is %q", parsed.Title)
	}
	if parsed.Description != entry.Description {
		t.Fatalf("the description is %q", parsed.Description)
	}
	if parsed.Body != entry.Body {
		t.Fatalf("the body is %q", parsed.Body)
	}
	if strings.Contains(parsed.Body, "smart-agents-memory:title") {
		t.Fatalf("the marker stayed in the body: %q", parsed.Body)
	}
}

func TestAgentAndUserRoundTrip(t *testing.T) {
	agent := memory.Entry{
		Title: "A lesson", Date: "2026-11-11", Body: "The body.",
		NativeType: "reference",
	}
	parsedAgent := ParseAgent(RenderAgent(agent))
	if len(parsedAgent) != 1 || parsedAgent[0].Title != "A lesson" || parsedAgent[0].Body != "The body." {
		t.Fatalf("the agent store came back as %+v", parsedAgent)
	}

	user := memory.Entry{Title: "Identity", Body: "The person.", Reason: "because it is always true"}
	parsedUser := ParseUser(RenderUser(user))
	if len(parsedUser) != 1 || parsedUser[0].Title != "Identity" || parsedUser[0].Reason != user.Reason || parsedUser[0].Body != user.Body {
		t.Fatalf("the user store came back as %+v", parsedUser)
	}
}

func TestQwenRoundTrip(t *testing.T) {
	entry := memory.Entry{
		Title: "A preference", Body: "The rule.", Description: "One line",
		NativeType: "user", Keywords: []string{"memory"},
	}
	parsed, ok := ParseQwen(RenderQwen(entry), "a-preference.md")
	if !ok {
		t.Fatal("the qwen file does not parse")
	}
	if parsed.Title != entry.Title || parsed.Body != entry.Body || parsed.Description != entry.Description {
		t.Fatalf("the qwen file came back as %+v", parsed)
	}
}

func TestRenderHotIsStable(t *testing.T) {
	entries := []memory.Entry{
		{Title: "Beta", Type: memory.TypeLesson, Tier: memory.TierHot, Body: "Second."},
		{Title: "Alpha", Type: memory.TypeUser, Tier: memory.TierHot, Body: "First."},
		{Title: "Cold", Type: memory.TypeLesson, Tier: memory.TierCold, Body: "Hidden."},
	}
	got := RenderHot(entries)
	again := RenderHot(entries)
	if got != again {
		t.Fatal("the hot render is not stable")
	}
	if !strings.Contains(got, "### Alpha") || !strings.Contains(got, "### Beta") {
		t.Fatalf("the hot render is %q", got)
	}
	if strings.Contains(got, "Hidden") {
		t.Fatalf("a cold entry entered the hot tier: %q", got)
	}
	if strings.Index(got, "### Alpha") > strings.Index(got, "### Beta") {
		t.Fatalf("user did not come before lesson:\n%s", got)
	}
}
