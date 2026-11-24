package gitscope

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/lindoelio/smart-agents-memory/internal/memory"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestResolveRejectsANonGitDirectory(t *testing.T) {
	if info, ok := Resolve(t.TempDir()); ok {
		t.Fatalf("a plain directory resolved as %+v", info)
	}
}

func TestResolveFindsAStandardCheckoutFromANestedDirectory(t *testing.T) {
	repo := filepath.Join(t.TempDir(), "My Repo")
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	nested := filepath.Join(repo, "src", "pkg")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}

	info, ok := Resolve(nested)
	if !ok {
		t.Fatal("the repository was not found")
	}
	wantSlug := memory.Slugify("My Repo")
	if info.RepoRoot != repo || info.WorktreeRoot != repo {
		t.Fatalf("the roots are %+v", info)
	}
	if info.RepoSlug != wantSlug {
		t.Fatalf("the slug is %q, want %q", info.RepoSlug, wantSlug)
	}
	if info.WorktreeID != "" {
		t.Fatalf("the main checkout has worktree id %q", info.WorktreeID)
	}
}

func TestResolveReadsALinkedWorktree(t *testing.T) {
	root := t.TempDir()
	repo := filepath.Join(root, "repo")
	worktree := filepath.Join(root, "feature-tree")
	gitDir := filepath.Join(repo, ".git", "worktrees", "Feature One")
	for _, dir := range []string{gitDir, worktree} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	writeFile(t, filepath.Join(gitDir, "commondir"), "../..\n")
	writeFile(t, filepath.Join(worktree, ".git"), "gitdir: "+gitDir+"\n")
	if err := os.MkdirAll(filepath.Join(worktree, "nested"), 0o755); err != nil {
		t.Fatal(err)
	}

	info, ok := Resolve(filepath.Join(worktree, "nested"))
	if !ok {
		t.Fatal("the linked worktree was not found")
	}
	if info.RepoRoot != repo {
		t.Fatalf("the repository root is %q, want %q", info.RepoRoot, repo)
	}
	if info.WorktreeRoot != worktree {
		t.Fatalf("the worktree root is %q, want %q", info.WorktreeRoot, worktree)
	}
	if info.RepoSlug != memory.Slugify("repo") {
		t.Fatalf("the repository slug is %q", info.RepoSlug)
	}
	if info.WorktreeID != memory.Slugify("Feature One") {
		t.Fatalf("the worktree id is %q", info.WorktreeID)
	}
}

func TestResolveFallsBackWhenCommondirIsMissing(t *testing.T) {
	root := t.TempDir()
	repo := filepath.Join(root, "repo")
	worktree := filepath.Join(root, "wt")
	gitDir := filepath.Join(repo, ".git", "worktrees", "wt")
	for _, dir := range []string{gitDir, worktree} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	writeFile(t, filepath.Join(worktree, ".git"), "gitdir: "+gitDir+"\n")

	info, ok := Resolve(worktree)
	if !ok {
		t.Fatal("the worktree was not found")
	}
	if info.RepoRoot != repo || info.WorktreeID != "wt" {
		t.Fatalf("the fallback resolved %+v", info)
	}
}

func TestActiveWorktreesListsTheLinkedWorktrees(t *testing.T) {
	repo := filepath.Join(t.TempDir(), "repo")
	for _, name := range []string{"feature", "Bug Fix"} {
		if err := os.MkdirAll(filepath.Join(repo, ".git", "worktrees", name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	writeFile(t, filepath.Join(repo, ".git", "worktrees", "not-a-worktree"), "a file is not a registration\n")

	active, err := ActiveWorktrees(repo)
	if err != nil {
		t.Fatal(err)
	}
	if !active["feature"] || !active[memory.Slugify("Bug Fix")] {
		t.Fatalf("the active worktrees are %+v", active)
	}
	if active["not-a-worktree"] {
		t.Fatal("a regular file became an active worktree")
	}
}

func TestActiveWorktreesWithoutRegistrationsIsEmpty(t *testing.T) {
	repo := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	active, err := ActiveWorktrees(repo)
	if err != nil {
		t.Fatal(err)
	}
	if len(active) != 0 {
		t.Fatalf("the active worktrees are %+v", active)
	}
}
