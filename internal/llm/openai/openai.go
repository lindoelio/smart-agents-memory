// Package openai is the OpenAI-compatible adapter for the llm port. It
// talks to api.openai.com or to a local smart-llm-gateway without changing
// the use cases.
package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/lindoelio/smart-agents-memory/internal/llm"
)

// DefaultKeyEnv is the environment variable holding the API key.
const DefaultKeyEnv = "SMART_AGENTS_MEMORY_LLM_API_KEY"

// Client is an OpenAI-compatible JSON client.
type Client struct {
	BaseURL string
	Model   string
	KeyEnv  string
	HTTP    *http.Client
}

// New builds a client. An empty base URL selects the local gateway unless
// the key environment variable is set, in which case it selects OpenAI.
func New(baseURL, model, keyEnv string) *Client {
	if keyEnv == "" {
		keyEnv = DefaultKeyEnv
	}
	if model == "" {
		model = "gpt-4o-mini"
	}
	return &Client{BaseURL: baseURL, Model: model, KeyEnv: keyEnv, HTTP: &http.Client{Timeout: 30 * time.Second}}
}

// Kind names the adapter.
func (c *Client) Kind() string { return "openai" }

// CompleteJSON sends one completion and unmarshals its JSON content.
func (c *Client) CompleteJSON(ctx context.Context, req llm.Request, out any) error {
	key := strings.TrimSpace(os.Getenv(c.KeyEnv))
	base := strings.TrimRight(c.BaseURL, "/")
	if base == "" {
		if key != "" {
			base = "https://api.openai.com/v1"
		} else {
			base = "http://127.0.0.1:8080/v1"
		}
	}
	payload := map[string]any{
		"model": c.Model,
		"messages": []map[string]string{
			{"role": "system", "content": req.SystemPrompt},
			{"role": "user", "content": req.UserPrompt},
		},
		"temperature": req.Temperature,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if key != "" {
		httpReq.Header.Set("Authorization", "Bearer "+key)
	}
	client := c.HTTP
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	resp, err := client.Do(httpReq)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("the llm endpoint returned %s", resp.Status)
	}
	var completion struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&completion); err != nil {
		return fmt.Errorf("cannot decode the llm response: %w", err)
	}
	if len(completion.Choices) == 0 {
		return fmt.Errorf("the llm response has no choices")
	}
	content := extractJSON(completion.Choices[0].Message.Content)
	if err := json.Unmarshal([]byte(content), out); err != nil {
		return fmt.Errorf("the llm response is not JSON: %w", err)
	}
	return nil
}

// extractJSON removes an optional Markdown fence and returns the object or
// array inside the assistant message.
func extractJSON(content string) string {
	text := strings.TrimSpace(content)
	if strings.HasPrefix(text, "```") {
		if newline := strings.Index(text, "\n"); newline >= 0 {
			text = text[newline+1:]
		}
		if end := strings.LastIndex(text, "```"); end >= 0 {
			text = text[:end]
		}
		text = strings.TrimSpace(text)
	}
	start := strings.IndexAny(text, "{[")
	end := strings.LastIndexAny(text, "}]")
	if start >= 0 && end > start {
		return text[start : end+1]
	}
	return text
}
