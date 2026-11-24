// Package gitscope resolves the Git repository and linked worktree that
// contain a directory. It inspects the filesystem only: no git subprocess,
// no network, and no dependency on the working tree being clean.
//
// The project scope is the repository root. A linked worktree keeps its own
// identifier so a branch-local memory can be isolated without losing the
// repository-wide one.
package gitscope

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/lindoelio/smart-agents-memory/internal/memory"
)

// Info is the scope of a directory inside a Git repository.
type Info struct {
	// RepoRoot is the directory that holds the common .git directory.
	RepoRoot string
	// RepoSlug is the address of the repository in the corpus.
	RepoSlug string
	// WorktreeRoot is the directory that holds this checkout's .git marker.
	WorktreeRoot string
	// WorktreeID is the linked worktree name; it is empty in the main
	// checkout.
	WorktreeID string
}

// Resolve walks upwards from startDir looking for a .git directory or file.
// It returns false when startDir is not inside a Git repository.
func Resolve(startDir string) (Info, bool) {
	if strings.TrimSpace(startDir) == "" {
		startDir = "."
	}
	abs, err := filepath.Abs(startDir)
	if err != nil {
		return Info{}, false
	}
	if stat, err := os.Stat(abs); err == nil && !stat.IsDir() {
		abs = filepath.Dir(abs)
	}

	for dir := abs; ; dir = filepath.Dir(dir) {
		gitPath := filepath.Join(dir, ".git")
		stat, err := os.Lstat(gitPath)
		if err == nil {
			switch {
			case stat.IsDir():
				return mainCheckout(dir), true
			case stat.Mode().IsRegular():
				return linkedWorktree(dir, gitPath)
			default:
				return Info{}, false
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return Info{}, false
		}
	}
}

// ActiveWorktrees lists the linked worktrees registered in a repository.
// The main checkout is not in the map: an entry with an empty WorktreeID is
// repository-wide.
func ActiveWorktrees(repoRoot string) (map[string]bool, error) {
	out := map[string]bool{}
	entries, err := os.ReadDir(filepath.Join(repoRoot, ".git", "worktrees"))
	if err != nil {
		if os.IsNotExist(err) {
			return out, nil
		}
		return nil, err
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		out[memory.Slugify(entry.Name())] = true
	}
	return out, nil
}

// mainCheckout describes the checkout whose .git is a directory.
func mainCheckout(dir string) Info {
	return Info{
		RepoRoot:     dir,
		RepoSlug:     memory.Slugify(filepath.Base(dir)),
		WorktreeRoot: dir,
		WorktreeID:   "",
	}
}

// linkedWorktree describes a checkout whose .git is a gitdir pointer. The
// pointer names the administrative directory under the common repository;
// its parent is the repository root.
func linkedWorktree(worktreeRoot, gitPath string) (Info, bool) {
	data, err := os.ReadFile(gitPath)
	if err != nil {
		return Info{}, false
	}
	line := strings.TrimSpace(strings.SplitN(string(data), "\n", 2)[0])
	const prefix = "gitdir:"
	if !strings.HasPrefix(line, prefix) {
		return Info{}, false
	}
	gitDir := strings.TrimSpace(strings.TrimPrefix(line, prefix))
	if gitDir == "" {
		return Info{}, false
	}
	if !filepath.IsAbs(gitDir) {
		gitDir = filepath.Join(worktreeRoot, gitDir)
	}
	gitDir = filepath.Clean(gitDir)

	commonDir := commonDirectory(gitDir)
	repoRoot := filepath.Dir(commonDir)
	return Info{
		RepoRoot:     repoRoot,
		RepoSlug:     memory.Slugify(filepath.Base(repoRoot)),
		WorktreeRoot: worktreeRoot,
		WorktreeID:   memory.Slugify(filepath.Base(gitDir)),
	}, true
}

// commonDirectory resolves the common .git directory of a linked worktree.
// Git writes it in commondir; the parent of worktrees/<name> is the fallback
// for an older or hand-made registration.
func commonDirectory(gitDir string) string {
	data, err := os.ReadFile(filepath.Join(gitDir, "commondir"))
	if err == nil {
		common := strings.TrimSpace(string(data))
		if common != "" {
			if !filepath.IsAbs(common) {
				common = filepath.Join(gitDir, common)
			}
			return filepath.Clean(common)
		}
	}
	return filepath.Clean(filepath.Join(gitDir, "..", ".."))
}
