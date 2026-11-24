package teammemory

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lindoelio/smart-agents-memory/internal/memory"
	"github.com/lindoelio/smart-agents-memory/internal/native"
)

func newRepo(t *testing.T) string {
	t.Helper()
	repo := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	return repo
}

func TestAddWritesThePrimaryAndTheQwenProjection(t *testing.T) {
	repo := newRepo(t)
	store := Open(repo)
	e := memory.Entry{
		Title: "A shared rule", Type: memory.TypeLesson, Body: "Rule. Why it matters.",
		Description: "A shared rule", Native: "minimax-agent",
	}
	result, err := store.Add(context.Background(), e, AddOptions{ProjectQwen: true})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Created {
		t.Fatalf("the write was not created: %+v", result)
	}
	primary := filepath.Join(repo, ".agents", "memory", "corpus", "lesson", "a-shared-rule.md")
	data, err := os.ReadFile(primary)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "Rule. Why it matters.") {
		t.Fatalf("the primary note lost the body:\n%s", data)
	}
	index, err := os.ReadFile(filepath.Join(repo, ".agents", "memory", "MEMORY.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(index), "[A shared rule](corpus/lesson/a-shared-rule.md)") {
		t.Fatalf("the primary index is %q", index)
	}

	qwen := filepath.Join(repo, ".qwen", "team-memory", "lesson", "a-shared-rule.md")
	qdata, err := os.ReadFile(qwen)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(qdata), "name: A shared rule") {
		t.Fatalf("the qwen projection is %q", qdata)
	}
	qindex, err := os.ReadFile(filepath.Join(repo, ".qwen", "team-memory", "MEMORY.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(qindex), "[A shared rule](lesson/a-shared-rule.md)") {
		t.Fatalf("the qwen index is %q", qindex)
	}

	entries, err := store.Entries(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Title != "A shared rule" {
		t.Fatalf("the team entries are %+v", entries)
	}
}

func TestQwenOnlyTeamMemoryIsReadable(t *testing.T) {
	repo := newRepo(t)
	qwen := filepath.Join(repo, ".qwen", "team-memory", "project")
	if err := os.MkdirAll(qwen, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(qwen, "decision.md"), []byte(native.RenderQwen(memory.Entry{
		Title: "A Qwen decision", Type: memory.TypeProject, Body: "The decision.",
		Description: "A decision", NativeType: "reference",
	})), 0o644); err != nil {
		t.Fatal(err)
	}
	entries, err := Open(repo).Entries(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Title != "A Qwen decision" {
		t.Fatalf("the Qwen entry is %+v", entries)
	}
	if entries[0].Source != "qwen-team-memory" {
		t.Fatalf("the source is %q", entries[0].Source)
	}
}

func TestExistingQwenDirectoryGetsProjection(t *testing.T) {
	repo := newRepo(t)
	if err := os.MkdirAll(filepath.Join(repo, ".qwen", "team-memory"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(repo).Add(context.Background(), memory.Entry{
		Title: "A shared rule", Type: memory.TypeLesson, Body: "Rule.",
	}, AddOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(repo, ".qwen", "team-memory", "lesson", "a-shared-rule.md")); err != nil {
		t.Fatalf("the existing Qwen directory did not receive the projection: %v", err)
	}
}

func TestAddingAnExistingTitleOverwritesItsQwenProjection(t *testing.T) {
	repo := newRepo(t)
	store := Open(repo)
	first := memory.Entry{Title: "A shared rule", Type: memory.TypeLesson, Body: "First."}
	if _, err := store.Add(context.Background(), first, AddOptions{ProjectQwen: true}); err != nil {
		t.Fatal(err)
	}
	second := memory.Entry{Title: "A shared rule", Type: memory.TypeLesson, Body: "Second."}
	if _, err := store.Add(context.Background(), second, AddOptions{ProjectQwen: true}); err != nil {
		t.Fatal(err)
	}
	matches, err := filepath.Glob(filepath.Join(repo, ".qwen", "team-memory", "lesson", "a-shared-rule*.md"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 1 {
		t.Fatalf("the Qwen projection has %d files: %v", len(matches), matches)
	}
	data, err := os.ReadFile(matches[0])
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "Second.") || !strings.Contains(string(data), "First.") {
		t.Fatalf("the append did not reach the Qwen projection:\n%s", data)
	}
}

func TestAddRejectsAUserEntry(t *testing.T) {
	_, err := Open(newRepo(t)).Add(context.Background(), memory.Entry{
		Title: "A person", Type: memory.TypeUser, Body: "The person.",
	}, AddOptions{})
	if err == nil || !strings.Contains(err.Error(), "personal user profile") {
		t.Fatalf("the error is %v", err)
	}
}

func TestAddRejectsASecret(t *testing.T) {
	repo := newRepo(t)
	_, err := Open(repo).Add(context.Background(), memory.Entry{
		Title: "A leaked key", Type: memory.TypeLesson,
		Body: "token ghp_" + strings.Repeat("a", 36),
	}, AddOptions{})
	if err == nil || !strings.Contains(err.Error(), "GitHub PAT") {
		t.Fatalf("the secret was accepted: %v", err)
	}
	if _, err := os.Stat(filepath.Join(repo, ".agents", "memory")); !os.IsNotExist(err) {
		t.Fatal("a rejected team write created the store")
	}
}

func TestSymlinkTeamRootIsRejected(t *testing.T) {
	repo := newRepo(t)
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(repo, ".agents")); err != nil {
		t.Fatal(err)
	}
	store := Open(repo)
	if _, err := store.Add(context.Background(), memory.Entry{
		Title: "A rule", Type: memory.TypeLesson, Body: "Rule.",
	}, AddOptions{}); err == nil {
		t.Fatal("a symlinked .agents directory was accepted")
	}
	if _, err := store.Entries(context.Background()); err == nil {
		t.Fatal("a symlinked .agents directory was read")
	}
}

func TestSymlinkFileIsIgnoredOnRead(t *testing.T) {
	repo := newRepo(t)
	root := filepath.Join(repo, ".agents", "memory", "lesson")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "outside.md")
	if err := os.WriteFile(outside, []byte("---\ntitle: Outside\ntype: lesson\n---\n\nBody.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "outside.md")); err != nil {
		t.Fatal(err)
	}
	entries, err := Open(repo).Entries(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("a symlinked note was read: %+v", entries)
	}
}

func TestSymlinkQwenRootIsRejected(t *testing.T) {
	repo := newRepo(t)
	outside := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, ".qwen"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(repo, ".qwen", "team-memory")); err != nil {
		t.Fatal(err)
	}
	_, err := Open(repo).Add(context.Background(), memory.Entry{
		Title: "A rule", Type: memory.TypeLesson, Body: "Rule.",
	}, AddOptions{ProjectQwen: true})
	if err == nil {
		t.Fatal("a symlinked Qwen team root was accepted")
	}
}
