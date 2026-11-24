package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"

	"github.com/lindoelio/smart-agents-memory/internal/app"
	"github.com/lindoelio/smart-agents-memory/internal/vault"

	_ "github.com/lindoelio/smart-agents-memory/internal/vault/obsidian"
)

func newApp(t *testing.T) *app.App {
	t.Helper()
	root := t.TempDir()
	a, err := app.New(vault.Config{Backend: "obsidian", MemoryHome: root, ProjectionRoot: root}, root, io.Discard, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func TestServeProtocolAndTools(t *testing.T) {
	a := newApp(t)
	server := NewServer(a)
	input := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize"}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"memory_add","arguments":{"scope":"lesson","title":"A flaky test","body":"The flaky test stops the release.","category":"testing_standard"}}}`,
		`{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"memory_tree","arguments":{}}}`,
		`{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"memory_recall","arguments":{"query":"flaky","limit":2}}}`,
		`{"jsonrpc":"2.0","id":6,"method":"tools/call","params":{"name":"memory_search","arguments":{"query":"flaky"}}}`,
		`{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"memory_brief","arguments":{}}}`,
	}, "\n") + "\n"

	out := &bytes.Buffer{}
	if err := server.Serve(context.Background(), strings.NewReader(input), out); err != nil {
		t.Fatal(err)
	}
	decoder := json.NewDecoder(out)
	var responses []map[string]any
	for {
		var resp map[string]any
		if err := decoder.Decode(&resp); err != nil {
			if err == io.EOF {
				break
			}
			t.Fatalf("a response is not JSON: %v\n%s", err, out.String())
		}
		responses = append(responses, resp)
	}
	if len(responses) != 7 {
		t.Fatalf("got %d responses:\n%s", len(responses), out.String())
	}
	if responses[0]["result"] == nil {
		t.Fatalf("initialize failed: %+v", responses[0])
	}
	tools := responses[1]["result"].(map[string]any)["tools"].([]any)
	if len(tools) != 5 {
		t.Fatalf("the tool list has %d tools", len(tools))
	}
	for i, want := range []string{"recorded", "Memory tree", "Recall", "flaky", "A flaky test"} {
		text := toolText(t, responses[i+2])
		if !strings.Contains(text, want) {
			t.Fatalf("response %d does not contain %q: %q", i+2, want, text)
		}
	}
	treeResult := responses[3]["result"].(map[string]any)
	structured, ok := treeResult["structuredContent"].(map[string]any)
	if !ok {
		t.Fatalf("memory_tree has no structured content: %+v", treeResult)
	}
	revision, ok := structured["revision"].(string)
	if !ok || len(revision) != 64 {
		t.Fatalf("memory_tree revision is %q", revision)
	}
	if !strings.Contains(toolText(t, responses[3]), "revision: "+revision) {
		t.Fatalf("memory_tree text does not carry the revision: %q", toolText(t, responses[3]))
	}
	if router, _ := structured["router_prompt"].(string); !strings.Contains(router, "Memory tree") {
		t.Fatalf("memory_tree router is %q", router)
	}
}

func toolText(t *testing.T, resp map[string]any) string {
	t.Helper()
	result, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("the response has no result: %+v", resp)
	}
	content, ok := result["content"].([]any)
	if !ok || len(content) == 0 {
		t.Fatalf("the result has no content: %+v", result)
	}
	item, ok := content[0].(map[string]any)
	if !ok {
		t.Fatalf("the content is %+v", content[0])
	}
	return item["text"].(string)
}

func TestUnknownToolIsAnErrorContent(t *testing.T) {
	a := newApp(t)
	server := NewServer(a)
	input := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"nonsense","arguments":{}}}` + "\n"
	out := &bytes.Buffer{}
	if err := server.Serve(context.Background(), strings.NewReader(input), out); err != nil {
		t.Fatal(err)
	}
	var resp map[string]any
	if err := json.Unmarshal(out.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	result := resp["result"].(map[string]any)
	if result["isError"] != true {
		t.Fatalf("the unknown tool was not an error: %+v", result)
	}
}
