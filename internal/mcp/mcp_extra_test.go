package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
)

// serveOne feeds one JSON-RPC message and returns the decoded response, if
// the server sent one.
func serveOne(t *testing.T, message string) (map[string]any, bool) {
	t.Helper()
	server := NewServer(newApp(t))
	out := &bytes.Buffer{}
	if err := server.Serve(context.Background(), strings.NewReader(message+"\n"), out); err != nil {
		t.Fatalf("the server failed: %v", err)
	}
	if out.Len() == 0 {
		return nil, false
	}
	var resp map[string]any
	if err := json.Unmarshal(out.Bytes(), &resp); err != nil {
		t.Fatalf("the response is not JSON: %v\n%s", err, out.String())
	}
	return resp, true
}

func rpcErrorCode(t *testing.T, resp map[string]any) int {
	t.Helper()
	rpcErr, ok := resp["error"].(map[string]any)
	if !ok {
		t.Fatalf("the response has no error: %+v", resp)
	}
	code, ok := rpcErr["code"].(float64)
	if !ok {
		t.Fatalf("the error has no code: %+v", rpcErr)
	}
	return int(code)
}

func TestInvalidJSONIsAParseError(t *testing.T) {
	resp, ok := serveOne(t, `{"jsonrpc":"2.0",`)
	if !ok {
		t.Fatal("a parse error was not answered")
	}
	if got := rpcErrorCode(t, resp); got != -32700 {
		t.Fatalf("the parse error code is %d", got)
	}
	if resp["id"] != nil {
		t.Fatalf("the parse error carries an id: %+v", resp)
	}
}

func TestUnknownMethodIsNotImplemented(t *testing.T) {
	resp, ok := serveOne(t, `{"jsonrpc":"2.0","id":1,"method":"tools/nonsense"}`)
	if !ok {
		t.Fatal("an unknown method was not answered")
	}
	if got := rpcErrorCode(t, resp); got != -32601 {
		t.Fatalf("the unknown method code is %d", got)
	}
	if resp["id"].(float64) != 1 {
		t.Fatalf("the response id is %v", resp["id"])
	}
}

func TestToolsCallNeedsValidParams(t *testing.T) {
	for name, message := range map[string]string{
		"no params":     `{"jsonrpc":"2.0","id":1,"method":"tools/call"}`,
		"string params": `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":"nonsense"}`,
	} {
		t.Run(name, func(t *testing.T) {
			resp, ok := serveOne(t, message)
			if !ok {
				t.Fatal("invalid params were not answered")
			}
			if got := rpcErrorCode(t, resp); got != -32602 {
				t.Fatalf("the invalid params code is %d", got)
			}
		})
	}
}

func TestInvalidToolArgumentsAreErrorContent(t *testing.T) {
	for _, tool := range []string{"memory_recall", "memory_search", "memory_brief", "memory_add"} {
		t.Run(tool, func(t *testing.T) {
			message := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"` + tool + `","arguments":"nonsense"}}`
			resp, ok := serveOne(t, message)
			if !ok {
				t.Fatal("the tool was not answered")
			}
			result, ok := resp["result"].(map[string]any)
			if !ok {
				t.Fatalf("the response has no result: %+v", resp)
			}
			if result["isError"] != true {
				t.Fatalf("the invalid arguments were accepted: %+v", result)
			}
			text := toolText(t, resp)
			if !strings.Contains(text, tool) {
				t.Fatalf("the error does not name the tool: %q", text)
			}
		})
	}
}

func TestAToolFailureIsAnErrorContent(t *testing.T) {
	cases := map[string]string{
		"memory_recall": `{"refs":["missing:ref"]}`,
		"memory_search": `{"query":""}`,
		"memory_brief":  `{"project":"Bad Slug!"}`,
		"memory_add":    `{"scope":"nonsense","title":"A title","body":"A body."}`,
	}
	for tool, args := range cases {
		t.Run(tool, func(t *testing.T) {
			message := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"` + tool + `","arguments":` + args + `}}`
			resp, ok := serveOne(t, message)
			if !ok {
				t.Fatal("the tool was not answered")
			}
			result, ok := resp["result"].(map[string]any)
			if !ok {
				t.Fatalf("the response has no result: %+v", resp)
			}
			if result["isError"] != true {
				t.Fatalf("the failed call is not an error: %+v", result)
			}
		})
	}
}

func TestServeSkipsEmptyLines(t *testing.T) {
	server := NewServer(newApp(t))
	out := &bytes.Buffer{}
	input := "\n\n" + `{"jsonrpc":"2.0","id":1,"method":"initialize"}` + "\n\n"
	if err := server.Serve(context.Background(), strings.NewReader(input), out); err != nil {
		t.Fatal(err)
	}
	decoder := json.NewDecoder(out)
	var count int
	for {
		var resp map[string]any
		if err := decoder.Decode(&resp); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			t.Fatal(err)
		}
		count++
	}
	if count != 1 {
		t.Fatalf("empty lines produced %d responses", count)
	}
}

func TestServeReturnsTheContextError(t *testing.T) {
	server := NewServer(newApp(t))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	input := `{"jsonrpc":"2.0","id":1,"method":"initialize"}` + "\n"
	if err := server.Serve(ctx, strings.NewReader(input), io.Discard); err == nil {
		t.Fatal("a canceled server kept running")
	}
}

func TestServeReportsAnOversizedMessage(t *testing.T) {
	server := NewServer(newApp(t))
	input := strings.Repeat("a", 9*1024*1024) + "\n"
	if err := server.Serve(context.Background(), strings.NewReader(input), io.Discard); err == nil {
		t.Fatal("an oversized message was accepted")
	}
}

// failingWriter fails every write, so the server cannot answer.
type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) {
	return 0, errors.New("no reader")
}

func TestServeReportsAWriteFailure(t *testing.T) {
	server := NewServer(newApp(t))
	input := `{"jsonrpc":"2.0","id":1,"method":"initialize"}` + "\n"
	if err := server.Serve(context.Background(), strings.NewReader(input), failingWriter{}); err == nil {
		t.Fatal("a failed write was not reported")
	}

	// The parse error response also reports a failed write.
	if err := server.Serve(context.Background(), strings.NewReader("not json\n"), failingWriter{}); err == nil {
		t.Fatal("a failed parse-error write was not reported")
	}
}

func TestInitializedNotificationHasNoResponse(t *testing.T) {
	if _, ok := serveOne(t, `{"jsonrpc":"2.0","method":"notifications/initialized"}`); ok {
		t.Fatal("a notification was answered")
	}
}
