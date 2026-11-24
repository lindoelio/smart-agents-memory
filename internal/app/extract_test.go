package app_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lindoelio/smart-agents-memory/internal/app"
	"github.com/lindoelio/smart-agents-memory/internal/llm"
	"github.com/lindoelio/smart-agents-memory/internal/memory"
	"github.com/lindoelio/smart-agents-memory/internal/vault"
)

type extractLLM struct {
	payload string
	calls   int
}

func (f *extractLLM) Kind() string { return "fake" }

func (f *extractLLM) CompleteJSON(_ context.Context, _ llm.Request, out any) error {
	f.calls++
	return json.Unmarshal([]byte(f.payload), out)
}

func TestExtractAddsColdUnpinnedCandidatesAndAdvancesCursor(t *testing.T) {
	h := newHarness(t, "obsidian")
	ctx := context.Background()
	fake := &extractLLM{payload: `{"candidates":[{"scope":"lesson","title":"A flaky test","body":"The flaky test stops the release. Why it matters.","description":"A flaky test","category":"testing_standard","keywords":["test"],"usage_scenarios":["before a release"]}]}`}
	h.app.LLM = fake

	report, err := h.app.Extract(ctx, app.ExtractOptions{
		Transcript: `{"role":"user","content":"remember that the flaky test stops the release"}
{"role":"assistant","content":"noted"}`,
		SessionID: "s1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if fake.calls != 1 || report.Candidates != 1 || report.Added != 1 {
		t.Fatalf("the report is %+v, calls %d", report, fake.calls)
	}
	entries, err := h.app.Vault.Entries(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Tier != memory.TierCold || entries[0].Pinned {
		t.Fatalf("the extracted entry is %+v", entries)
	}
	data, err := os.ReadFile(filepath.Join(h.root, ".extract-cursor.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"processed_offset": 2`) {
		t.Fatalf("the cursor is %s", data)
	}

	second, err := h.app.Extract(ctx, app.ExtractOptions{
		Transcript: `{"role":"user","content":"remember that the flaky test stops the release"}
{"role":"assistant","content":"noted"}`,
		SessionID: "s1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if fake.calls != 1 || second.SkippedReason != "no_new_user_turns" {
		t.Fatalf("the second run called the model: %+v calls=%d", second, fake.calls)
	}
}

func TestExtractRefusesPinnedAndHotTitles(t *testing.T) {
	h := newHarness(t, "obsidian")
	ctx := context.Background()
	if _, err := h.app.Add(ctx, app.AddOptions{
		Scope: "lesson", Title: "Pinned title", Body: "The curated rule.", Pinned: true,
	}); err != nil {
		t.Fatal(err)
	}
	fake := &extractLLM{payload: `{"candidates":[{"scope":"lesson","title":"Pinned title","body":"A new body. Why it matters."}]}`}
	h.app.LLM = fake
	report, err := h.app.Extract(ctx, app.ExtractOptions{
		Transcript: `{"role":"user","content":"add this"}`,
		SessionID:  "s2",
	})
	if err != nil {
		t.Fatal(err)
	}
	if report.Added != 0 || len(report.Skipped) == 0 {
		t.Fatalf("the pinned candidate was accepted: %+v", report)
	}
	entries, err := h.app.Vault.Entries(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || !strings.Contains(entries[0].Body, "The curated rule.") {
		t.Fatalf("the pinned entry changed: %+v", entries)
	}
}

func TestExtractRefusesAHotTitle(t *testing.T) {
	h := newHarness(t, "obsidian")
	ctx := context.Background()
	if _, err := h.app.Add(ctx, app.AddOptions{
		Scope: "user", Title: "Hot title", Body: "The hot rule.", Tier: "hot",
	}); err != nil {
		t.Fatal(err)
	}
	h.app.LLM = &extractLLM{payload: `{"candidates":[{"scope":"lesson","title":"Hot title","body":"A new body. Why it matters."}]}`}
	report, err := h.app.Extract(ctx, app.ExtractOptions{
		Transcript: `{"role":"user","content":"add this"}`,
		SessionID:  "s6",
	})
	if err != nil {
		t.Fatal(err)
	}
	if report.Added != 0 || len(report.Skipped) == 0 {
		t.Fatalf("the hot candidate was accepted: %+v", report)
	}
	entries, err := h.app.Vault.Entries(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || !strings.Contains(entries[0].Body, "The hot rule.") {
		t.Fatalf("the hot entry changed: %+v", entries)
	}
}

func TestExtractRejectsSecrets(t *testing.T) {
	h := newHarness(t, "obsidian")
	ctx := context.Background()
	fake := &extractLLM{payload: `{"candidates":[{"scope":"lesson","title":"A leaked key","body":"ghp_` + strings.Repeat("a", 36) + `"}]}`}
	h.app.LLM = fake
	report, err := h.app.Extract(ctx, app.ExtractOptions{
		Transcript: `{"role":"user","content":"extract"}`,
		SessionID:  "s3",
	})
	if err != nil {
		t.Fatal(err)
	}
	if report.Added != 0 {
		t.Fatalf("the secret was added: %+v", report)
	}
	if report.Candidates != 1 || report.Accepted != 0 || len(report.Skipped) != 1 {
		t.Fatalf("the rejected candidate was counted as accepted: %+v", report)
	}
	entries, err := h.app.Vault.Entries(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("the secret entered the corpus: %+v", entries)
	}
}

func TestExtractDryRunDoesNotWriteOrAdvanceTheCursor(t *testing.T) {
	h := newHarness(t, "obsidian")
	ctx := context.Background()
	h.app.LLM = &extractLLM{payload: `{"candidates":[{"scope":"lesson","title":"A lesson","body":"The rule. Why it matters."}]}`}
	report, err := h.app.Extract(ctx, app.ExtractOptions{
		Transcript: `{"role":"user","content":"extract"}`,
		SessionID:  "s4", DryRun: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if report.Candidates != 1 || report.Accepted != 1 || report.Added != 0 {
		t.Fatalf("the dry run reported %+v", report)
	}
	entries, err := h.app.Vault.Entries(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("the dry run wrote %+v", entries)
	}
	if _, err := os.Stat(filepath.Join(h.root, ".extract-cursor.json")); !os.IsNotExist(err) {
		t.Fatal("the dry run advanced the cursor")
	}
}

func TestWatchOnceExtractsNewTranscripts(t *testing.T) {
	h := newHarness(t, "obsidian")
	ctx := context.Background()
	dir := filepath.Join(t.TempDir(), "chats")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "session.jsonl")
	if err := os.WriteFile(file, []byte(`{"role":"user","content":"remember this durable rule"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	h.app.LLM = &extractLLM{payload: `{"candidates":[{"scope":"lesson","title":"A watched rule","body":"The rule. Why it matters."}]}`}
	report, err := h.app.WatchOnce(ctx, app.WatchOptions{Dirs: []string{dir}})
	if err != nil {
		t.Fatal(err)
	}
	if report.Files != 1 || report.Added != 1 {
		t.Fatalf("the watch report is %+v", report)
	}
	second, err := h.app.WatchOnce(ctx, app.WatchOptions{Dirs: []string{dir}})
	if err != nil {
		t.Fatal(err)
	}
	if second.Added != 0 {
		t.Fatalf("the second watch re-extracted: %+v", second)
	}
}

func TestWatchOnceIgnoresFilesThatAreNotTranscripts(t *testing.T) {
	h := newHarness(t, "obsidian")
	ctx := context.Background()
	dir := t.TempDir()
	files := map[string]string{
		"settings.json":  `{"theme":"dark","model":"gpt-4o-mini"}`,
		"storage.json":   `[{"path":"a.json"},{"path":"b.json"}]`,
		"session.json":   `{"messages":[{"role":"user","content":"a real conversation"}]}`,
		"session.jsonl":  "{\"role\":\"user\",\"content\":\"a jsonl conversation\"}\n",
		"plain.txt":      "a plain transcript from the user",
		"notes.md":       "# Notes\n\nremember this durable rule",
		"debug.log":      "not json at all, just a log line",
		"unrelated.yaml": "key: value",
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	fake := &extractLLM{payload: `{"candidates":[]}`}
	h.app.LLM = fake

	report, err := h.app.WatchOnce(ctx, app.WatchOptions{Dirs: []string{dir}})
	if err != nil {
		t.Fatal(err)
	}
	// settings.json, storage.json, session.json, session.jsonl, plain.txt and
	// debug.log are scanned; notes.md and the YAML file are not.
	if report.Files != 6 || report.Ignored != 2 {
		t.Fatalf("the watch report is %+v", report)
	}
	if fake.calls != 4 {
		t.Fatalf("the watch called the model %d times, want 4", fake.calls)
	}
	if len(report.Errors) != 0 {
		t.Fatalf("the watch reported errors: %v", report.Errors)
	}
	data, err := os.ReadFile(filepath.Join(h.root, ".extract-cursor.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "settings.json") || strings.Contains(string(data), "storage.json") {
		t.Fatalf("an ignored file got a cursor: %s", data)
	}
}

func TestWatchOnceAcceptsAJSONDocumentWithMessages(t *testing.T) {
	h := newHarness(t, "obsidian")
	ctx := context.Background()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "chat.json"),
		[]byte(`{"messages":[{"role":"user","content":"a document conversation"}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	fake := &extractLLM{payload: `{"candidates":[]}`}
	h.app.LLM = fake
	report, err := h.app.WatchOnce(ctx, app.WatchOptions{Dirs: []string{dir}})
	if err != nil {
		t.Fatal(err)
	}
	if report.Files != 1 || report.Ignored != 0 || fake.calls != 1 {
		t.Fatalf("the document was not extracted: %+v calls=%d", report, fake.calls)
	}
}

func TestInstallHooksWritesAnExecutableScript(t *testing.T) {
	h := newHarness(t, "obsidian")
	path, lines, err := h.app.InstallHooks()
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&0o111 == 0 {
		t.Fatal("the hook script is not executable")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "extract --stdin --auto-dream --quiet") {
		t.Fatalf("the hook script is %q", data)
	}
	if len(lines) < 3 {
		t.Fatalf("the hook lines are %v", lines)
	}
}

func TestExtractAutoDreamTriggersAtTheThreshold(t *testing.T) {
	h := newHarness(t, "obsidian")
	ctx := context.Background()
	h.app.LLM = &extractLLM{payload: `{"candidates":[{"scope":"lesson","title":"A lesson","body":"The rule. Why it matters."}],"operations":[]}`}
	report, err := h.app.Extract(ctx, app.ExtractOptions{
		Transcript: `{"role":"user","content":"extract"}`,
		SessionID:  "s5", AutoDream: true, DreamThreshold: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if report.Dream == nil {
		t.Fatalf("auto-dream did not run: %+v", report)
	}
}

func TestExtractOneShotProcessesEveryPayloadAndKeepsNoCursor(t *testing.T) {
	h := newHarness(t, "obsidian")
	ctx := context.Background()
	fake := &extractLLM{payload: `{"candidates":[]}`}
	h.app.LLM = fake
	payload := `{"role":"user","content":"one finished turn"}`

	for i := 1; i <= 2; i++ {
		report, err := h.app.Extract(ctx, app.ExtractOptions{Transcript: payload, OneShot: true})
		if err != nil {
			t.Fatal(err)
		}
		if report.SkippedReason != "" {
			t.Fatalf("the payload %d was skipped: %+v", i, report)
		}
		if fake.calls != i {
			t.Fatalf("the payload %d produced %d model calls", i, fake.calls)
		}
	}
	if _, err := os.Stat(filepath.Join(h.root, ".extract-cursor.json")); !os.IsNotExist(err) {
		t.Fatal("the one-shot run wrote a cursor")
	}
}

func TestExtractWithASessionIDKeepsTheCursor(t *testing.T) {
	h := newHarness(t, "obsidian")
	ctx := context.Background()
	fake := &extractLLM{payload: `{"candidates":[]}`}
	h.app.LLM = fake
	payload := `{"role":"user","content":"one finished turn"}`

	if _, err := h.app.Extract(ctx, app.ExtractOptions{Transcript: payload, SessionID: "session"}); err != nil {
		t.Fatal(err)
	}
	second, err := h.app.Extract(ctx, app.ExtractOptions{Transcript: payload, SessionID: "session"})
	if err != nil {
		t.Fatal(err)
	}
	if fake.calls != 1 || second.SkippedReason != "no_new_user_turns" {
		t.Fatalf("the session cursor did not survive: %+v calls=%d", second, fake.calls)
	}
}

func TestExtractFromATranscriptPathKeepsTheCursor(t *testing.T) {
	h := newHarness(t, "obsidian")
	ctx := context.Background()
	fake := &extractLLM{payload: `{"candidates":[]}`}
	h.app.LLM = fake
	path := filepath.Join(t.TempDir(), "session.jsonl")
	if err := os.WriteFile(path, []byte(`{"role":"user","content":"from a file"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := h.app.Extract(ctx, app.ExtractOptions{TranscriptPath: path}); err != nil {
		t.Fatal(err)
	}
	second, err := h.app.Extract(ctx, app.ExtractOptions{TranscriptPath: path})
	if err != nil {
		t.Fatal(err)
	}
	if fake.calls != 1 || second.SkippedReason != "no_new_user_turns" {
		t.Fatalf("the file cursor did not survive: %+v calls=%d", second, fake.calls)
	}
}

// failingVault fails every write, like a full disk or a read-only store.
type failingVault struct {
	vault.Vault
	err error
}

func (v *failingVault) Create(context.Context, memory.Entry) (memory.Entry, error) {
	return memory.Entry{}, v.err
}

func (v *failingVault) Update(context.Context, memory.Entry) error { return v.err }

func TestExtractStopsOnAWriteFailureAndRetriesWithoutDuplicates(t *testing.T) {
	h := newHarness(t, "obsidian")
	ctx := context.Background()
	fake := &extractLLM{payload: `{"candidates":[{"scope":"lesson","title":"A retried lesson","body":"The rule. Why it matters."}]}`}
	h.app.LLM = fake
	transcript := `{"role":"user","content":"extract this durable rule"}`
	options := app.ExtractOptions{Transcript: transcript, SessionID: "s-retry"}

	real := h.app.Vault
	h.app.Vault = &failingVault{Vault: real, err: errors.New("disk full")}
	report, err := h.app.Extract(ctx, options)
	if err == nil || !strings.Contains(err.Error(), "disk full") {
		t.Fatalf("the write failure was swallowed: %v", err)
	}
	if len(report.Skipped) != 0 {
		t.Fatalf("the write failure was reported as a skip: %+v", report)
	}
	if report.Accepted != 0 || report.Added != 0 {
		t.Fatalf("the failed candidate was counted: %+v", report)
	}
	if _, statErr := os.Stat(filepath.Join(h.root, ".extract-cursor.json")); !os.IsNotExist(statErr) {
		t.Fatal("the cursor advanced past an unwritten candidate")
	}

	h.app.Vault = real
	recovered, err := h.app.Extract(ctx, options)
	if err != nil {
		t.Fatal(err)
	}
	if recovered.Accepted != 1 || recovered.Added != 1 || fake.calls != 2 {
		t.Fatalf("the retry did not persist the candidate: %+v calls=%d", recovered, fake.calls)
	}
	entries, err := real.Entries(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("the retry duplicated the entry: %+v", entries)
	}

	third, err := h.app.Extract(ctx, options)
	if err != nil {
		t.Fatal(err)
	}
	if fake.calls != 2 || third.SkippedReason != "no_new_user_turns" {
		t.Fatalf("the recovered cursor did not advance: %+v calls=%d", third, fake.calls)
	}
}
