package app_test

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lindoelio/smart-agents-memory/internal/app"
	"github.com/lindoelio/smart-agents-memory/internal/extract"
	"github.com/lindoelio/smart-agents-memory/internal/sources"
	_ "modernc.org/sqlite"
)

func TestSyncRejectsAStoreThatIsNotReady(t *testing.T) {
	h := newHarness(t, "obsidian")
	h.app.Vault = nil
	if _, err := h.app.Sync(context.Background(), app.SyncOptions{}); err == nil {
		t.Fatal("sync accepted a nil vault")
	}
}

func TestSyncReportsAParseErrorWithoutAdvancing(t *testing.T) {
	h := newHarness(t, "obsidian")
	ctx := context.Background()
	path := writeCursorSession(t, h.home, `{not json}`)
	h.app.LLM = &extractLLM{payload: `{"candidates":[]}`}
	report, err := h.app.Sync(ctx, app.SyncOptions{Backfill: true, SourceID: "cursor"})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Errors) == 0 {
		t.Fatalf("the parse error was ignored: %+v", report)
	}
	state, err := sources.LoadState(filepath.Join(h.root, ".sync-state.json"))
	if err != nil {
		t.Fatal(err)
	}
	for key := range state.Files {
		if strings.Contains(key, "cursor") {
			t.Fatalf("the cursor advanced after a parse error: %v", state.Files)
		}
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
}

func TestSyncSinceSkipsOldFiles(t *testing.T) {
	h := newHarness(t, "obsidian")
	ctx := context.Background()
	path := writeCursorSession(t, h.home, `{"role":"user","content":"an old rule"}`)
	old := time.Now().Add(-48 * time.Hour)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}
	fake := &extractLLM{payload: `{"candidates":[]}`}
	h.app.LLM = fake
	report, err := h.app.Sync(ctx, app.SyncOptions{Backfill: true, Since: time.Hour, SourceID: "cursor"})
	if err != nil {
		t.Fatal(err)
	}
	if fake.calls != 0 {
		t.Fatalf("the old file was extracted: %+v", report)
	}
}

func TestSyncBackfillAfterSeedProcessesHistory(t *testing.T) {
	h := newHarness(t, "obsidian")
	ctx := context.Background()
	writeCursorSession(t, h.home, `{"role":"user","content":"history before seed"}`)
	fake := &extractLLM{payload: `{"candidates":[]}`}
	h.app.LLM = fake
	if _, err := h.app.Sync(ctx, app.SyncOptions{SourceID: "cursor"}); err != nil {
		t.Fatal(err)
	}
	if fake.calls != 0 {
		t.Fatalf("the seed called the model: %d", fake.calls)
	}
	if _, err := h.app.Sync(ctx, app.SyncOptions{Backfill: true, SourceID: "cursor"}); err != nil {
		t.Fatal(err)
	}
	if fake.calls != 1 {
		t.Fatalf("the backfill after seed called the model %d times", fake.calls)
	}
}

func TestSyncSkipsADatabaseWithoutSchema(t *testing.T) {
	h := newHarness(t, "obsidian")
	dbPath := filepath.Join(h.home, ".local", "share", "opencode", "opencode.db")
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE unrelated (id TEXT)`); err != nil {
		t.Fatal(err)
	}
	db.Close()
	report, err := h.app.Sync(context.Background(), app.SyncOptions{Backfill: true, SourceID: "opencode"})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Errors) == 0 {
		t.Fatalf("the missing schema was ignored: %+v", report)
	}
}

func TestSourceStatusesReadSavedStateAndDisabledSources(t *testing.T) {
	h := newHarness(t, "obsidian")
	if err := os.MkdirAll(filepath.Join(h.home, ".cursor", "projects"), 0o755); err != nil {
		t.Fatal(err)
	}
	h.app.Cfg.SyncDisabled = []string{"cursor"}
	state := sources.NewState()
	state.Sources["cursor"] = sources.SourceState{LastSync: time.Now(), Errors: []string{"old error"}}
	if err := sources.SaveState(filepath.Join(h.root, ".sync-state.json"), state); err != nil {
		t.Fatal(err)
	}
	statuses := h.app.SourceStatuses()
	for _, status := range statuses {
		if status.ID != "cursor" {
			continue
		}
		if status.Enabled {
			t.Fatal("the disabled source is enabled")
		}
		if status.LastSync.IsZero() || len(status.Errors) != 1 {
			t.Fatalf("the saved status was not loaded: %+v", status)
		}
		return
	}
	t.Fatal("the cursor source is missing")
}

func TestSyncDisabledSourceLeavesNoError(t *testing.T) {
	h := newHarness(t, "obsidian")
	h.app.Cfg.SyncDisabled = []string{"opencode"}
	report, err := h.app.Sync(context.Background(), app.SyncOptions{SourceID: "opencode"})
	if err != nil {
		t.Fatal(err)
	}
	if report.Sources != 0 {
		t.Fatalf("the disabled source was counted: %+v", report)
	}
}

func TestSyncReadErrorIsReported(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root can read a 000 file")
	}
	h := newHarness(t, "obsidian")
	path := writeCursorSession(t, h.home, `{"role":"user","content":"unreadable"}`)
	if err := os.Chmod(path, 0o000); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(path, 0o644)
	h.app.LLM = &extractLLM{payload: `{"candidates":[]}`}
	report, err := h.app.Sync(context.Background(), app.SyncOptions{Backfill: true, SourceID: "cursor"})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Errors) == 0 {
		t.Fatalf("the read error was ignored: %+v", report)
	}
}

func TestSyncAssistantOnlyFileAdvancesWithoutCalling(t *testing.T) {
	h := newHarness(t, "obsidian")
	fake := &extractLLM{payload: `{"candidates":[]}`}
	h.app.LLM = fake
	writeCursorSession(t, h.home, `{"role":"assistant","content":"only an answer"}`)
	if _, err := h.app.Sync(context.Background(), app.SyncOptions{Backfill: true, SourceID: "cursor"}); err != nil {
		t.Fatal(err)
	}
	if fake.calls != 0 {
		t.Fatalf("an assistant-only file called the model: %d", fake.calls)
	}
}

func TestSyncCustomWatchDirectory(t *testing.T) {
	h := newHarness(t, "obsidian")
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	if err := os.WriteFile(path, []byte(`{"role":"user","content":"a custom rule"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	h.app.Cfg.WatchDirs = []string{dir}
	fake := &extractLLM{payload: `{"candidates":[]}`}
	h.app.LLM = fake
	report, err := h.app.Sync(context.Background(), app.SyncOptions{Backfill: true, SourceID: "custom"})
	if err != nil {
		t.Fatal(err)
	}
	if fake.calls != 1 || report.Files != 1 {
		t.Fatalf("the custom source is %+v calls=%d", report, fake.calls)
	}
}

func TestSyncOpenCodeWithNoMessages(t *testing.T) {
	h := newHarness(t, "obsidian")
	dbPath := filepath.Join(h.home, ".local", "share", "opencode", "opencode.db")
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE session_message (session_id TEXT, seq INTEGER, type TEXT, data TEXT)`); err != nil {
		t.Fatal(err)
	}
	db.Close()
	report, err := h.app.Sync(context.Background(), app.SyncOptions{Backfill: true, SourceID: "opencode"})
	if err != nil {
		t.Fatal(err)
	}
	if report.Changed != 0 || len(report.Errors) != 0 {
		t.Fatalf("the empty db is %+v", report)
	}
}

func TestSyncRefusesConcurrentRuns(t *testing.T) {
	h := newHarness(t, "obsidian")
	lock, err := sources.AcquireLock(filepath.Join(h.root, ".sync.lock"), time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Release()
	if _, err := h.app.Sync(context.Background(), app.SyncOptions{}); err == nil {
		t.Fatal("a concurrent sync was accepted")
	}
}

func TestSyncAutoDreamAtThreshold(t *testing.T) {
	h := newHarness(t, "obsidian")
	ctx := context.Background()
	writeCursorSession(t, h.home, `{"role":"user","content":"a dream rule"}`)
	fake := &extractLLM{payload: `{"candidates":[],"operations":[]}`}
	h.app.LLM = fake
	if err := os.WriteFile(filepath.Join(h.root, ".smart-agents-memory-dream.json"), []byte(`{"version":1,"dirty_mutations":20}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := h.app.Sync(ctx, app.SyncOptions{Backfill: true, SourceID: "cursor", AutoDream: true}); err != nil {
		t.Fatal(err)
	}
}

func TestSyncSQLiteDryRun(t *testing.T) {
	h := newHarness(t, "obsidian")
	ctx := context.Background()
	dbPath := filepath.Join(h.home, ".local", "share", "opencode", "opencode.db")
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE session_message (session_id TEXT, seq INTEGER, type TEXT, data TEXT)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO session_message VALUES ('s1', 1, 'user', '{"text":"dry sqlite"}')`); err != nil {
		t.Fatal(err)
	}
	db.Close()
	fake := &extractLLM{payload: `{"candidates":[]}`}
	h.app.LLM = fake
	report, err := h.app.Sync(ctx, app.SyncOptions{Backfill: true, DryRun: true, SourceID: "opencode"})
	if err != nil {
		t.Fatal(err)
	}
	if fake.calls != 0 || report.Changed == 0 {
		t.Fatalf("the sqlite dry run is %+v calls=%d", report, fake.calls)
	}
}

func TestSyncSQLiteReportsMalformedData(t *testing.T) {
	h := newHarness(t, "obsidian")
	dbPath := filepath.Join(h.home, ".local", "share", "opencode", "opencode.db")
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE session_message (session_id TEXT, seq INTEGER, type TEXT, data TEXT)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO session_message VALUES ('s1', 1, 'user', '{')`); err != nil {
		t.Fatal(err)
	}
	db.Close()
	report, err := h.app.Sync(context.Background(), app.SyncOptions{Backfill: true, SourceID: "opencode"})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Errors) == 0 {
		t.Fatalf("the malformed sqlite data was ignored: %+v", report)
	}
}

func TestSyncSeedsPerSource(t *testing.T) {
	h := newHarness(t, "obsidian")
	ctx := context.Background()
	writeCursorSession(t, h.home, `{"role":"user","content":"cursor history"}`)
	qwenPath := filepath.Join(h.home, ".qwen", "projects", "p", "chats", "s.jsonl")
	if err := os.MkdirAll(filepath.Dir(qwenPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(qwenPath, []byte(`{"role":"user","content":"qwen history"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	fake := &extractLLM{payload: `{"candidates":[]}`}
	h.app.LLM = fake
	if _, err := h.app.Sync(ctx, app.SyncOptions{SourceID: "cursor"}); err != nil {
		t.Fatal(err)
	}
	if fake.calls != 0 {
		t.Fatalf("the cursor seed called the model: %d", fake.calls)
	}
	state, err := sources.LoadState(filepath.Join(h.root, ".sync-state.json"))
	if err != nil {
		t.Fatal(err)
	}
	if state.Seeded {
		t.Fatal("the global state was marked seeded after one source")
	}
	if _, err := h.app.Sync(ctx, app.SyncOptions{}); err != nil {
		t.Fatal(err)
	}
	if fake.calls != 0 {
		t.Fatalf("seeding qwen called the model: %d", fake.calls)
	}
	f, err := os.OpenFile(qwenPath, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(`{"role":"user","content":"a new qwen rule"}` + "\n"); err != nil {
		f.Close()
		t.Fatal(err)
	}
	f.Close()
	if _, err := h.app.Sync(ctx, app.SyncOptions{}); err != nil {
		t.Fatal(err)
	}
	if fake.calls != 1 {
		t.Fatalf("the new qwen turn called the model %d times", fake.calls)
	}
}

func TestSyncMigratesTheLegacyCursor(t *testing.T) {
	h := newHarness(t, "obsidian")
	ctx := context.Background()
	path := writeCursorSession(t, h.home, `{"role":"user","content":"already extracted"}`, `{"role":"assistant","content":"noted"}`)
	legacy := extract.CursorStore{Sessions: map[string]extract.SessionCursor{
		path: {SessionID: path, TranscriptPath: path, ProcessedOffset: 2},
	}}
	if err := extract.Save(filepath.Join(h.root, ".extract-cursor.json"), legacy); err != nil {
		t.Fatal(err)
	}
	fake := &extractLLM{payload: `{"candidates":[]}`}
	h.app.LLM = fake
	report, err := h.app.Sync(ctx, app.SyncOptions{SourceID: "cursor"})
	if err != nil {
		t.Fatal(err)
	}
	if report.Migrated != 1 || fake.calls != 0 {
		t.Fatalf("the legacy cursor was not migrated: %+v calls=%d", report, fake.calls)
	}
	state, err := sources.LoadState(filepath.Join(h.root, ".sync-state.json"))
	if err != nil {
		t.Fatal(err)
	}
	for key, file := range state.Files {
		if strings.Contains(key, "cursor") && file.Offset != file.Size {
			t.Fatalf("the migrated cursor is %+v", file)
		}
	}
	if _, err := h.app.Sync(ctx, app.SyncOptions{SourceID: "cursor"}); err != nil {
		t.Fatal(err)
	}
	if fake.calls != 0 {
		t.Fatalf("the migrated history was extracted: %d", fake.calls)
	}
}

func TestSyncReadsTheGrokHistoryFallback(t *testing.T) {
	h := newHarness(t, "obsidian")
	path := filepath.Join(h.home, ".grok", "sessions", "cwd", "session", "chat_history.jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(
		`{"type":"system","content":"system prompt"}`+"\n"+
			`{"type":"user","content":"remember the grok fallback"}`+"\n"+
			`{"type":"assistant","content":"noted"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	fake := &extractLLM{payload: `{"candidates":[{"scope":"lesson","title":"Grok fallback","body":"The rule. Why it matters."}]}`}
	h.app.LLM = fake
	report, err := h.app.Sync(context.Background(), app.SyncOptions{Backfill: true, SourceID: "grok"})
	if err != nil {
		t.Fatal(err)
	}
	if fake.calls != 1 || report.Added != 1 {
		t.Fatalf("the grok fallback is %+v calls=%d", report, fake.calls)
	}
}

func TestSyncReadsTheAntigravityTranscriptFallback(t *testing.T) {
	h := newHarness(t, "obsidian")
	path := filepath.Join(h.home, ".gemini", "antigravity-cli", "brain", "id", ".system_generated", "logs", "transcript.jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(
		`{"type":"USER_INPUT","content":"<USER_REQUEST>remember the agy fallback</USER_REQUEST>"}`+"\n"+
			`{"type":"PLANNER_RESPONSE","content":"noted"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	fake := &extractLLM{payload: `{"candidates":[{"scope":"lesson","title":"Agy fallback","body":"The rule. Why it matters."}]}`}
	h.app.LLM = fake
	report, err := h.app.Sync(context.Background(), app.SyncOptions{Backfill: true, SourceID: "antigravity"})
	if err != nil {
		t.Fatal(err)
	}
	if fake.calls != 1 || report.Added != 1 {
		t.Fatalf("the antigravity fallback is %+v calls=%d", report, fake.calls)
	}
}

func TestDoctorShowsSeedAndBackfillState(t *testing.T) {
	h := newHarness(t, "obsidian")
	ctx := context.Background()
	writeCursorSession(t, h.home, `{"role":"user","content":"a backfilled rule"}`)
	h.app.LLM = &extractLLM{payload: `{"candidates":[]}`}
	if _, err := h.app.Sync(ctx, app.SyncOptions{Backfill: true, SourceID: "cursor"}); err != nil {
		t.Fatal(err)
	}
	report, err := h.app.Doctor(ctx)
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(report.Notes, "\n")
	if !strings.Contains(joined, "seed: done") || !strings.Contains(joined, "backfill: done") {
		t.Fatalf("the doctor notes are %q", joined)
	}
}

func TestDoctorShowsPendingSeed(t *testing.T) {
	h := newHarness(t, "obsidian")
	report, err := h.app.Doctor(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(report.Notes, "\n")
	if !strings.Contains(joined, "seed: pending") || !strings.Contains(joined, "backfill: not run") {
		t.Fatalf("the doctor notes are %q", joined)
	}
}
