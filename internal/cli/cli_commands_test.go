package cli_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/lindoelio/smart-agents-memory/internal/cli"
)

// runContext executes the command line with a context, for the commands that
// wait on cancellation.
func runContext(t *testing.T, ctx context.Context, args ...string) (int, string, string) {
	t.Helper()
	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}
	code := cli.Run(ctx, args, out, errOut)
	return code, out.String(), errOut.String()
}

// writeTranscript puts one finished turn where the cursor source discovers it.
func writeTranscript(t *testing.T, home string) string {
	t.Helper()
	path := filepath.Join(home, ".cursor", "projects", "p", "agent-transcripts", "s", "s.jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	line := `{"role":"user","content":"Remember the sync CLI rule. Why: it must resume."}` + "\n"
	if err := os.WriteFile(path, []byte(line), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// writeLegacyNote writes a corpus note in the previous vocabulary.
func writeLegacyNote(t *testing.T, root, dir, name, text string) string {
	t.Helper()
	path := filepath.Join(root, "corpus", dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestAGlobalFlagParseErrorReturnsTwo(t *testing.T) {
	if code, _, _ := run(t, "--nonsense"); code != 2 {
		t.Fatalf("the exit code is %d, want 2", code)
	}
}

func TestCommandFlagParseErrorsReturnTwo(t *testing.T) {
	commands := [][]string{
		{"search", "--nope"},
		{"brief", "--nope"},
		{"add", "--nope"},
		{"pin", "--nope"},
		{"unpin", "--nope"},
		{"prune-worktrees", "--nope"},
		{"recall", "--nope"},
		{"mcp", "--nope"},
		{"dream", "--nope"},
		{"extract", "--nope"},
		{"watch", "--nope"},
		{"sync", "--nope"},
		{"log", "--nope"},
		{"index", "--nope"},
		{"install", "--nope"},
		{"doctor", "--nope"},
		{"path", "--nope"},
		{"migrate", "--nope"},
	}
	for _, args := range commands {
		code, _, errOut := run(t, args...)
		if code != 2 {
			t.Fatalf("%v: the exit code is %d, want 2: %s", args, code, errOut)
		}
	}
}

func TestAnUnknownBackendIsReported(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	code, _, errOut := run(t, "--vault", "nonsense", "path")
	if code != 1 || !strings.Contains(errOut, "unknown vault backend") {
		t.Fatalf("the exit code is %d: %q", code, errOut)
	}
}

func TestCommandsThatNeedAStoreReportIt(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	root := filepath.Join(t.TempDir(), "missing")
	commands := [][]string{
		{"add", "--title", "A rule", "--body", "The rule."},
		{"pin", "A rule"},
		{"unpin", "A rule"},
		{"prune-worktrees"},
		{"index"},
		{"sync"},
		{"dream"},
	}
	for _, args := range commands {
		full := append([]string{"--vault", "obsidian", "--memory-home", root}, args...)
		code, _, errOut := run(t, full...)
		if code != 1 || !strings.Contains(errOut, "no memory store") {
			t.Fatalf("%v: the exit code is %d: %q", args, code, errOut)
		}
	}
}

func TestPathWithTheSQLiteGateway(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	database := filepath.Join(t.TempDir(), "memory.db")
	code, out, errOut := run(t, "--vault", "sqlite", "--database", database, "path")
	if code != 0 {
		t.Fatalf("the exit code is %d: %s", code, errOut)
	}
	if strings.TrimSpace(out) != database {
		t.Fatalf("the path is %q, want %q", strings.TrimSpace(out), database)
	}
}

func TestLogPrintsTheWriteLogNewestFirst(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	if code, _, errOut := run(t, "--vault", "obsidian", "--memory-home", root,
		"add", "--scope", "lesson", "--title", "A logged rule", "--body", "The rule."); code != 0 {
		t.Fatalf("cannot create the entry: %s", errOut)
	}
	code, out, errOut := run(t, "--vault", "obsidian", "--memory-home", root, "log")
	if code != 0 {
		t.Fatalf("the exit code is %d: %s", code, errOut)
	}
	if !strings.Contains(out, "add | lesson | A logged rule") {
		t.Fatalf("the log is %q", out)
	}
	code, out, errOut = run(t, "--vault", "obsidian", "--memory-home", root, "log", "--limit", "1")
	if code != 0 || len(strings.Split(strings.TrimSpace(out), "\n")) != 1 {
		t.Fatalf("the limited log is %q: %s", out, errOut)
	}
}

func TestSearchFailsWhenTheCorpusCannotBeRead(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	if err := os.WriteFile(filepath.Join(root, "corpus"), []byte("not a folder"), 0o644); err != nil {
		t.Fatal(err)
	}
	code, _, errOut := run(t, "--vault", "disk", "--memory-home", root, "search", "anything")
	if code != 1 {
		t.Fatalf("the exit code is %d, want 1: %q", code, errOut)
	}
}

func TestMCPFailsWhenStdinCannotBeRead(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	closed, err := os.CreateTemp(t.TempDir(), "closed")
	if err != nil {
		t.Fatal(err)
	}
	if err := closed.Close(); err != nil {
		t.Fatal(err)
	}
	original := os.Stdin
	os.Stdin = closed
	defer func() { os.Stdin = original }()

	code, _, errOut := run(t, "--vault", "obsidian", "--memory-home", t.TempDir(), "mcp")
	if code != 1 || !strings.Contains(errOut, "closed") {
		t.Fatalf("the exit code is %d: %q", code, errOut)
	}
}

func TestSearchWithAZeroLimitOmitsTheMatches(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	writeLegacyNote(t, root, "lesson", "note.md", `---
title: "A limit rule"
type: lesson
date: 2026-11-07
---

## A limit rule

The needle is here.
`)
	code, out, errOut := run(t, "--vault", "obsidian", "--memory-home", root, "search", "--limit", "0", "needle")
	if code != 0 {
		t.Fatalf("the exit code is %d: %s", code, errOut)
	}
	if !strings.Contains(out, "more matches omitted") {
		t.Fatalf("the omitted notice is missing: %q", out)
	}
}

// failingWriter fails every write, to exercise the encode error path.
type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errWriteFailed }

var errWriteFailed = errors.New("the writer failed")

func TestSyncReportsAJSONEncodingFailure(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	errOut := &bytes.Buffer{}
	code := cli.Run(context.Background(),
		[]string{"--vault", "obsidian", "--memory-home", root, "sync", "--json"},
		failingWriter{}, errOut)
	if code != 1 || !strings.Contains(errOut.String(), "the writer failed") {
		t.Fatalf("the exit code is %d: %q", code, errOut.String())
	}
}

func TestIndexReconcilesAndThenIdles(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	writeLegacyNote(t, root, "lesson", "projected.md", `---
title: "A projected rule"
type: lesson
date: 2026-11-07
---

## A projected rule

The rule.
`)

	code, out, errOut := run(t, "--vault", "obsidian", "--memory-home", root, "index")
	if code != 0 {
		t.Fatalf("the exit code is %d: %s", code, errOut)
	}
	if !strings.Contains(out, "projected to native: 1") {
		t.Fatalf("the first index is %q", out)
	}

	code, out, errOut = run(t, "--vault", "obsidian", "--memory-home", root, "index")
	if code != 0 {
		t.Fatalf("the second exit code is %d: %s", code, errOut)
	}
	if !strings.Contains(out, "nothing to reconcile") {
		t.Fatalf("the second index is %q", out)
	}
}

func TestIndexImportsANewerProjection(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	if code, _, errOut := run(t, "--vault", "obsidian", "--memory-home", root,
		"add", "--scope", "lesson", "--title", "An imported rule", "--body", "The original body."); code != 0 {
		t.Fatalf("cannot create the entry: %s", errOut)
	}
	if code, _, errOut := run(t, "--vault", "obsidian", "--memory-home", root, "index"); code != 0 {
		t.Fatalf("cannot export: %s", errOut)
	}

	projections, err := filepath.Glob(filepath.Join(root, "native", "minimax-agent", "topics", "*.md"))
	if err != nil || len(projections) != 1 {
		t.Fatalf("the projection is %v: %v", projections, err)
	}
	data, err := os.ReadFile(projections[0])
	if err != nil {
		t.Fatal(err)
	}
	edited := strings.Replace(string(data), "The original body.", "The edited body.", 1)
	if err := os.WriteFile(projections[0], []byte(edited), 0o644); err != nil {
		t.Fatal(err)
	}
	future := time.Now().Add(3 * time.Second)
	if err := os.Chtimes(projections[0], future, future); err != nil {
		t.Fatal(err)
	}

	notes, err := filepath.Glob(filepath.Join(root, "corpus", "lesson", "*.md"))
	if err != nil || len(notes) != 1 {
		t.Fatalf("the corpus note is %v: %v", notes, err)
	}
	if err := os.Remove(notes[0]); err != nil {
		t.Fatal(err)
	}

	code, out, errOut := run(t, "--vault", "obsidian", "--memory-home", root, "index")
	if code != 0 {
		t.Fatalf("the exit code is %d: %s", code, errOut)
	}
	if !strings.Contains(out, "imported from native: 1") {
		t.Fatalf("the index did not import: %q", out)
	}
	notes, err = filepath.Glob(filepath.Join(root, "corpus", "*", "*.md"))
	if err != nil || len(notes) != 1 {
		t.Fatalf("the imported corpus note is %v: %v", notes, err)
	}
	imported, err := os.ReadFile(notes[0])
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(imported), "The edited body.") {
		t.Fatalf("the imported note is %q", imported)
	}
}

func TestMigrateReportsAppliesAndIsIdempotent(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", t.TempDir())

	legacy := writeLegacyNote(t, root, "lesson", "legacy.md", `---
title: "A legacy lesson"
type: licao
date: 2025-01-01
source: memoria-cli
---

## A legacy lesson

The body.
`)
	merged := writeLegacyNote(t, root, "user", "nome.md", `---
title: "Nome"
type: usuario
date: 2025-01-01
source: memoria-cli
---

## Nome

The name.
`)
	orphan := filepath.Join(root, "native", "minimax-agent", "topics", "orphan.md")
	if err := os.MkdirAll(filepath.Dir(orphan), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(orphan, []byte("# no frontmatter\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	hot := filepath.Join(root, "MEMORY.md")
	if err := os.WriteFile(hot, []byte("<!-- memoria:hot:start -->\nprofile\n<!-- memoria:hot:end -->\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	code, out, errOut := run(t, "--vault", "obsidian", "--memory-home", root, "migrate")
	if code != 0 {
		t.Fatalf("the dry run exit code is %d: %s", code, errOut)
	}
	for _, want := range []string{
		"type licao -> lesson",
		"source memoria-cli -> smart-agents-memory-cli",
		"merged into \"Name\" and removed",
		"orphan projection removed",
		"hot tier markers renamed",
		"run again with --apply",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("the dry run is missing %q:\n%s", want, out)
		}
	}
	for _, path := range []string{merged, orphan} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("the dry run removed %s: %v", path, err)
		}
	}

	code, out, errOut = run(t, "--vault", "obsidian", "--memory-home", root, "migrate", "--apply")
	if code != 0 {
		t.Fatalf("the apply exit code is %d: %s %s", code, out, errOut)
	}
	data, err := os.ReadFile(legacy)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "type: lesson") || !strings.Contains(string(data), "smart-agents-memory-cli") {
		t.Fatalf("the migrated note is:\n%s", data)
	}
	for _, path := range []string{merged, orphan} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("the apply left %s: %v", path, err)
		}
	}
	hotData, err := os.ReadFile(hot)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(hotData), "smart-agents-memory:hot:start") {
		t.Fatalf("the hot tier was not migrated:\n%s", hotData)
	}

	code, out, errOut = run(t, "--vault", "obsidian", "--memory-home", root, "migrate")
	if code != 0 || !strings.Contains(out, "nothing to migrate") {
		t.Fatalf("the second run is %d %q: %s", code, out, errOut)
	}
}

func TestMigrateWithoutAStoreFinishes(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	code, out, errOut := run(t, "--vault", "obsidian", "--memory-home", t.TempDir(), "migrate")
	if code != 0 || !strings.Contains(out, "nothing to migrate") {
		t.Fatalf("the exit code is %d: %q %q", code, out, errOut)
	}
}

func TestMCPJSONRPCSession(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	stdio := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize"}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"memory_search","arguments":{"query":"anything"}}}`,
		`{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"memory_tree","arguments":{}}}`,
		`{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"unknown_tool","arguments":{}}}`,
		`{"jsonrpc":"2.0","id":6,"method":"tools/call","params":"bad"}`,
		`{"jsonrpc":"2.0","id":7,"method":"nope"}`,
		`not json`,
		``,
		"",
	}, "\n")

	code, out, errOut := runStdin(t, stdio, "--vault", "obsidian", "--memory-home", root, "mcp")
	if code != 0 {
		t.Fatalf("the exit code is %d: %s", code, errOut)
	}
	for _, want := range []string{
		`"protocolVersion":"2024-11-05"`,
		`"memory_add"`,
		`"revision:`,
		`"isError":true`,
		`unknown tool`,
		`invalid params`,
		`method not found`,
		`parse error`,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("the session is missing %q:\n%s", want, out)
		}
	}
}

func TestMCPRequiresAValidBackend(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	code, _, errOut := runStdin(t, "", "--vault", "nonsense", "mcp")
	if code != 1 || !strings.Contains(errOut, "unknown vault backend") {
		t.Fatalf("the exit code is %d: %q", code, errOut)
	}
}

func TestSyncSeedsThenResumes(t *testing.T) {
	root := t.TempDir()
	home := t.TempDir()
	t.Setenv("HOME", home)
	writeTranscript(t, home)

	code, out, errOut := run(t, "--vault", "obsidian", "--memory-home", root, "sync")
	if code != 0 {
		t.Fatalf("the seed exit code is %d: %s %s", code, out, errOut)
	}
	if !strings.Contains(out, "seeded=1") || !strings.Contains(out, "sources=") {
		t.Fatalf("the seed report is %q", out)
	}

	code, out, errOut = run(t, "--vault", "obsidian", "--memory-home", root, "sync")
	if code != 0 {
		t.Fatalf("the resume exit code is %d: %s %s", code, out, errOut)
	}
	if !strings.Contains(out, "changed=0") {
		t.Fatalf("the resume report is %q", out)
	}
	if strings.Contains(out, "seeded=") {
		t.Fatalf("the resume reported a seed: %q", out)
	}
}

func TestSyncSourceFilterAndUnknownSource(t *testing.T) {
	root := t.TempDir()
	home := t.TempDir()
	t.Setenv("HOME", home)
	writeTranscript(t, home)
	calls := 0
	server := llmServer(t, `{"candidates":[{"scope":"lesson","title":"A synced rule","body":"The rule. Why it matters."}]}`, &calls)
	t.Setenv("SMART_AGENTS_MEMORY_LLM_URL", server.URL)

	code, out, errOut := run(t, "--vault", "obsidian", "--memory-home", root, "sync", "--backfill", "--source", "cursor")
	if code != 0 {
		t.Fatalf("the filtered sync exit code is %d: %s %s", code, out, errOut)
	}
	if !strings.Contains(out, "added=1") || calls != 1 {
		t.Fatalf("the filtered sync is %q calls=%d", out, calls)
	}

	code, _, errOut = run(t, "--vault", "obsidian", "--memory-home", root, "sync", "--source", "bogus")
	if code != 1 || !strings.Contains(errOut, "unknown source") {
		t.Fatalf("the unknown source is %d: %q", code, errOut)
	}
}

func TestSyncSinceVariants(t *testing.T) {
	root := t.TempDir()
	home := t.TempDir()
	t.Setenv("HOME", home)
	writeTranscript(t, home)

	invalid := []string{"not-a-duration", "0d", "-2d", "0s", "xd"}
	for _, value := range invalid {
		code, _, errOut := run(t, "--vault", "obsidian", "--memory-home", root, "sync", "--since", value)
		if code != 2 || !strings.Contains(errOut, "invalid duration") {
			t.Fatalf("%s: the exit code is %d: %q", value, code, errOut)
		}
	}

	for _, value := range []string{"1d", "24h", ""} {
		code, _, errOut := run(t, "--vault", "obsidian", "--memory-home", root, "sync", "--since", value)
		if code != 0 {
			t.Fatalf("%s: the exit code is %d: %s", value, code, errOut)
		}
	}
}

func TestSyncQuietSuppressesItsReport(t *testing.T) {
	root := t.TempDir()
	home := t.TempDir()
	t.Setenv("HOME", home)
	writeTranscript(t, home)
	code, out, errOut := run(t, "--vault", "obsidian", "--memory-home", root, "sync", "--quiet")
	if code != 0 {
		t.Fatalf("the exit code is %d: %s", code, errOut)
	}
	if strings.TrimSpace(out) != "" {
		t.Fatalf("the quiet sync printed %q", out)
	}
}

func TestSyncQuietFailsOnAFileError(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads a mode 000 file")
	}
	root := t.TempDir()
	home := t.TempDir()
	t.Setenv("HOME", home)
	path := writeTranscript(t, home)
	if err := os.Chmod(path, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(path, 0o644) })

	code, out, errOut := run(t, "--vault", "obsidian", "--memory-home", root, "sync", "--backfill")
	if code != 1 {
		t.Fatalf("the exit code is %d: %q %q", code, out, errOut)
	}
	if !strings.Contains(errOut, "warning:") {
		t.Fatalf("the file error was not reported: %q", errOut)
	}

	code, out, errOut = run(t, "--vault", "obsidian", "--memory-home", root, "sync", "--backfill", "--quiet")
	if code != 1 {
		t.Fatalf("the exit code is %d: %q %q", code, out, errOut)
	}
	if strings.TrimSpace(out) != "" {
		t.Fatalf("the quiet sync printed %q", out)
	}
	if !strings.Contains(errOut, "warning:") {
		t.Fatalf("the file error was not reported: %q", errOut)
	}
}

func TestWatchLoopExitsWhenTheContextIsCanceled(t *testing.T) {
	root := t.TempDir()
	home := t.TempDir()
	t.Setenv("HOME", home)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	code, _, errOut := runContext(t, ctx, "--vault", "obsidian", "--memory-home", root, "watch")
	if code != 0 {
		t.Fatalf("the exit code is %d: %q", code, errOut)
	}
	if !strings.Contains(errOut, "watch is deprecated") {
		t.Fatalf("the deprecation warning is %q", errOut)
	}
}

func TestWatchLoopRequiresAStore(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	root := filepath.Join(t.TempDir(), "missing")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	code, _, errOut := runContext(t, ctx, "--vault", "obsidian", "--memory-home", root, "watch")
	if code != 1 || !strings.Contains(errOut, "no memory store") {
		t.Fatalf("the exit code is %d: %q", code, errOut)
	}
}

func TestInstallDryRunNeedsASchedulerFlag(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	code, _, errOut := run(t, "--vault", "obsidian", "--memory-home", t.TempDir(), "install", "--dry-run")
	if code != 2 || !strings.Contains(errOut, "--dry-run requires --scheduler or --no-scheduler") {
		t.Fatalf("the exit code is %d: %q", code, errOut)
	}
}

func TestInstallThenDoctorIsHealthy(t *testing.T) {
	root := t.TempDir()
	home := t.TempDir()
	t.Setenv("HOME", home)

	code, out, errOut := run(t, "--vault", "obsidian", "--memory-home", root, "install")
	if code != 0 {
		t.Fatalf("the install exit code is %d: %s %s", code, out, errOut)
	}
	if !strings.Contains(out, "store: "+root) || !strings.Contains(out, "[ok] skill:") {
		t.Fatalf("the install output is %q", out)
	}
	if _, err := os.Stat(filepath.Join(root, "corpus")); err != nil {
		t.Fatalf("the install did not create the store: %v", err)
	}

	code, out, errOut = run(t, "--vault", "obsidian", "--memory-home", root, "doctor")
	if code != 0 {
		t.Fatalf("the doctor exit code is %d: %s %s", code, out, errOut)
	}
	if !strings.Contains(out, "no problems found") {
		t.Fatalf("the doctor output is %q", out)
	}
}

func TestDoctorReportsAMissingStore(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	root := filepath.Join(t.TempDir(), "missing")
	code, _, errOut := run(t, "--vault", "obsidian", "--memory-home", root, "doctor")
	if code != 1 {
		t.Fatalf("the exit code is %d, want 1", code)
	}
	if !strings.Contains(errOut, "no memory store") || !strings.Contains(errOut, "problem(s)") {
		t.Fatalf("the doctor did not report the store: %q", errOut)
	}
}

func TestDreamDryRunPrintsTheSuffix(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	if code, _, errOut := run(t, "--vault", "obsidian", "--memory-home", root,
		"add", "--scope", "lesson", "--title", "A dry dream", "--body", "The rule."); code != 0 {
		t.Fatalf("cannot create the entry: %s", errOut)
	}
	code, out, errOut := run(t, "--vault", "obsidian", "--memory-home", root,
		"dream", "--deterministic-only", "--dry-run")
	if code != 0 {
		t.Fatalf("the exit code is %d: %s", code, errOut)
	}
	if !strings.Contains(out, "(dry run)") {
		t.Fatalf("the dream output is %q", out)
	}
}

func TestExtractWithoutAnLLMIsReported(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("SMART_AGENTS_MEMORY_LLM_URL", "")
	t.Setenv("SMART_AGENTS_MEMORY_LLM_MODEL", "")
	t.Setenv("SMART_AGENTS_MEMORY_LLM_API_KEY", "")

	code, _, errOut := runStdin(t, `{"role":"user","content":"a turn"}`,
		"--vault", "obsidian", "--memory-home", root, "extract", "--stdin")
	if code != 1 || !strings.Contains(errOut, "needs an LLM client") {
		t.Fatalf("the exit code is %d: %q", code, errOut)
	}
}

func TestExtractMissingTranscriptFileIsReported(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	missing := filepath.Join(t.TempDir(), "missing.jsonl")
	code, _, errOut := run(t, "--vault", "obsidian", "--memory-home", t.TempDir(),
		"extract", "--transcript", missing)
	if code != 1 {
		t.Fatalf("the exit code is %d, want 1: %q", code, errOut)
	}
	if !strings.Contains(errOut, "no such file") {
		t.Fatalf("the message is %q", errOut)
	}
}

func TestExtractTranscriptFileAndQuiet(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	calls := 0
	server := llmServer(t, `{"candidates":[]}`, &calls)
	t.Setenv("SMART_AGENTS_MEMORY_LLM_URL", server.URL)

	transcript := filepath.Join(t.TempDir(), "turn.jsonl")
	if err := os.WriteFile(transcript, []byte(`{"role":"user","content":"a finished turn"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	code, out, errOut := run(t, "--vault", "obsidian", "--memory-home", root,
		"extract", "--transcript", transcript)
	if code != 0 {
		t.Fatalf("the exit code is %d: %s %s", code, out, errOut)
	}
	if !strings.Contains(out, "extracted: candidates=0 added=0") {
		t.Fatalf("the report is %q", out)
	}

	code, out, errOut = run(t, "--vault", "obsidian", "--memory-home", root,
		"extract", "--transcript", transcript, "--quiet")
	if code != 0 {
		t.Fatalf("the quiet exit code is %d: %s", code, errOut)
	}
	if strings.TrimSpace(out) != "" {
		t.Fatalf("the quiet extraction printed %q", out)
	}
}

func TestBriefCarriesTheQueryResults(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	if code, _, errOut := run(t, "--vault", "obsidian", "--memory-home", root,
		"add", "--scope", "lesson", "--title", "A searchable brief", "--body", "The needle."); code != 0 {
		t.Fatalf("cannot create the entry: %s", errOut)
	}
	code, out, errOut := run(t, "--vault", "obsidian", "--memory-home", root,
		"brief", "--global", "--query", "needle")
	if code != 0 {
		t.Fatalf("the exit code is %d: %s", code, errOut)
	}
	if !strings.Contains(out, "A searchable brief") {
		t.Fatalf("the query results are missing: %q", out)
	}
}

func TestSearchReportsNothingFound(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	code, out, errOut := run(t, "--vault", "obsidian", "--memory-home", root, "search", "absent")
	if code != 0 {
		t.Fatalf("the exit code is %d: %s", code, errOut)
	}
	if !strings.Contains(out, "nothing found") {
		t.Fatalf("the output is %q", out)
	}
}

func TestRecallWithTheLLMSelector(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	if code, _, errOut := run(t, "--vault", "obsidian", "--memory-home", root,
		"add", "--scope", "lesson", "--title", "A selected rule", "--body", "The selected body.",
		"--category", "testing_standard"); code != 0 {
		t.Fatalf("cannot create the entry: %s", errOut)
	}
	calls := 0
	server := llmServer(t, `{"refs":["lesson:a-selected-rule"]}`, &calls)
	t.Setenv("SMART_AGENTS_MEMORY_LLM_URL", server.URL)

	code, out, errOut := run(t, "--vault", "obsidian", "--memory-home", root,
		"recall", "--query", "selected", "--llm")
	if code != 0 {
		t.Fatalf("the exit code is %d: %s", code, errOut)
	}
	if calls != 1 || !strings.Contains(out, "The selected body.") {
		t.Fatalf("the recall is %q calls=%d", out, calls)
	}
}

func TestPruneWorktreesNothingToPrune(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	repo := newCLIRepo(t)
	t.Chdir(repo)
	code, out, errOut := run(t, "--vault", "obsidian", "--memory-home", root, "prune-worktrees")
	if code != 0 {
		t.Fatalf("the exit code is %d: %s", code, errOut)
	}
	if !strings.Contains(out, "nothing to prune") {
		t.Fatalf("the output is %q", out)
	}
}

func TestPruneWorktreesProtectsPinnedEntries(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	repo := newCLIRepo(t)
	worktree := newCLIWorktree(t, repo, "feature")
	t.Chdir(worktree)
	if code, _, errOut := run(t, "--vault", "obsidian", "--memory-home", root,
		"add", "--scope", "project", "--worktree", "auto", "--pinned",
		"--title", "A pinned branch decision", "--body", "The decision."); code != 0 {
		t.Fatalf("cannot create the entry: %s", errOut)
	}
	note := filepath.Join(root, "projects", "repo@feature.md")
	if err := os.MkdirAll(filepath.Dir(note), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(note, []byte("branch note"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(repo, ".git", "worktrees", "feature")); err != nil {
		t.Fatal(err)
	}
	t.Chdir(repo)

	code, out, errOut := run(t, "--vault", "obsidian", "--memory-home", root, "prune-worktrees", "--apply")
	if code != 0 {
		t.Fatalf("the exit code is %d: %s %s", code, out, errOut)
	}
	if !strings.Contains(out, "protected: repo@feature :: A pinned branch decision (pinned)") {
		t.Fatalf("the protected entry is missing:\n%s", out)
	}
	if !strings.Contains(out, "protected note: repo@feature") {
		t.Fatalf("the protected note is missing:\n%s", out)
	}
	if !strings.Contains(out, "nothing removable; 1 protected item(s).") {
		t.Fatalf("the summary is missing:\n%s", out)
	}
	if _, err := os.Stat(note); err != nil {
		t.Fatalf("the protected note was removed: %v", err)
	}
}

func TestPinAndAddUsageErrors(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if code, _, _ := run(t, "add", "--body", "no title"); code != 2 {
		t.Fatalf("add without a title returned %d", code)
	}
	if code, _, _ := run(t, "unpin"); code != 2 {
		t.Fatalf("unpin without a title returned %d", code)
	}

	root := t.TempDir()
	code, _, errOut := run(t, "--vault", "obsidian", "--memory-home", root, "pin", "An absent rule")
	if code != 1 {
		t.Fatalf("pinning an absent rule returned %d", code)
	}
	if !strings.Contains(errOut, "not found") {
		t.Fatalf("the message is %q", errOut)
	}
}

func TestHelpCommandPrintsUsage(t *testing.T) {
	code, out, _ := run(t, "help")
	if code != 0 || !strings.Contains(out, "Usage:") {
		t.Fatalf("the help command is %d %q", code, out)
	}
}

func TestAnInvalidBackendFailsEveryCommand(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	root := t.TempDir()
	commands := [][]string{
		{"search", "anything"},
		{"brief"},
		{"add", "--title", "A rule", "--body", "The rule."},
		{"pin", "A rule"},
		{"unpin", "A rule"},
		{"prune-worktrees"},
		{"recall", "--tree"},
		{"dream"},
		{"watch"},
		{"sync"},
		{"log"},
		{"index"},
		{"install"},
		{"doctor"},
		{"path"},
		{"migrate"},
	}
	for _, args := range commands {
		full := append([]string{"--vault", "nonsense", "--memory-home", root}, args...)
		code, _, errOut := run(t, full...)
		if code != 1 || !strings.Contains(errOut, "unknown vault backend") {
			t.Fatalf("%v: the exit code is %d: %q", args, code, errOut)
		}
	}

	code, _, errOut := runStdin(t, `{"role":"user","content":"a turn"}`,
		"--vault", "nonsense", "--memory-home", root, "extract", "--stdin")
	if code != 1 || !strings.Contains(errOut, "unknown vault backend") {
		t.Fatalf("extract: the exit code is %d: %q", code, errOut)
	}
}

func TestBriefWorktreeWithoutAProjectFails(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Chdir(t.TempDir())
	code, _, errOut := run(t, "--vault", "obsidian", "--memory-home", t.TempDir(),
		"brief", "--worktree", "feature")
	if code != 1 || !strings.Contains(errOut, "needs a project") {
		t.Fatalf("the exit code is %d: %q", code, errOut)
	}
}

func TestRecallUnknownRefFails(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	code, _, errOut := run(t, "--vault", "obsidian", "--memory-home", t.TempDir(),
		"recall", "--ref", "lesson:absent")
	if code != 1 || !strings.Contains(errOut, "not found") {
		t.Fatalf("the exit code is %d: %q", code, errOut)
	}
}

func TestDreamReportsAConcurrentRun(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	lock := filepath.Join(root, ".consolidation.lock")
	body := `{"pid":1,"started_at":"` + time.Now().Format(time.RFC3339Nano) + `"}`
	if err := os.WriteFile(lock, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	code, _, errOut := run(t, "--vault", "obsidian", "--memory-home", root, "dream")
	if code != 1 || !strings.Contains(errOut, "already running") {
		t.Fatalf("the exit code is %d: %q", code, errOut)
	}
}

func TestExtractFailsWhenStdinCannotBeRead(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	closed, err := os.CreateTemp(t.TempDir(), "closed")
	if err != nil {
		t.Fatal(err)
	}
	if err := closed.Close(); err != nil {
		t.Fatal(err)
	}
	original := os.Stdin
	os.Stdin = closed
	defer func() { os.Stdin = original }()

	code, _, errOut := run(t, "--vault", "obsidian", "--memory-home", t.TempDir(), "extract")
	if code != 1 || !strings.Contains(errOut, "closed") {
		t.Fatalf("the exit code is %d: %q", code, errOut)
	}
}

func TestExtractFailsWhenTheModelAnswerIsNotJSON(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	calls := 0
	server := llmServer(t, `not json`, &calls)
	t.Setenv("SMART_AGENTS_MEMORY_LLM_URL", server.URL)

	code, _, errOut := runStdin(t, `{"role":"user","content":"a turn"}`,
		"--vault", "obsidian", "--memory-home", root, "extract", "--stdin")
	if code != 1 || !strings.Contains(errOut, "not JSON") {
		t.Fatalf("the exit code is %d: %q", code, errOut)
	}
}

func TestExtractRequiresAStoreWithStdin(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	root := filepath.Join(t.TempDir(), "missing")
	code, _, errOut := runStdin(t, `{"role":"user","content":"a turn"}`,
		"--vault", "obsidian", "--memory-home", root, "extract", "--stdin")
	if code != 1 || !strings.Contains(errOut, "no memory store") {
		t.Fatalf("the exit code is %d: %q", code, errOut)
	}
}

func TestExtractReportsSkippedCandidatesAndTheDream(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	for i := 0; i < 10; i++ {
		title := "A seeded rule " + strconv.Itoa(i)
		if code, _, errOut := run(t, "--vault", "obsidian", "--memory-home", root,
			"add", "--scope", "lesson", "--title", title, "--body", "The rule."); code != 0 {
			t.Fatalf("cannot seed the entry: %s", errOut)
		}
	}
	calls := 0
	payload := `{"candidates":[{"scope":"lesson","title":"A leaked key","body":"ghp_` + strings.Repeat("a", 36) + `"}]}`
	server := llmServer(t, payload, &calls)
	t.Setenv("SMART_AGENTS_MEMORY_LLM_URL", server.URL)

	code, out, errOut := runStdin(t, `{"role":"user","content":"a turn"}`,
		"--vault", "obsidian", "--memory-home", root, "extract", "--stdin", "--auto-dream")
	if code != 0 {
		t.Fatalf("the exit code is %d: %s %s", code, out, errOut)
	}
	if !strings.Contains(out, "skipped:") {
		t.Fatalf("the skipped candidate is missing: %q", out)
	}
	if !strings.Contains(out, "dream: merged=") {
		t.Fatalf("the auto dream did not report: %q", out)
	}
}

func TestWatchReportsASyncError(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	lock := filepath.Join(root, ".sync.lock")
	body := `{"pid":1,"started_at":"` + time.Now().Format(time.RFC3339Nano) + `"}`
	if err := os.WriteFile(lock, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	code, _, errOut := runContext(t, ctx, "--vault", "obsidian", "--memory-home", root, "watch")
	if code != 0 {
		t.Fatalf("the exit code is %d: %q", code, errOut)
	}
	if !strings.Contains(errOut, "warning: sync is already running") {
		t.Fatalf("the warning is %q", errOut)
	}
}

func TestWatchReportsFileWarnings(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads a mode 000 file")
	}
	root := t.TempDir()
	home := t.TempDir()
	t.Setenv("HOME", home)
	path := writeTranscript(t, home)
	if code, _, errOut := run(t, "--vault", "obsidian", "--memory-home", root, "sync"); code != 0 {
		t.Fatalf("cannot seed the state: %s", errOut)
	}
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString(`{"role":"user","content":"another turn"}` + "\n"); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(path, 0o644) })

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	code, _, errOut := runContext(t, ctx, "--vault", "obsidian", "--memory-home", root, "watch")
	if code != 0 {
		t.Fatalf("the exit code is %d: %q", code, errOut)
	}
	if !strings.Contains(errOut, path) {
		t.Fatalf("the file warning is %q", errOut)
	}
}

func TestWatchRunsOnEveryTick(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	ctx, cancel := context.WithCancel(context.Background())
	timer := time.AfterFunc(60*time.Millisecond, cancel)
	defer timer.Stop()
	code, _, errOut := runContext(t, ctx, "--vault", "obsidian", "--memory-home", root,
		"watch", "--interval", "5ms")
	if code != 0 {
		t.Fatalf("the exit code is %d: %q", code, errOut)
	}
}

func TestLogFailsWhenTheLogFolderIsAFile(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	if err := os.WriteFile(filepath.Join(root, "log"), []byte("not a folder"), 0o644); err != nil {
		t.Fatal(err)
	}
	code, _, errOut := run(t, "--vault", "obsidian", "--memory-home", root, "log")
	if code != 1 {
		t.Fatalf("the exit code is %d, want 1: %q", code, errOut)
	}
}

func TestIndexFailsWhenTheNativeFolderIsAFile(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	if err := os.WriteFile(filepath.Join(root, "native"), []byte("not a folder"), 0o644); err != nil {
		t.Fatal(err)
	}
	code, _, errOut := run(t, "--vault", "obsidian", "--memory-home", root, "index")
	if code != 1 {
		t.Fatalf("the exit code is %d, want 1: %q", code, errOut)
	}
}

func TestIndexWorksOnTheSQLiteGateway(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	database := filepath.Join(t.TempDir(), "memory.db")
	if code, _, errOut := run(t, "--vault", "sqlite", "--database", database, "install"); code != 0 {
		t.Fatalf("cannot install the sqlite store: %s", errOut)
	}
	code, out, errOut := run(t, "--vault", "sqlite", "--database", database, "index")
	if code != 0 {
		t.Fatalf("the exit code is %d: %s", code, errOut)
	}
	if !strings.Contains(out, "index: ") || !strings.Contains(out, "(0 entries)") {
		t.Fatalf("the index report is %q", out)
	}
}

func TestIndexUpdatesAChangedProjection(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	if code, _, errOut := run(t, "--vault", "obsidian", "--memory-home", root,
		"add", "--scope", "lesson", "--title", "An edited rule", "--body", "The original body."); code != 0 {
		t.Fatalf("cannot create the entry: %s", errOut)
	}
	projections, err := filepath.Glob(filepath.Join(root, "native", "minimax-agent", "topics", "*.md"))
	if err != nil || len(projections) != 1 {
		t.Fatalf("the projection is %v: %v", projections, err)
	}
	data, err := os.ReadFile(projections[0])
	if err != nil {
		t.Fatal(err)
	}
	edited := strings.Replace(string(data), "The original body.", "The edited body.", 1)
	if err := os.WriteFile(projections[0], []byte(edited), 0o644); err != nil {
		t.Fatal(err)
	}
	future := time.Now().Add(3 * time.Second)
	if err := os.Chtimes(projections[0], future, future); err != nil {
		t.Fatal(err)
	}

	code, out, errOut := run(t, "--vault", "obsidian", "--memory-home", root, "index")
	if code != 0 {
		t.Fatalf("the exit code is %d: %s", code, errOut)
	}
	if !strings.Contains(out, "updated from native: 1") {
		t.Fatalf("the update was not reported: %q", out)
	}
}

func TestInstallFailsWhenTheNativeFolderIsAFile(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	if err := os.WriteFile(filepath.Join(root, "native"), []byte("not a folder"), 0o644); err != nil {
		t.Fatal(err)
	}
	code, _, errOut := run(t, "--vault", "obsidian", "--memory-home", root, "install")
	if code != 1 {
		t.Fatalf("the exit code is %d, want 1: %q", code, errOut)
	}
}

func TestInstallFailsWhenTheHookFolderIsAFile(t *testing.T) {
	root := t.TempDir()
	home := t.TempDir()
	t.Setenv("HOME", home)
	hooks := filepath.Join(home, ".local", "share", "smart-agents-memory", "hooks")
	if err := os.MkdirAll(filepath.Dir(hooks), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(hooks, []byte("not a folder"), 0o644); err != nil {
		t.Fatal(err)
	}
	code, _, errOut := run(t, "--vault", "obsidian", "--memory-home", root, "install", "--hooks")
	if code != 1 {
		t.Fatalf("the exit code is %d, want 1: %q", code, errOut)
	}
}

func TestSchedulerReadsTheConfiguredInterval(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	configPath := filepath.Join(home, ".config", "smart-agents-memory", "config.json")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, []byte(`{"sync_interval":"7m"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	code, out, errOut := run(t, "--vault", "obsidian", "--memory-home", t.TempDir(),
		"install", "--scheduler", "--dry-run")
	if code != 0 {
		t.Fatalf("the exit code is %d: %s %s", code, out, errOut)
	}
	if !strings.Contains(out, "[scheduler]") {
		t.Fatalf("the scheduler snippet is %q", out)
	}
}

func TestSchedulerInstallFailureIsReported(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("the launchd path is macOS only")
	}
	root := t.TempDir()
	home := t.TempDir()
	t.Setenv("HOME", home)
	blocker := filepath.Join(home, "Library")
	if err := os.WriteFile(blocker, []byte("not a folder"), 0o644); err != nil {
		t.Fatal(err)
	}
	code, _, errOut := run(t, "--vault", "obsidian", "--memory-home", root, "install", "--scheduler")
	if code != 1 {
		t.Fatalf("the exit code is %d, want 1: %q", code, errOut)
	}
}

func TestMigrateFailsWhenTheNoteCannotBeWritten(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root writes a read-only file")
	}
	root := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	note := writeLegacyNote(t, root, "lesson", "legacy.md", `---
title: "A read only lesson"
type: licao
date: 2025-01-01
source: memoria-cli
---

## A read only lesson

The body.
`)
	if err := os.Chmod(note, 0o444); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(note, 0o644) })

	code, _, errOut := run(t, "--vault", "obsidian", "--memory-home", root, "migrate", "--apply")
	if code != 1 || !strings.Contains(errOut, "permission denied") {
		t.Fatalf("the exit code is %d: %q", code, errOut)
	}
}

func TestPruneWorktreesProtectsHotEntries(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	repo := newCLIRepo(t)
	worktree := newCLIWorktree(t, repo, "feature")
	t.Chdir(worktree)
	if code, _, errOut := run(t, "--vault", "obsidian", "--memory-home", root,
		"add", "--scope", "project", "--worktree", "auto", "--tier", "hot",
		"--title", "A hot branch decision", "--body", "The decision."); code != 0 {
		t.Fatalf("cannot create the entry: %s", errOut)
	}
	if err := os.RemoveAll(filepath.Join(repo, ".git", "worktrees", "feature")); err != nil {
		t.Fatal(err)
	}
	t.Chdir(repo)

	code, out, errOut := run(t, "--vault", "obsidian", "--memory-home", root, "prune-worktrees")
	if code != 0 {
		t.Fatalf("the exit code is %d: %s %s", code, out, errOut)
	}
	if !strings.Contains(out, "(hot)") {
		t.Fatalf("the hot entry is not protected:\n%s", out)
	}
}
