package extract

import (
	"testing"
	"time"
)

func TestParseJSONL(t *testing.T) {
	content := `{"role":"user","content":"A question"}
{"role":"assistant","content":[{"type":"text","text":"An answer"}]}
`
	turns, err := Parse([]byte(content))
	if err != nil {
		t.Fatal(err)
	}
	if len(turns) != 2 || turns[0].Role != "user" || turns[1].Text != "An answer" {
		t.Fatalf("the turns are %+v", turns)
	}
}

func TestParseJSONArrayAndHookPayload(t *testing.T) {
	array, err := Parse([]byte(`[{"role":"human","content":"hello"},{"role":"ai","content":"hi"}]`))
	if err != nil || len(array) != 2 || array[0].Role != "user" || array[1].Role != "assistant" {
		t.Fatalf("the array is %+v %v", array, err)
	}
	hook, err := Parse([]byte(`{"messages":[{"type":"user","message":{"parts":[{"text":"from hook"}]}}]}`))
	if err != nil || len(hook) != 1 || hook[0].Text != "from hook" {
		t.Fatalf("the hook is %+v %v", hook, err)
	}
}

func TestParsePlainText(t *testing.T) {
	turns, err := Parse([]byte("A plain turn from a shell hook."))
	if err != nil || len(turns) != 1 || turns[0].Role != "user" {
		t.Fatalf("the plain turn is %+v %v", turns, err)
	}
}

func TestParseStrictRejectsAConfigurationDocument(t *testing.T) {
	for name, content := range map[string]string{
		"object": `{"theme":"dark","model":"gpt-4o-mini","tools":["read"]}`,
		"array":  `[{"path":"a.json"},{"path":"b.json"}]`,
	} {
		for _, allowPlain := range []bool{false, true} {
			turns, err := ParseStrict([]byte(content), allowPlain)
			if err != nil {
				t.Fatalf("%s allowPlain=%v: %v", name, allowPlain, err)
			}
			if len(turns) != 0 {
				t.Fatalf("%s allowPlain=%v became %+v", name, allowPlain, turns)
			}
		}
	}
}

func TestParseStrictRejectsJSONLinesWithoutTurns(t *testing.T) {
	content := `{"level":"info","message":"started"}
{"level":"info","message":"stopped"}
`
	turns, err := ParseStrict([]byte(content), true)
	if err != nil {
		t.Fatal(err)
	}
	if len(turns) != 0 {
		t.Fatalf("the log lines became %+v", turns)
	}
}

func TestParseStrictAcceptsAConversation(t *testing.T) {
	jsonl, err := ParseStrict([]byte(`{"type":"session_meta","id":"s1"}
{"role":"user","content":"remember this"}
{"role":"assistant","content":"noted"}`), false)
	if err != nil || len(jsonl) != 2 || jsonl[0].Role != "user" {
		t.Fatalf("the jsonl conversation is %+v %v", jsonl, err)
	}
	document, err := ParseStrict([]byte(`{"messages":[{"role":"user","content":"a document"}]}`), false)
	if err != nil || len(document) != 1 || document[0].Text != "a document" {
		t.Fatalf("the document is %+v %v", document, err)
	}
}

func TestParseStrictAcceptsPlainTextOnlyWhenAllowed(t *testing.T) {
	content := []byte("A plain note from the user.")
	rejected, err := ParseStrict(content, false)
	if err != nil || len(rejected) != 0 {
		t.Fatalf("plain text passed without permission: %+v %v", rejected, err)
	}
	accepted, err := ParseStrict(content, true)
	if err != nil || len(accepted) != 1 || accepted[0].Text != string(content) {
		t.Fatalf("plain text was refused with permission: %+v %v", accepted, err)
	}
}

func TestParseStaysPermissiveForUnknownPayloads(t *testing.T) {
	turns, err := Parse([]byte(`{"theme":"dark"}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(turns) != 1 || turns[0].Role != "user" {
		t.Fatalf("the permissive parse dropped an unknown payload: %+v", turns)
	}
}

func TestCompressionOffsetClamp(t *testing.T) {
	store := CursorStore{Sessions: map[string]SessionCursor{
		"s": {SessionID: "s", ProcessedOffset: 10},
	}}
	turns := []Turn{{Role: "user", Text: "one"}}
	if got := StartOffset(store, "s", turns); got != 0 {
		t.Fatalf("the clamp returned %d", got)
	}
	if !HasNewUserTurn(turns, 0) {
		t.Fatal("the new user turn was not seen")
	}
	if HasNewUserTurn([]Turn{{Role: "assistant", Text: "only"}}, 0) {
		t.Fatal("an assistant-only slice counted as a user turn")
	}
}

func TestCursorRoundTrip(t *testing.T) {
	path := t.TempDir() + "/.extract-cursor.json"
	store := CursorStore{Sessions: map[string]SessionCursor{}}
	Advance(&store, "session", "transcript.jsonl", 3, time.Now())
	if err := Save(path, store); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Sessions["session"].ProcessedOffset != 3 {
		t.Fatalf("the cursor is %+v", loaded)
	}
}
