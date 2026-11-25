package memory

import (
	"strings"
	"testing"
)

func TestParseAndFormatRoundTrip(t *testing.T) {
	source := `---
title: "A durable lesson"
type: lesson
date: 2026-11-07
source: "smart-agents-memory-cli"
native: minimax-agent
tier: cold
native_type: reference
---

## A durable lesson

The rule. Why it matters.
`
	entry, ok := Parse(source)
	if !ok {
		t.Fatal("the note was not parsed")
	}
	if entry.Title != "A durable lesson" {
		t.Fatalf("title = %q", entry.Title)
	}
	if entry.Type != TypeLesson {
		t.Fatalf("type = %q", entry.Type)
	}
	if entry.Tier != TierCold {
		t.Fatalf("tier = %q", entry.Tier)
	}
	if entry.Body != "The rule. Why it matters." {
		t.Fatalf("body = %q", entry.Body)
	}
	if entry.Date != "2026-11-07" {
		t.Fatalf("date = %q", entry.Date)
	}

	again, ok := Parse(Format(entry))
	if !ok {
		t.Fatal("the formatted note was not parsed")
	}
	if again.Body != entry.Body || again.Title != entry.Title || again.Type != entry.Type {
		t.Fatalf("round trip changed the entry: %+v", again)
	}
}

func TestPinnedRoundTrip(t *testing.T) {
	source := `---
title: "A pinned rule"
type: lesson
date: 2026-11-07
tier: cold
pinned: true
---

## A pinned rule

The rule. Why it matters.
`
	entry, ok := Parse(source)
	if !ok {
		t.Fatal("the pinned note was not parsed")
	}
	if !entry.Pinned {
		t.Fatal("the note was not read as pinned")
	}
	if _, kept := entry.Extra["pinned"]; kept {
		t.Fatal("pinned was preserved as an unknown key")
	}

	formatted := Format(entry)
	if !strings.Contains(formatted, "tier: cold\npinned: true\n") {
		t.Fatalf("pinned is not after tier:\n%s", formatted)
	}
	again, ok := Parse(formatted)
	if !ok || !again.Pinned {
		t.Fatal("the formatted pinned note did not parse back as pinned")
	}

	unpinned := Format(Entry{Title: "An ordinary rule", Type: TypeLesson, Body: "Body."})
	if strings.Contains(unpinned, "pinned:") {
		t.Fatalf("an unpinned note carries the key:\n%s", unpinned)
	}
}

func TestProjectAndWorktreeRoundTrip(t *testing.T) {
	entry := Entry{
		Title: "A branch decision", Type: TypeProject, Body: "The rule.",
		Project: "my-repo", Worktree: "feature",
	}
	formatted := Format(entry)
	if !strings.Contains(formatted, "project: my-repo\nworktree: feature\n") {
		t.Fatalf("the scope is missing from the format:\n%s", formatted)
	}
	parsed, ok := Parse(formatted)
	if !ok {
		t.Fatal("the scoped note did not parse")
	}
	if parsed.Project != "my-repo" || parsed.Worktree != "feature" {
		t.Fatalf("the scope came back as %+v", parsed)
	}
	if _, kept := parsed.Extra["project"]; kept {
		t.Fatal("project was preserved as an unknown key")
	}
	if _, kept := parsed.Extra["worktree"]; kept {
		t.Fatal("worktree was preserved as an unknown key")
	}

	plain := Format(Entry{Title: "A plain rule", Type: TypeLesson, Body: "Body."})
	if strings.Contains(plain, "project:") || strings.Contains(plain, "worktree:") {
		t.Fatalf("an unscoped note carries the keys:\n%s", plain)
	}
}

func TestCategoryAndScenariosRoundTrip(t *testing.T) {
	entry := Entry{
		Title: "A testing lesson", Type: TypeLesson, Body: "The rule.",
		Category: "testing_standard", UsageScenarios: []string{"when a test is flaky", "before a release"},
	}
	formatted := Format(entry)
	if !strings.Contains(formatted, "category: testing_standard\n") ||
		!strings.Contains(formatted, "usage_scenarios:\n  - \"when a test is flaky\"\n") {
		t.Fatalf("the taxonomy is missing from the format:\n%s", formatted)
	}
	parsed, ok := Parse(formatted)
	if !ok {
		t.Fatal("the classified note did not parse")
	}
	if parsed.Category != "testing_standard" || len(parsed.UsageScenarios) != 2 {
		t.Fatalf("the taxonomy came back as %+v", parsed)
	}
}

func TestInferCategory(t *testing.T) {
	cases := []struct {
		entry Entry
		want  string
	}{
		{Entry{Type: TypeUser, Title: "Communication preference"}, "communication_preference"},
		{Entry{Type: TypeUser, Title: "Identity"}, "basic_information"},
		{Entry{Type: TypeProject, Title: "Tech stack"}, "tech_stack"},
		{Entry{Type: TypeProject, Title: "A decision"}, "important_decision"},
		{Entry{Type: TypeLesson, Title: "Flaky test"}, "testing_standard"},
		{Entry{Type: TypeLesson, Title: "MCP setup"}, "mcp_experience"},
		{Entry{Type: TypeLesson, Title: "A regression trap"}, "common_pitfall"},
		{Entry{Type: TypeReference, Title: "Dashboard pointer"}, "tool_experience"},
	}
	for _, tc := range cases {
		if got := InferCategory(tc.entry); got != tc.want {
			t.Errorf("InferCategory(%q) = %q, want %q", tc.entry.Title, got, tc.want)
		}
	}
	if !ValidCategory("important_decision") || !ValidCategory(Uncategorized) || ValidCategory("nonsense") {
		t.Fatal("ValidCategory is wrong")
	}
}

func TestParseKeepsUnknownKeys(t *testing.T) {
	source := `---
title: "Keeps what it does not know"
type: lesson
date: 2026-11-07
alwaysApply: true
custom: "kept"
---

Body.
`
	entry, ok := Parse(source)
	if !ok {
		t.Fatal("the note was not parsed")
	}
	if entry.Extra["alwaysApply"] != "true" {
		t.Fatalf("alwaysApply = %q", entry.Extra["alwaysApply"])
	}
	if entry.Extra["custom"] != "kept" {
		t.Fatalf("custom = %q", entry.Extra["custom"])
	}
	formatted := Format(entry)
	if !strings.Contains(formatted, "alwaysApply: true") {
		t.Fatalf("the extra key was dropped:\n%s", formatted)
	}
}

func TestParseRejectsANoteWithoutTitle(t *testing.T) {
	if _, ok := Parse("---\ntype: lesson\n---\n\nBody.\n"); ok {
		t.Fatal("a note without a title was accepted")
	}
	if _, ok := Parse("no frontmatter at all"); ok {
		t.Fatal("a note without frontmatter was accepted")
	}
}

func TestSlugify(t *testing.T) {
	cases := map[string]string{
		"CI is not the test loop":    "ci-is-not-the-test-loop",
		"  Trimmed  ":                "trimmed",
		"pipefail aborts the script": "pipefail-aborts-the-script",
		"":                           "entry",
	}
	for input, want := range cases {
		if got := Slugify(input); got != want {
			t.Errorf("Slugify(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestSlugifyShortensLongTitles(t *testing.T) {
	long := strings.Repeat("very long title ", 12)
	slug := Slugify(long)
	if len(slug) > 80 {
		t.Fatalf("the slug is %d characters long", len(slug))
	}
	if !strings.Contains(slug, "-") {
		t.Fatalf("the slug has no hash: %q", slug)
	}
	if Slugify(long) != slug {
		t.Fatal("the slug is not deterministic")
	}
}

func TestASCIISlug(t *testing.T) {
	cases := map[string]string{
		"Reconciliação do corpus":               "reconciliacao-do-corpus",
		"Reconciliação do corpus: mtime decide": "reconciliacao-do-corpus-mtime-decide",
		"---leading": "leading",
	}
	for input, want := range cases {
		got := ASCIISlug(input)
		if got != want {
			t.Errorf("ASCIISlug(%q) = %q, want %q", input, got, want)
		}
	}
	long := ASCIISlug(strings.Repeat("abc ", 40))
	if len(long) > 64 {
		t.Fatalf("the topic name is %d characters long", len(long))
	}
	for i, r := range long {
		valid := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-'
		if !valid {
			t.Fatalf("the topic name has an invalid character at %d: %q", i, long)
		}
	}
}

func TestNormTitle(t *testing.T) {
	if NormTitle("CI is not the test loop") != NormTitle("ci is not the test loop") {
		t.Fatal("the comparison is case sensitive")
	}
	if NormTitle("`code` title") != "code title" {
		t.Fatalf("NormTitle = %q", NormTitle("`code` title"))
	}
}

func TestTypeMapping(t *testing.T) {
	if TypeUser.NativeTarget() != "minimax-user" {
		t.Fatal("a user entry does not target the user store")
	}
	if TypeLesson.NativeTarget() != "minimax-agent" {
		t.Fatal("a lesson does not target the agent store")
	}
	if TypeUser.NativeType() != "user" {
		t.Fatal("a user entry is not a user native type")
	}
	if TypeLesson.NativeType() != "reference" {
		t.Fatal("a lesson is not a reference native type")
	}
	for _, typ := range Types {
		if !typ.Valid() {
			t.Fatalf("%q is not valid", typ)
		}
	}
	if Type("unknown").Valid() {
		t.Fatal("an unknown type is valid")
	}
}

func TestEffectiveTier(t *testing.T) {
	if (Entry{}).EffectiveTier() != TierCold {
		t.Fatal("an entry without a tier is not cold")
	}
	if (Entry{Tier: TierHot}).EffectiveTier() != TierHot {
		t.Fatal("a hot entry is not hot")
	}
}

func TestIsEnglish(t *testing.T) {
	if !IsEnglish("The rule is that you always write the why, because a rule without a why is not reusable at all.") {
		t.Fatal("English was not recognized")
	}
	if IsEnglish("A regra e que voce sempre escreve o porque, porque uma regra sem motivo nao serve para nada aqui.") {
		t.Fatal("Portuguese was not recognized")
	}
	if !IsEnglish("short text") {
		t.Fatal("a short text warns")
	}
}

func TestIsEnglishStrict(t *testing.T) {
	cases := []struct {
		text string
		want bool
	}{
		{"Não faça isso porque quebra o build.", false},
		{"Fix the bug now.", true},
		{"sqlite migration", true},
		{"A regra e que voce sempre escreve o porque, porque uma regra sem motivo nao serve para nada aqui.", false},
		{"The rule is that you always write the why, because a rule without a why is not reusable at all.", true},
	}
	for _, tc := range cases {
		if got := IsEnglishStrict(tc.text); got != tc.want {
			t.Errorf("IsEnglishStrict(%q) = %v, want %v", tc.text, got, tc.want)
		}
	}
}

func TestKeywordsWithYAMLPunctuationRoundTrip(t *testing.T) {
	entry := Entry{
		Title:    "A tricky keyword",
		Type:     TypeLesson,
		Date:     "2026-11-25",
		Body:     "The rule. Why: it matters.",
		Keywords: []string{"plain", "a: b", "#comment", "key: value", "line one\nline two"},
	}
	parsed, ok := Parse(Format(entry))
	if !ok {
		t.Fatal("the formatted entry did not parse")
	}
	if len(parsed.Keywords) != len(entry.Keywords) {
		t.Fatalf("the keywords are %v", parsed.Keywords)
	}
	for i, want := range entry.Keywords {
		if parsed.Keywords[i] != want {
			t.Fatalf("keyword %d is %q, want %q", i, parsed.Keywords[i], want)
		}
	}
}
