package cli_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lindoelio/smart-agents-memory/internal/cli"

	_ "github.com/lindoelio/smart-agents-memory/internal/vault/disk"
	_ "github.com/lindoelio/smart-agents-memory/internal/vault/obsidian"
	_ "github.com/lindoelio/smart-agents-memory/internal/vault/sqlite"
)

// run executes the command line and returns the code and the output.
func run(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}
	code := cli.Run(context.Background(), args, out, errOut)
	return code, out.String(), errOut.String()
}

func TestTheHelpListsTheCommands(t *testing.T) {
	code, out, _ := run(t)
	if code != 0 {
		t.Fatalf("the exit code is %d", code)
	}
	for _, command := range []string{"search", "brief", "add", "log", "index", "install", "doctor", "path", "migrate", "version"} {
		if !strings.Contains(out, command) {
			t.Errorf("the help does not mention %q", command)
		}
	}
}

func TestVersion(t *testing.T) {
	code, out, _ := run(t, "version")
	if code != 0 {
		t.Fatalf("the exit code is %d", code)
	}
	if strings.TrimSpace(out) == "" {
		t.Fatal("the version is empty")
	}
}

func TestTheLegacyFlagsGetAPointer(t *testing.T) {
	cases := map[string]string{
		"--escopo":  "--scope",
		"--titulo":  "--title",
		"--corpo":   "--body",
		"--projeto": "--project",
		"--fonte":   "--source",
	}
	for legacy, current := range cases {
		code, _, errOut := run(t, "add", legacy, "x", "--body", "y")
		if code != 2 {
			t.Fatalf("%s: the exit code is %d, want 2", legacy, code)
		}
		if !strings.Contains(errOut, current) {
			t.Fatalf("%s: the message does not point to %s: %q", legacy, current, errOut)
		}
	}
}

func TestSearchPrintsTheMatches(t *testing.T) {
	root := t.TempDir()
	home := t.TempDir()
	t.Setenv("HOME", home)

	corpus := filepath.Join(root, "corpus", "lesson")
	if err := os.MkdirAll(corpus, 0o755); err != nil {
		t.Fatal(err)
	}
	note := "---\ntitle: \"pipefail aborts the script\"\ntype: lesson\ndate: 2026-11-07\n---\n\n## pipefail aborts the script\n\nUnder set -euo pipefail the script aborts.\n"
	if err := os.WriteFile(filepath.Join(corpus, "pipefail.md"), []byte(note), 0o644); err != nil {
		t.Fatal(err)
	}

	code, out, errOut := run(t, "--vault", "obsidian", "--memory-home", root, "search", "pipefail")
	if code != 0 {
		t.Fatalf("the exit code is %d: %s", code, errOut)
	}
	if !strings.Contains(out, "corpus/lesson/pipefail.md") {
		t.Fatalf("the output does not carry the path: %q", out)
	}
	if !strings.Contains(out, "aborts") {
		t.Fatalf("the output does not carry the match: %q", out)
	}
}

func TestSearchWithoutAQueryFails(t *testing.T) {
	code, _, errOut := run(t, "search")
	if code != 2 {
		t.Fatalf("the exit code is %d, want 2", code)
	}
	if !strings.Contains(errOut, "query") {
		t.Fatalf("the message does not mention the query: %q", errOut)
	}
}

func TestAnUnknownCommandFails(t *testing.T) {
	code, _, errOut := run(t, "nonsense")
	if code != 2 {
		t.Fatalf("the exit code is %d, want 2", code)
	}
	if !strings.Contains(errOut, "unknown command") {
		t.Fatalf("the message is %q", errOut)
	}
}

func TestPathPrintsTheStore(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	code, out, errOut := run(t, "--vault", "obsidian", "--memory-home", root, "path")
	if code != 0 {
		t.Fatalf("the exit code is %d: %s", code, errOut)
	}
	if strings.TrimSpace(out) != root {
		t.Fatalf("the path is %q, want %q", strings.TrimSpace(out), root)
	}
}
