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
