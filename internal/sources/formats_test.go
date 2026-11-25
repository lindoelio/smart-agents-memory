package sources

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func readFixture(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "extract", "testdata", path))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestParseMessagesFixtures(t *testing.T) {
	cases := []struct {
		file string
		want []string
	}{
		{"claude/session.jsonl", []string{"user", "assistant"}},
		{"qwen/session.jsonl", []string{"user", "assistant"}},
		{"cursor/session.jsonl", []string{"user", "assistant"}},
		{"grok/session.jsonl", []string{"user", "assistant"}},
	}
	for _, tc := range cases {
		t.Run(tc.file, func(t *testing.T) {
			turns, err := Parse(FormatMessagesJSONL, readFixture(t, tc.file))
			if err != nil {
				t.Fatal(err)
			}
			if len(turns) != len(tc.want) {
				t.Fatalf("the turns are %+v", turns)
			}
			for i, role := range tc.want {
				if turns[i].Role != role {
					t.Fatalf("turn %d is %+v, want role %s", i, turns[i], role)
				}
			}
			all := strings.Join([]string{turns[0].Text, turns[1].Text}, "\n")
			for _, forbidden := range []string{"system-reminder", "synthetic_reason", "tool output", "<user_query>", "<timestamp>"} {
				if strings.Contains(all, forbidden) {
					t.Fatalf("the prompt kept %q: %q", forbidden, all)
				}
			}
		})
	}
}

func TestParseACPFixture(t *testing.T) {
	turns, err := Parse(FormatACPJSONL, readFixture(t, "grok/updates.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if len(turns) != 2 || turns[0].Role != "user" || turns[1].Role != "assistant" {
		t.Fatalf("the ACP turns are %+v", turns)
	}
	if !strings.Contains(turns[0].Text, "ACP adapter") {
		t.Fatalf("the user text is %q", turns[0].Text)
	}
}

func TestParseStepFixture(t *testing.T) {
	turns, err := Parse(FormatStepJSONL, readFixture(t, "antigravity/session.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if len(turns) != 3 {
		t.Fatalf("the step turns are %+v", turns)
	}
	if turns[0].Role != "user" || turns[0].Text != "Remember the Antigravity hook rule. Why: the payload is camelCase." {
		t.Fatalf("the first turn is %+v", turns[0])
	}
	if turns[1].Role != "assistant" {
		t.Fatalf("the second turn is %+v", turns[1])
	}
	if strings.Contains(turns[0].Text, "USER_REQUEST") || strings.Contains(turns[0].Text, "ADDITIONAL_METADATA") {
		t.Fatalf("the wrapper was kept: %q", turns[0].Text)
	}
}

func TestParseResponseItemFixture(t *testing.T) {
	turns, err := Parse(FormatResponseItemJSONL, readFixture(t, "codex/session.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if len(turns) != 2 || turns[0].Role != "user" || turns[1].Role != "assistant" {
		t.Fatalf("the Codex turns are %+v", turns)
	}
	if strings.Contains(turns[0].Text, "AGENTS.md") || strings.Contains(turns[0].Text, "developer") {
		t.Fatalf("the context leaked: %q", turns[0].Text)
	}
}

func TestParseRejectsAnInvalidLine(t *testing.T) {
	if _, err := Parse(FormatMessagesJSONL, []byte("{not json}\n")); err == nil {
		t.Fatal("an invalid JSONL line was accepted")
	}
	if _, err := Parse(Format("unknown"), nil); err == nil {
		t.Fatal("an unknown format was accepted")
	}
}

func TestParseDropsAnEmptyOrUnsupportedLine(t *testing.T) {
	turns, err := Parse(FormatMessagesJSONL, []byte("\n{\"role\":\"system\",\"content\":\"no\"}\n{\"role\":\"user\",\"content\":\"  \"}\n{\"role\":\"user\",\"content\":\"yes\"}\n"))
	if err != nil {
		t.Fatal(err)
	}
	if len(turns) != 1 || turns[0].Text != "yes" {
		t.Fatalf("the turns are %+v", turns)
	}
}

func TestTextOfSkipsToolBlocks(t *testing.T) {
	value := []any{
		map[string]any{"type": "text", "text": "a"},
		map[string]any{"type": "tool_result", "content": "ignore"},
		map[string]any{"type": "thinking", "text": "ignore"},
	}
	if got := textOf(value); got != "a" {
		t.Fatalf("textOf returned %q", got)
	}
	if got := textOf(map[string]any{"message": map[string]any{"content": []any{map[string]any{"type": "text", "text": "nested"}}}}); got != "nested" {
		t.Fatalf("textOf returned %q", got)
	}
}

func TestNormalizeRole(t *testing.T) {
	cases := map[string]string{
		"user": "user", "human": "user", "USER_INPUT": "user",
		"assistant": "assistant", "model": "assistant", "PLANNER_RESPONSE": "assistant",
		"system": "", "tool": "", "": "",
	}
	for input, want := range cases {
		if got := normalizeRole(input); got != want {
			t.Fatalf("%q became %q, want %q", input, got, want)
		}
	}
}

func TestMatchGlob(t *testing.T) {
	cases := []struct {
		pattern, name string
		want          bool
	}{
		{"**/*.jsonl", "a/b/c.jsonl", true},
		{"**/*.jsonl", "c.jsonl", true},
		{"*/chats/*.jsonl", "project/chats/a.jsonl", true},
		{"*/chats/*.jsonl", "project/other/a.jsonl", false},
		{"**/subagents/**", "p/subagents/a.jsonl", true},
		{"a?c", "abc", true},
		{"a?c", "a/c", false},
	}
	for _, tc := range cases {
		if got := matchGlob(tc.pattern, tc.name); got != tc.want {
			t.Fatalf("matchGlob(%q, %q) = %v", tc.pattern, tc.name, got)
		}
	}
}

func TestBuildAppliesOverrides(t *testing.T) {
	reg := Build(map[string][]string{"cursor": {"/tmp/custom"}}, []string{"codex"}, 1024, []string{"/tmp/extra"})
	cursor, ok := reg.ByID("cursor")
	if !ok || cursor.Roots[0] != "/tmp/custom" || cursor.MaxBytes != 1024 {
		t.Fatalf("the cursor override is %+v", cursor)
	}
	codex, ok := reg.ByID("codex")
	if !ok || codex.Enabled {
		t.Fatalf("the codex source is %+v", codex)
	}
	custom, ok := reg.ByID("custom")
	if !ok || !custom.Enabled || custom.Roots[0] != "/tmp/extra" {
		t.Fatalf("the custom source is %+v", custom)
	}
	if _, ok := reg.ByID("nope"); ok {
		t.Fatal("an unknown source was found")
	}
}

func TestCleanTextRemovesHarnessBlocks(t *testing.T) {
	input := `<system-reminder>context</system-reminder><timestamp>now</timestamp><user_query>query</user_query><USER_REQUEST>request</USER_REQUEST><ADDITIONAL_METADATA>meta</ADDITIONAL_METADATA><USER_SETTINGS_CHANGE>change</USER_SETTINGS_CHANGE>`
	if got := cleanText(input); got != "queryrequest" {
		t.Fatalf("cleanText returned %q", got)
	}
	if !isAgentsInstructions("# AGENTS.md instructions\n\n<INSTRUCTIONS>x</INSTRUCTIONS>") {
		t.Fatal("the AGENTS.md prefix was not recognized")
	}
}

func TestClipIsRuneSafe(t *testing.T) {
	text := strings.Repeat("á", maxTurnChars+10)
	got := clip(text)
	if !strings.HasSuffix(got, "[truncated]") {
		t.Fatalf("the suffix is missing: %q", got[len(got)-20:])
	}
	if !strings.HasPrefix(got, "á") {
		t.Fatalf("the text was cut in the middle of a rune: %q", got[:2])
	}
}

func TestParseEmptyInput(t *testing.T) {
	turns, err := Parse(FormatMessagesJSONL, nil)
	if err != nil || len(turns) != 0 {
		t.Fatalf("empty input returned %+v %v", turns, err)
	}
}

func TestParseACPAndStepSkipUnknownRecords(t *testing.T) {
	acp, err := Parse(FormatACPJSONL, []byte(`{"method":"session/update","params":{"update":{"sessionUpdate":"turn_completed"}}}`+"\n"))
	if err != nil || len(acp) != 0 {
		t.Fatalf("ACP returned %+v %v", acp, err)
	}
	steps, err := Parse(FormatStepJSONL, []byte(`{"type":"GENERIC","content":"x"}`+"\n"))
	if err != nil || len(steps) != 0 {
		t.Fatalf("steps returned %+v %v", steps, err)
	}
}

func TestTextOfScalarsAndEmptyMaps(t *testing.T) {
	if textOf(42) != "" || textOf(nil) != "" || stringValue(42) != "" {
		t.Fatal("a scalar became text")
	}
	if textOf(map[string]any{"type": "text"}) != "" {
		t.Fatal("an empty text block became text")
	}
	if textOf([]any{"a", map[string]any{"type": "text", "text": "b"}}) != "a\nb" {
		t.Fatal("the mixed array was not joined")
	}
}

func TestRoleOfFallbacks(t *testing.T) {
	cases := []map[string]any{
		{"sender": "human"},
		{"message": map[string]any{"role": "assistant"}},
		{"type": "system"},
		{},
	}
	want := []string{"human", "assistant", "system", ""}
	for i, input := range cases {
		if got := roleOf(input); got != want[i] {
			t.Fatalf("roleOf(%v) = %q, want %q", input, got, want[i])
		}
	}
	if firstNonNil(nil, nil, "x") != "x" {
		t.Fatal("firstNonNil did not reach the value")
	}
}

func TestExpandPath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if got := ExpandPath("~/x"); got != filepath.Join(home, "x") {
		t.Fatalf("ExpandPath returned %q", got)
	}
	if got := ExpandPath("~"); got != home {
		t.Fatalf("ExpandPath(~) returned %q", got)
	}
	if got := ExpandPath("/abs"); got != "/abs" {
		t.Fatalf("ExpandPath kept %q", got)
	}
}
