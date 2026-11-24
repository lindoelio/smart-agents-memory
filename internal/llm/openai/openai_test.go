package openai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/lindoelio/smart-agents-memory/internal/llm"
)

func TestCompleteJSONStripsMarkdownFence(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Fatalf("the path is %q", r.URL.Path)
		}
		var body struct {
			Model    string `json:"model"`
			Messages []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body.Model != "test-model" || len(body.Messages) != 2 {
			t.Fatalf("the request is %+v", body)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{
				"message": map[string]string{"content": "```json\n{\"refs\":[\"lesson:a\"]}\n```"},
			}},
		})
	}))
	defer server.Close()

	client := New(server.URL+"/v1", "test-model", "TEST_LLM_KEY")
	var out struct {
		Refs []string `json:"refs"`
	}
	if err := client.CompleteJSON(context.Background(), llm.Request{
		SystemPrompt: "system", UserPrompt: "user", Temperature: 0,
	}, &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Refs) != 1 || out.Refs[0] != "lesson:a" {
		t.Fatalf("the refs are %v", out.Refs)
	}
}

func TestCompleteJSONReportsAnHTTPFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusUnauthorized)
	}))
	defer server.Close()
	client := New(server.URL, "test-model", "TEST_LLM_KEY")
	err := client.CompleteJSON(context.Background(), llm.Request{}, &map[string]any{})
	if err == nil || !strings.Contains(err.Error(), "401") {
		t.Fatalf("the error is %v", err)
	}
}

func TestExtractJSON(t *testing.T) {
	if got := extractJSON("prefix {\"a\":1} suffix"); got != `{"a":1}` {
		t.Fatalf("the extraction is %q", got)
	}
	if got := extractJSON("```\n[1,2]\n```"); got != "[1,2]" {
		t.Fatalf("the fenced extraction is %q", got)
	}
}
