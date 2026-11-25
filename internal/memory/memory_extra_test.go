package memory

import (
	"strings"
	"testing"
	"time"
)

func TestNormBody(t *testing.T) {
	if NormBody("a  b\n\tc") != "a b c" {
		t.Fatalf("NormBody = %q", NormBody("a  b\n\tc"))
	}
	if NormBody("") != "" {
		t.Fatalf("the empty body is %q", NormBody(""))
	}
}

func TestParseAFrontmatterWithNoTrailingNewline(t *testing.T) {
	entry, ok := Parse("---\ntitle: A rule\ntype: lesson\n---")
	if !ok {
		t.Fatal("a frontmatter that ends the file did not parse")
	}
	if entry.Title != "A rule" || entry.Body != "" {
		t.Fatalf("the entry is %+v", entry)
	}
}

func TestParseFallsBackToTheLineRead(t *testing.T) {
	entry, ok := Parse("---\ntitle: A hand edit\ntype: lesson\ncustom: [unclosed\n---\n\nThe body.\n")
	if !ok {
		t.Fatal("the broken YAML did not parse")
	}
	if entry.Title != "A hand edit" || entry.Type != TypeLesson || entry.Body != "The body." {
		t.Fatalf("the entry is %+v", entry)
	}
}

func TestParseABodyHeadingWithNoNewline(t *testing.T) {
	entry, ok := Parse("---\ntitle: A rule\ntype: lesson\n---\n\n## A rule")
	if !ok {
		t.Fatal("the note did not parse")
	}
	if entry.Body != "" {
		t.Fatalf("the body is %q", entry.Body)
	}
}

func TestFormatWritesEveryKnownField(t *testing.T) {
	entry := Entry{
		Title: "A full entry", Type: TypeLesson, Date: "2026-11-07", Body: "Body.",
		Tier: TierHot, Pinned: true, Source: "a-source", Native: "qwen",
		Project: "demo", Worktree: "feature", NativeFile: "a-file.md",
		NativeType: "reference", Reason: "because", Description: "A description",
		Category: "testing_standard", Keywords: []string{"one"}, UsageScenarios: []string{"when"},
		Extra: map[string]string{"zebra": "z", "alpha": "a"},
	}
	formatted := Format(entry)
	for _, want := range []string{
		`title: "A full entry"`, "type: lesson", "date: 2026-11-07", `source: "a-source"`,
		"native: qwen", "tier: hot", "pinned: true", "project: demo", "worktree: feature",
		"native_file: a-file.md", "native_type: reference", `reason: "because"`,
		`description: "A description"`, "category: testing_standard", "  - one",
		"usage_scenarios:", "alpha: a", "zebra: z",
	} {
		if !strings.Contains(formatted, want) {
			t.Errorf("the format is missing %q:\n%s", want, formatted)
		}
	}
	if strings.Index(formatted, "alpha: a") > strings.Index(formatted, "zebra: z") {
		t.Fatalf("the extra keys are not sorted:\n%s", formatted)
	}
	parsed, ok := Parse(formatted)
	if !ok {
		t.Fatal("the full entry did not parse back")
	}
	if parsed.Pinned != entry.Pinned || parsed.Tier != entry.Tier || parsed.Extra["alpha"] != "a" {
		t.Fatalf("the round trip lost fields: %+v", parsed)
	}
}

func TestFormatDefaultsTheDateAndSource(t *testing.T) {
	formatted := Format(Entry{Title: "A rule", Type: TypeLesson, Body: "Body."})
	if !strings.Contains(formatted, "date: "+time.Now().Format("2006-01-02")) {
		t.Fatalf("the date is missing:\n%s", formatted)
	}
	if !strings.Contains(formatted, `source: "`+DefaultSource+`"`) {
		t.Fatalf("the source is missing:\n%s", formatted)
	}
}

func TestScalarRendersEveryYAMLValue(t *testing.T) {
	cases := []struct {
		value any
		want  string
	}{
		{nil, ""},
		{"text", "text"},
		{true, "true"},
		{7, "7"},
		{int64(8), "8"},
		{1.5, "1.5"},
		{time.Date(2026, 11, 7, 0, 0, 0, 0, time.UTC), "2026-11-07"},
		{[]any{"a", "b"}, "a, b"},
		{[]any{int64(1), "b"}, "1, b"},
	}
	for _, tc := range cases {
		if got := scalar(tc.value); got != tc.want {
			t.Errorf("scalar(%v) = %q, want %q", tc.value, got, tc.want)
		}
	}
}

func TestStringList(t *testing.T) {
	if got := stringList(nil); got != nil {
		t.Fatalf("nil is %v", got)
	}
	if got := stringList(""); got != nil {
		t.Fatalf("the empty scalar is %v", got)
	}
	if got := stringList("one"); len(got) != 1 || got[0] != "one" {
		t.Fatalf("the scalar is %v", got)
	}
	if got := stringList([]any{"one", "", nil, "two"}); len(got) != 2 || got[1] != "two" {
		t.Fatalf("the list is %v", got)
	}
	if got := stringList(true); len(got) != 1 || got[0] != "true" {
		t.Fatalf("a bool is %v", got)
	}
}

func TestSortedKeys(t *testing.T) {
	if got := sortedKeys(nil); got != nil {
		t.Fatalf("nil is %v", got)
	}
	got := sortedKeys(map[string]string{"c": "3", "a": "1", "b": "2"})
	if len(got) != 3 || got[0] != "a" || got[1] != "b" || got[2] != "c" {
		t.Fatalf("the keys are %v", got)
	}
}

func TestInferCategoryCoversTheTaxonomy(t *testing.T) {
	cases := []struct {
		entry Entry
		want  string
	}{
		{Entry{Type: TypeUser, Title: "Hobbies"}, "hobbies"},
		{Entry{Type: TypeUser, Title: "Working style"}, "behavior_habit"},
		{Entry{Type: TypeProject, Title: "Build pipeline"}, "build_configuration"},
		{Entry{Type: TypeProject, Title: "Dependencies"}, "dependency_configuration"},
		{Entry{Type: TypeProject, Title: "Editor setup"}, "ide_configuration"},
		{Entry{Type: TypeProject, Title: "Git worktree"}, "scm_configuration"},
		{Entry{Type: TypeProject, Title: "Deployment runtime"}, "environment_configuration"},
		{Entry{Type: TypeProject, Title: "Project overview"}, "project_introduction"},
		{Entry{Type: TypeLesson, Title: "A comment rule"}, "comment_standard"},
		{Entry{Type: TypeLesson, Title: "A command flag"}, "tool_experience"},
		{Entry{Type: TypeLesson, Title: "A style convention"}, "code_standard"},
		{Entry{Type: TypeLesson, Title: "Something else"}, "practice_standard"},
		{Entry{Type: TypeReference, Title: "An MCP pointer"}, "mcp_experience"},
		{Entry{Type: Type("unknown"), Title: "anything"}, Uncategorized},
		{Entry{Type: TypeLesson, Title: "Explicit", Category: "important_decision"}, "important_decision"},
		{Entry{Type: TypeUser, Title: "Keywords", Keywords: []string{"language"}}, "communication_preference"},
		{Entry{Type: TypeProject, Title: "A note", Description: "framework"}, "tech_stack"},
	}
	for _, tc := range cases {
		if got := InferCategory(tc.entry); got != tc.want {
			t.Errorf("InferCategory(%+v) = %q, want %q", tc.entry, got, tc.want)
		}
	}
}

func TestASCIISlugEdgeCases(t *testing.T) {
	if got := ASCIISlug("---"); got != "topic" {
		t.Fatalf("a punctuation-only title is %q", got)
	}
	if got := ASCIISlug("Café ~ test"); got != "cafe-test" {
		t.Fatalf("the accented title is %q", got)
	}
	if got := ASCIISlug("1st place"); got != "1st-place" {
		t.Fatalf("a leading digit is %q", got)
	}
	if got := ASCIISlug(""); got != "topic" {
		t.Fatalf("an empty title is %q", got)
	}
}
