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
	for _, command := range []string{"search", "brief", "add", "pin", "unpin", "prune-worktrees", "log", "index", "install", "doctor", "path", "migrate", "version"} {
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

func TestAddPinnedFlag(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", t.TempDir())

	code, out, errOut := run(t, "--vault", "obsidian", "--memory-home", root,
		"add", "--scope", "lesson", "--title", "A pinned rule",
		"--body", "The rule. Why it matters.", "--native", "qwen", "--pinned")
	if code != 0 {
		t.Fatalf("the exit code is %d: %s %s", code, out, errOut)
	}
	pinned := filepath.Join(root, "native", "qwen", "pinned", "a-pinned-rule.md")
	if _, err := os.Stat(pinned); err != nil {
		t.Fatalf("the pinned projection was not written: %v", err)
	}
}

func TestPinnedEntryRefusesModification(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", t.TempDir())

	args := []string{"--vault", "obsidian", "--memory-home", root,
		"add", "--scope", "lesson", "--title", "A pinned rule",
		"--body", "The rule.", "--pinned"}
	if code, _, errOut := run(t, args...); code != 0 {
		t.Fatalf("cannot create the pinned entry: %d %s", code, errOut)
	}
	code, _, errOut := run(t, "--vault", "obsidian", "--memory-home", root,
		"add", "--scope", "lesson", "--title", "A pinned rule", "--body", "Another body.")
	if code != 1 || !strings.Contains(errOut, "pinned") {
		t.Fatalf("the pinned entry was modified: %d %q", code, errOut)
	}
	code, _, errOut = run(t, "--vault", "obsidian", "--memory-home", root,
		"add", "--scope", "lesson", "--title", "A pinned rule", "--body", "Another body.", "--force")
	if code != 0 {
		t.Fatalf("the forced write failed: %d %s", code, errOut)
	}
}

func TestPinAndUnpinCommands(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", t.TempDir())

	if code, _, errOut := run(t, "--vault", "obsidian", "--memory-home", root,
		"add", "--scope", "lesson", "--title", "A movable rule",
		"--body", "The rule.", "--native", "qwen"); code != 0 {
		t.Fatalf("cannot create the entry: %d %s", code, errOut)
	}
	code, out, errOut := run(t, "--vault", "obsidian", "--memory-home", root, "pin", "A movable rule")
	if code != 0 || !strings.Contains(out, "pinned:") {
		t.Fatalf("pin failed: %d %q %q", code, out, errOut)
	}
	pinned := filepath.Join(root, "native", "qwen", "pinned", "a-movable-rule.md")
	if _, err := os.Stat(pinned); err != nil {
		t.Fatalf("pin did not write the protected projection: %v", err)
	}
	code, out, errOut = run(t, "--vault", "obsidian", "--memory-home", root, "unpin", "A movable rule")
	if code != 0 || !strings.Contains(out, "unpinned:") {
		t.Fatalf("unpin failed: %d %q %q", code, out, errOut)
	}
	if _, err := os.Stat(pinned); !os.IsNotExist(err) {
		t.Fatal("unpin left the protected projection")
	}
}

func TestPinWithoutTitleIsUsageError(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	code, _, errOut := run(t, "pin")
	if code != 2 || !strings.Contains(errOut, "title") {
		t.Fatalf("the exit code is %d: %q", code, errOut)
	}
}

func newCLIRepo(t *testing.T) string {
	t.Helper()
	repo := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	return repo
}

func newCLIWorktree(t *testing.T, repo, name string) string {
	t.Helper()
	worktree := filepath.Join(t.TempDir(), name)
	gitDir := filepath.Join(repo, ".git", "worktrees", name)
	for _, dir := range []string{worktree, gitDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(gitDir, "commondir"), []byte("../..\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(worktree, ".git"), []byte("gitdir: "+gitDir+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return worktree
}

func TestAddAutoDetectsTheProjectFromTheCheckout(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	repo := newCLIRepo(t)
	t.Chdir(repo)

	code, out, errOut := run(t, "--vault", "obsidian", "--memory-home", root,
		"add", "--scope", "project", "--title", "A project decision", "--body", "The decision.")
	if code != 0 {
		t.Fatalf("the exit code is %d: %s %s", code, out, errOut)
	}
	note := filepath.Join(root, "corpus", "project", "a-project-decision.md")
	data, err := os.ReadFile(note)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "project: repo") || strings.Contains(string(data), "worktree:") {
		t.Fatalf("the auto scope is wrong:\n%s", data)
	}
}

func TestAddWorktreeAutoUsesTheLinkedWorktree(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	repo := newCLIRepo(t)
	worktree := newCLIWorktree(t, repo, "feature")
	t.Chdir(worktree)

	code, out, errOut := run(t, "--vault", "obsidian", "--memory-home", root,
		"add", "--scope", "project", "--worktree", "auto",
		"--title", "A branch decision", "--body", "The decision.")
	if code != 0 {
		t.Fatalf("the exit code is %d: %s %s", code, out, errOut)
	}
	note := filepath.Join(root, "corpus", "project", "a-branch-decision.md")
	data, err := os.ReadFile(note)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "project: repo") || !strings.Contains(string(data), "worktree: feature") {
		t.Fatalf("the worktree scope is wrong:\n%s", data)
	}
}

func TestBriefAutoDetectsTheRepository(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	repo := newCLIRepo(t)
	t.Chdir(repo)
	if code, _, errOut := run(t, "--vault", "obsidian", "--memory-home", root,
		"add", "--scope", "project", "--title", "A project decision", "--body", "The decision."); code != 0 {
		t.Fatalf("cannot create the entry: %s", errOut)
	}
	code, out, errOut := run(t, "--vault", "obsidian", "--memory-home", root, "brief")
	if code != 0 {
		t.Fatalf("the exit code is %d: %s", code, errOut)
	}
	if !strings.Contains(out, "## Project: repo") {
		t.Fatalf("the brief did not scope the repository:\n%s", out)
	}
	if strings.Contains(errOut, "not found") {
		t.Fatalf("auto-detection warned: %s", errOut)
	}
}

func TestBriefGlobalSkipsTheRepository(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	repo := newCLIRepo(t)
	t.Chdir(repo)
	if code, _, errOut := run(t, "--vault", "obsidian", "--memory-home", root,
		"add", "--scope", "project", "--title", "A project decision", "--body", "The decision."); code != 0 {
		t.Fatalf("cannot create the entry: %s", errOut)
	}
	code, out, errOut := run(t, "--vault", "obsidian", "--memory-home", root, "brief", "--global")
	if code != 0 {
		t.Fatalf("the exit code is %d: %s", code, errOut)
	}
	if strings.Contains(out, "## Project:") || strings.Contains(out, "## Worktree:") {
		t.Fatalf("--global still scoped the brief:\n%s", out)
	}
}

func TestBriefExplicitProjectStillWarns(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	code, _, errOut := run(t, "--vault", "obsidian", "--memory-home", root, "brief", "--project", "missing")
	if code != 0 {
		t.Fatalf("the exit code is %d", code)
	}
	if !strings.Contains(errOut, "project 'missing' not found") {
		t.Fatalf("the explicit project did not warn: %q", errOut)
	}
}

func TestPruneWorktreesDefaultsToDryRun(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	repo := newCLIRepo(t)
	worktree := newCLIWorktree(t, repo, "feature")
	t.Chdir(worktree)
	if code, _, errOut := run(t, "--vault", "obsidian", "--memory-home", root,
		"add", "--scope", "project", "--worktree", "auto",
		"--title", "A branch decision", "--body", "The decision."); code != 0 {
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

	code, out, errOut := run(t, "--vault", "obsidian", "--memory-home", root, "prune-worktrees")
	if code != 0 {
		t.Fatalf("the exit code is %d: %s", code, errOut)
	}
	if !strings.Contains(out, "entry: repo@feature") || !strings.Contains(out, "note: repo@feature") {
		t.Fatalf("the dry run did not list the scope:\n%s", out)
	}
	if !strings.Contains(out, "run again with --apply") {
		t.Fatalf("the dry run did not ask for --apply:\n%s", out)
	}
	if _, err := os.Stat(note); err != nil {
		t.Fatalf("the dry run removed the note: %v", err)
	}
}

func TestPruneWorktreesApplyRemovesTheScope(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	repo := newCLIRepo(t)
	worktree := newCLIWorktree(t, repo, "feature")
	t.Chdir(worktree)
	if code, _, errOut := run(t, "--vault", "obsidian", "--memory-home", root,
		"add", "--scope", "project", "--worktree", "auto",
		"--title", "A branch decision", "--body", "The decision."); code != 0 {
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
	if _, err := os.Stat(note); !os.IsNotExist(err) {
		t.Fatal("the worktree note survived --apply")
	}
	entries := filepath.Join(root, "corpus", "project")
	files, err := os.ReadDir(entries)
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if len(files) != 0 {
		t.Fatalf("the corpus entry survived --apply: %v", files)
	}
}

func TestPruneWorktreesOutsideAGitRepositoryFails(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	t.Chdir(t.TempDir())
	code, _, errOut := run(t, "--vault", "obsidian", "--memory-home", root, "prune-worktrees")
	if code != 1 || !strings.Contains(errOut, "git repository") {
		t.Fatalf("the exit code is %d: %q", code, errOut)
	}
}

func TestAddTeamWritesToTheRepository(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	repo := newCLIRepo(t)
	t.Chdir(repo)
	root := filepath.Join(t.TempDir(), "missing-store")

	code, out, errOut := run(t, "--vault", "obsidian", "--memory-home", root,
		"add", "--scope", "lesson", "--team",
		"--title", "A shared rule", "--body", "The rule. Why it matters.")
	if code != 0 {
		t.Fatalf("the exit code is %d: %s %s", code, out, errOut)
	}
	primary := filepath.Join(repo, ".agents", "memory", "corpus", "lesson", "a-shared-rule.md")
	if _, err := os.Stat(primary); err != nil {
		t.Fatalf("the team note was not written: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "corpus")); !os.IsNotExist(err) {
		t.Fatal("team add created the private store")
	}
}

func TestAddTeamRejectsUserScope(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	repo := newCLIRepo(t)
	t.Chdir(repo)
	code, _, errOut := run(t, "--vault", "obsidian", "--memory-home", t.TempDir(),
		"add", "--scope", "user", "--team", "--title", "A person", "--body", "The person.")
	if code != 1 || !strings.Contains(errOut, "personal user profile") {
		t.Fatalf("the exit code is %d: %q", code, errOut)
	}
}

func TestAddTeamBlocksASecret(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	repo := newCLIRepo(t)
	t.Chdir(repo)
	code, _, errOut := run(t, "--vault", "obsidian", "--memory-home", t.TempDir(),
		"add", "--scope", "lesson", "--team", "--title", "A leaked key",
		"--body", "ghp_"+strings.Repeat("a", 36))
	if code != 1 || !strings.Contains(errOut, "GitHub PAT") {
		t.Fatalf("the secret was accepted: %d %q", code, errOut)
	}
	if _, err := os.Stat(filepath.Join(repo, ".agents", "memory")); !os.IsNotExist(err) {
		t.Fatal("the blocked team write created the store")
	}
}

func TestBriefCarriesTheTeamMemory(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	repo := newCLIRepo(t)
	t.Chdir(repo)
	root := t.TempDir()
	if code, _, errOut := run(t, "--vault", "obsidian", "--memory-home", root,
		"add", "--scope", "lesson", "--team",
		"--title", "A shared rule", "--body", "The shared body."); code != 0 {
		t.Fatalf("cannot create the team entry: %s", errOut)
	}
	code, out, errOut := run(t, "--vault", "obsidian", "--memory-home", root, "brief")
	if code != 0 {
		t.Fatalf("the exit code is %d: %s", code, errOut)
	}
	if !strings.Contains(out, "## Team Memory (repo)") || !strings.Contains(out, "The shared body.") {
		t.Fatalf("the brief is missing team memory:\n%s", out)
	}
}

func TestSearchPrefixesTeamHits(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	repo := newCLIRepo(t)
	t.Chdir(repo)
	root := t.TempDir()
	if code, _, errOut := run(t, "--vault", "obsidian", "--memory-home", root,
		"add", "--scope", "lesson", "--team",
		"--title", "A shared rule", "--body", "The shared body."); code != 0 {
		t.Fatalf("cannot create the team entry: %s", errOut)
	}
	code, out, errOut := run(t, "--vault", "obsidian", "--memory-home", root, "search", "shared")
	if code != 0 {
		t.Fatalf("the exit code is %d: %s", code, errOut)
	}
	if !strings.Contains(out, "[team] .agents/memory/corpus/lesson/a-shared-rule.md") {
		t.Fatalf("the team hit is not prefixed:\n%s", out)
	}
}

func TestBriefAndSearchWorkWithOnlyTeamMemory(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	repo := newCLIRepo(t)
	t.Chdir(repo)
	root := filepath.Join(t.TempDir(), "missing-store")
	if code, _, errOut := run(t, "--vault", "obsidian", "--memory-home", root,
		"add", "--scope", "lesson", "--team",
		"--title", "A shared rule", "--body", "The shared body."); code != 0 {
		t.Fatalf("cannot create the team entry: %s", errOut)
	}

	code, out, errOut := run(t, "--vault", "obsidian", "--memory-home", root, "brief")
	if code != 0 {
		t.Fatalf("brief without a private store: %d %s", code, errOut)
	}
	if !strings.Contains(out, "## Team Memory (repo)") || !strings.Contains(out, "The shared body.") {
		t.Fatalf("the brief lost the team memory:\n%s", out)
	}

	code, out, errOut = run(t, "--vault", "obsidian", "--memory-home", root, "search", "shared")
	if code != 0 {
		t.Fatalf("search without a private store: %d %s", code, errOut)
	}
	if !strings.Contains(out, "[team] .agents/memory/corpus/lesson/a-shared-rule.md") {
		t.Fatalf("the search lost the team hit:\n%s", out)
	}
}

func TestBriefGlobalShowsTeamMemory(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	repo := newCLIRepo(t)
	t.Chdir(repo)
	root := filepath.Join(t.TempDir(), "missing-store")
	if code, _, errOut := run(t, "--vault", "obsidian", "--memory-home", root,
		"add", "--scope", "lesson", "--team",
		"--title", "A shared rule", "--body", "The shared body."); code != 0 {
		t.Fatalf("cannot create the team entry: %s", errOut)
	}
	code, out, errOut := run(t, "--vault", "obsidian", "--memory-home", root, "brief", "--global")
	if code != 0 {
		t.Fatalf("the exit code is %d: %s", code, errOut)
	}
	if !strings.Contains(out, "## Team Memory (repo)") {
		t.Fatalf("--global hid the team memory:\n%s", out)
	}
}

func TestNotionIsRejected(t *testing.T) {
	code, _, errOut := run(t, "--vault", "notion", "path")
	if code == 0 || !strings.Contains(errOut, "0.2.0") {
		t.Fatalf("code %d, error %q", code, errOut)
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

func TestTheVersionFlagAnswersWithoutAStore(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	code, out, errOut := run(t, "--version")
	if code != 0 {
		t.Fatalf("the exit code is %d: %s", code, errOut)
	}
	if strings.TrimSpace(out) == "" {
		t.Fatal("the version is empty")
	}
}

func TestTheHelpAnswersWithoutAStore(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	code, out, _ := run(t, "--help")
	if code != 0 {
		t.Fatalf("the exit code is %d", code)
	}
	if !strings.Contains(out, "Usage:") {
		t.Fatalf("the help is missing: %q", out)
	}
}
