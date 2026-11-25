package native

import (
	"strings"
	"testing"
	"time"

	"github.com/lindoelio/smart-agents-memory/internal/memory"
)

func TestTargetValid(t *testing.T) {
	for _, target := range Targets {
		if !target.Valid() {
			t.Fatalf("%q is not valid", target)
		}
	}
	if Target("nonsense").Valid() {
		t.Fatal("an unknown target is valid")
	}
	if Target("").Valid() {
		t.Fatal("the empty target is valid")
	}
}

func TestAgentTitlesDropsTheDateAndNormalizes(t *testing.T) {
	content := "### A lesson (2026-11-07)\nType: reference\n\nBody.\n\n### `Another` Lesson (2026-11-08)\n\nBody.\n"
	titles := AgentTitles(content)
	if !titles[memory.NormTitle("A lesson")] {
		t.Fatalf("the dated title is missing: %v", titles)
	}
	if !titles[memory.NormTitle("`Another` Lesson")] {
		t.Fatalf("the second title is missing: %v", titles)
	}
	if titles[memory.NormTitle("A lesson (2026-11-07)")] {
		t.Fatal("the date stayed in the title")
	}
	if len(AgentTitles("no headings")) != 0 {
		t.Fatal("a text without headings has titles")
	}
}

func TestUserTitlesKeepsTheDate(t *testing.T) {
	content := "### A title (2026-11-07)\n\nBody.\n"
	titles := UserTitles(content)
	if !titles[memory.NormTitle("A title (2026-11-07)")] {
		t.Fatalf("the user title is missing: %v", titles)
	}
	if len(UserTitles("")) != 0 {
		t.Fatal("an empty store has titles")
	}
}

func TestTodayIsTheDateTheStoresUse(t *testing.T) {
	want := time.Now().Format("2006-01-02")
	if got := today(); got != want {
		t.Fatalf("today is %q, want %q", got, want)
	}
	if got := RenderAgent(memory.Entry{Title: "A lesson", Body: "Body."}); !strings.Contains(got, "("+want+")") {
		t.Fatalf("the default date is not today:\n%s", got)
	}
}

func TestParseAgentHandlesHandEdits(t *testing.T) {
	for name, tc := range map[string]struct {
		content string
		title   string
		body    string
		ntype   string
	}{
		"no type line": {
			content: "### A lesson\n\nThe body.\n",
			title:   "A lesson", body: "The body.", ntype: "reference",
		},
		"type line after a comment": {
			content: "### A lesson\n<!-- mem-append-reason: why -->\nType: user\nThe body.\n",
			title:   "A lesson", body: "<!-- mem-append-reason: why -->The body.", ntype: "user",
		},
		"type line with no body": {
			content: "### A lesson (2026-11-07)\nType: reference",
			title:   "A lesson", body: "", ntype: "reference",
		},
		"comment then type line with no body": {
			content: "### A lesson\n<!-- a comment -->\nType: user",
			title:   "A lesson", body: "<!-- a comment -->", ntype: "user",
		},
	} {
		t.Run(name, func(t *testing.T) {
			parsed := ParseAgent(tc.content)
			if len(parsed) != 1 {
				t.Fatalf("the store parsed to %+v", parsed)
			}
			if parsed[0].Title != tc.title || parsed[0].Body != tc.body || parsed[0].NativeType != tc.ntype {
				t.Fatalf("the entry is %+v", parsed[0])
			}
			if parsed[0].Type != memory.TypeLesson || parsed[0].Native != string(TargetMiniMaxAgent) {
				t.Fatalf("the entry mapping is %+v", parsed[0])
			}
		})
	}
	if parsed := ParseAgent("no headings at all\n"); len(parsed) != 0 {
		t.Fatalf("a text without headings parsed to %+v", parsed)
	}
}

func TestParseUserReadsTheReasonBeforeTheHeading(t *testing.T) {
	content := "<!-- mem-append-reason: always true -->\n### A preference\n\nThe body.\n"
	parsed := ParseUser(content)
	if len(parsed) != 1 {
		t.Fatalf("the store parsed to %+v", parsed)
	}
	if parsed[0].Title != "A preference" || parsed[0].Reason != "always true" || parsed[0].Body != "The body." {
		t.Fatalf("the entry is %+v", parsed[0])
	}
	if parsed[0].Type != memory.TypeUser || parsed[0].Source != "minimax-user-memory" {
		t.Fatalf("the entry mapping is %+v", parsed[0])
	}

	// A malformed reason comment is ignored.
	parsed = ParseUser("<!-- mem-append-reason: unterminated\n### A title\n\nBody.\n")
	if len(parsed) != 1 || parsed[0].Reason != "" {
		t.Fatalf("the malformed reason is %+v", parsed)
	}

	// A reason comment on its own block with no heading is not an entry.
	if parsed := ParseUser("<!-- mem-append-reason: nothing -->\n"); len(parsed) != 0 {
		t.Fatalf("a lone comment parsed to %+v", parsed)
	}
}

func TestParseTopicRejectsWhatIsNotATopic(t *testing.T) {
	if _, ok := ParseTopic("no frontmatter\n", "a.md"); ok {
		t.Fatal("a text without frontmatter parsed")
	}
	if _, ok := ParseTopic("---\ntype: reference\n---\n\nno marker\n", "a.md"); ok {
		t.Fatal("a topic without a marker or description title parsed")
	}
	if _, ok := ParseTopic("---\ndescription: [unclosed\n---\n\n"+TitleMark+"A title -->\nBody.\n", "a.md"); !ok {
		t.Fatal("a topic with broken YAML and a marker did not parse")
	}
}

func TestParseTopicFallsBackToTheDescription(t *testing.T) {
	parsed, ok := ParseTopic("---\ndescription: A described title\n---\n\nThe body.\n", "a.md")
	if !ok {
		t.Fatal("the topic did not parse")
	}
	if parsed.Title != "A described title" || parsed.Body != "The body." {
		t.Fatalf("the entry is %+v", parsed)
	}
}

func TestParseQwenHandlesHandEdits(t *testing.T) {
	t.Run("broken yaml falls back to the line read", func(t *testing.T) {
		content := "---\nname: [unclosed\ntype: user\n---\n\nThe body.\n"
		parsed, ok := ParseQwen(content, "a.md")
		if !ok {
			t.Fatal("the broken YAML did not parse")
		}
		if parsed.Title == "" || parsed.Type != memory.TypeUser {
			t.Fatalf("the entry is %+v", parsed)
		}
	})

	t.Run("a reference type is a lesson", func(t *testing.T) {
		content := "---\nname: A rule\ntype: reference\ncategory: practice_standard\n---\n\nBody.\n"
		parsed, ok := ParseQwen(content, "a.md")
		if !ok || parsed.Type != memory.TypeLesson {
			t.Fatalf("the entry is %+v", parsed)
		}
		if parsed.Category != "practice_standard" {
			t.Fatalf("the category is %q", parsed.Category)
		}
	})

	t.Run("a single keyword is a list of one", func(t *testing.T) {
		content := "---\nname: A rule\nkeywords: memory\nusage_scenarios: before a release\n---\n\nBody.\n"
		parsed, ok := ParseQwen(content, "a.md")
		if !ok {
			t.Fatal("the scalar keyword did not parse")
		}
		if len(parsed.Keywords) != 1 || len(parsed.UsageScenarios) != 1 {
			t.Fatalf("the lists are %v / %v", parsed.Keywords, parsed.UsageScenarios)
		}
	})

	t.Run("a list with an empty item drops it", func(t *testing.T) {
		content := "---\nname: A rule\nkeywords:\n  - one\n  - \"\"\n---\n\nBody.\n"
		parsed, ok := ParseQwen(content, "a.md")
		if !ok || len(parsed.Keywords) != 1 || parsed.Keywords[0] != "one" {
			t.Fatalf("the list is %+v", parsed)
		}
	})

	if _, ok := ParseQwen("no frontmatter\n", "a.md"); ok {
		t.Fatal("a text without frontmatter parsed")
	}
	if _, ok := ParseQwen("---\ntype: user\n---\n\nBody.\n", "a.md"); ok {
		t.Fatal("a topic without a name parsed")
	}
	if _, ok := ParseQwen("---\nname: 123\n---\n\nBody.\n", "a.md"); ok {
		t.Fatal("a non-string name parsed")
	}
}

func TestRenderDefaults(t *testing.T) {
	agent := RenderAgent(memory.Entry{Title: "A lesson"})
	if !strings.Contains(agent, "Type: reference\n") {
		t.Fatalf("the default native type is missing:\n%s", agent)
	}
	user := RenderUser(memory.Entry{Title: "A preference", Body: "Body."})
	if strings.Contains(user, "mem-append-reason") {
		t.Fatalf("an entry without a reason carries the comment:\n%s", user)
	}

	long := strings.Repeat("a description with words ", 10)
	topic := RenderTopic(memory.Entry{Title: "A topic", Description: long, Body: "Body."})
	var desc string
	for _, line := range strings.Split(topic, "\n") {
		if strings.HasPrefix(line, "description: ") {
			desc = strings.TrimPrefix(line, "description: ")
		}
	}
	if desc == "" || len(desc) > TopicDescription {
		t.Fatalf("the description was not shortened to the limit: %q", desc)
	}
	if !strings.HasSuffix(desc, "...") {
		t.Fatalf("the shortened description has no ellipsis: %q", desc)
	}

	if got := RenderHot(nil); got != "" {
		t.Fatalf("an empty hot tier is %q", got)
	}
}

func TestSplitFrontmatter(t *testing.T) {
	if _, _, ok := splitFrontmatter("no header\n"); ok {
		t.Fatal("a text without a header split")
	}
	if _, _, ok := splitFrontmatter("---\nname: A\n"); ok {
		t.Fatal("an unterminated header split")
	}
	head, body, ok := splitFrontmatter("---\nname: A\n---\n\nBody.\n")
	if !ok || head != "name: A" || body != "\nBody.\n" {
		t.Fatalf("the split is %q %q %v", head, body, ok)
	}
}

func TestStringListAndScalar(t *testing.T) {
	if got := stringList(nil); got != nil {
		t.Fatalf("nil is %v", got)
	}
	if got := stringList(""); got != nil {
		t.Fatalf("the empty scalar is %v", got)
	}
	if got := stringList("one"); len(got) != 1 || got[0] != "one" {
		t.Fatalf("the scalar is %v", got)
	}
	if got := stringList([]any{"one", "", 2, "two"}); len(got) != 2 || got[1] != "two" {
		t.Fatalf("the list is %v", got)
	}
	if got := scalar(2); got != "" {
		t.Fatalf("a non-string scalar is %q", got)
	}
	if got := scalar("text"); got != "text" {
		t.Fatalf("a string scalar is %q", got)
	}
}

func TestReasonComment(t *testing.T) {
	if got := reasonComment("<!-- mem-append-reason: why -->"); got != "why" {
		t.Fatalf("the reason is %q", got)
	}
	if got := reasonComment("<!-- other -->"); got != "" {
		t.Fatalf("another comment is %q", got)
	}
	if got := reasonComment("<!-- mem-append-reason: unterminated"); got != "" {
		t.Fatalf("an unterminated reason is %q", got)
	}
}
