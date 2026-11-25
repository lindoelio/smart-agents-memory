package app_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/lindoelio/smart-agents-memory/internal/app"
	"github.com/lindoelio/smart-agents-memory/internal/extract"
	"github.com/lindoelio/smart-agents-memory/internal/llm"
)

func TestExtractTurnsNoUserTurn(t *testing.T) {
	h := newHarness(t, "obsidian")
	h.app.LLM = &extractLLM{payload: `{"candidates":[]}`}
	report, err := h.app.ExtractTurns(context.Background(), []extract.Turn{{Role: "assistant", Text: "only"}}, app.ExtractOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if report.SkippedReason != "no_new_user_turns" {
		t.Fatalf("the report is %+v", report)
	}
}

func TestExtractTurnsRequiresAnLLM(t *testing.T) {
	h := newHarness(t, "obsidian")
	_, err := h.app.ExtractTurns(context.Background(), []extract.Turn{{Role: "user", Text: "a rule"}}, app.ExtractOptions{})
	if err == nil {
		t.Fatal("an extraction without LLM succeeded")
	}
}

func TestExtractTurnsReportsAnLLMError(t *testing.T) {
	h := newHarness(t, "obsidian")
	h.app.LLM = &failingExtractLLM{err: errors.New("endpoint")}
	_, err := h.app.ExtractTurns(context.Background(), []extract.Turn{{Role: "user", Text: "a rule"}}, app.ExtractOptions{})
	if err == nil {
		t.Fatal("the LLM error was swallowed")
	}
}

func TestExtractTurnsDryRunReportsAcceptedWithoutWriting(t *testing.T) {
	h := newHarness(t, "obsidian")
	h.app.LLM = &extractLLM{payload: `{"candidates":[{"scope":"lesson","title":"A dry turn","body":"The rule. Why it matters."}]}`}
	report, err := h.app.ExtractTurns(context.Background(), []extract.Turn{{Role: "user", Text: "a rule"}}, app.ExtractOptions{DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	if report.Candidates != 1 || report.Accepted != 1 || report.Added != 0 {
		t.Fatalf("the report is %+v", report)
	}
	entries, err := h.app.Vault.Entries(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("the dry run wrote %+v", entries)
	}
}

type failingExtractLLM struct{ err error }

func (f *failingExtractLLM) Kind() string { return "failing" }

func (f *failingExtractLLM) CompleteJSON(context.Context, llm.Request, any) error {
	return f.err
}

type captureLLM struct {
	request llm.Request
	payload string
}

func (c *captureLLM) Kind() string { return "capture" }

func (c *captureLLM) CompleteJSON(_ context.Context, request llm.Request, out any) error {
	c.request = request
	return json.Unmarshal([]byte(c.payload), out)
}

func TestExtractTurnsRedactsHighConfidenceSecretsBeforeThePrompt(t *testing.T) {
	h := newHarness(t, "obsidian")
	token := "ghp_" + strings.Repeat("a", 36)
	capture := &captureLLM{payload: `{"candidates":[]}`}
	h.app.LLM = capture
	report, err := h.app.ExtractTurns(context.Background(), []extract.Turn{
		{Role: "user", Text: "the token is " + token},
	}, app.ExtractOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if report.Redacted != 1 {
		t.Fatalf("the report is %+v", report)
	}
	if strings.Contains(capture.request.UserPrompt, token) {
		t.Fatal("the prompt carried the secret")
	}
	if !strings.Contains(capture.request.UserPrompt, "[redacted]") {
		t.Fatalf("the prompt is %q", capture.request.UserPrompt)
	}
}
