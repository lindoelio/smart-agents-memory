// Package teammemory is the Git-backed team store: a corpus inside the
// repository, shared through the normal Git workflow. The primary layout is
// .agents/memory/; .qwen/team-memory/ is an optional Qwen projection.
package teammemory

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/lindoelio/smart-agents-memory/internal/memory"
	"github.com/lindoelio/smart-agents-memory/internal/native"
	"github.com/lindoelio/smart-agents-memory/internal/secretguard"
)

const (
	primaryDir = ".agents/memory"
	qwenDir    = ".qwen/team-memory"
	indexName  = "MEMORY.md"
)

// Store is the team memory of one worktree.
type Store struct {
	worktreeRoot string
}

// Open builds the team store rooted at the active worktree.
func Open(worktreeRoot string) *Store {
	return &Store{worktreeRoot: worktreeRoot}
}

// AddOptions tune one team write.
type AddOptions struct {
	// Force appends even when the body is already present.
	Force bool
	// ProjectQwen writes the Qwen projection even when its directory does
	// not exist yet.
	ProjectQwen bool
}

// WriteResult describes what a team write did.
type WriteResult struct {
	Entry    memory.Entry
	Created  bool
	Appended bool
}

// Entries returns the primary entries and the Qwen projection entries that
// the primary does not already contain. It never follows a symlink.
func (s *Store) Entries(ctx context.Context) ([]memory.Entry, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	primary, err := s.readPrimary()
	if err != nil {
		return nil, err
	}
	qwen, err := s.readQwen()
	if err != nil {
		return nil, err
	}
	byTitle := map[string]bool{}
	out := make([]memory.Entry, 0, len(primary)+len(qwen))
	for _, e := range primary {
		byTitle[memory.NormTitle(e.Title)] = true
		out = append(out, e)
	}
	for _, e := range qwen {
		if byTitle[memory.NormTitle(e.Title)] {
			continue
		}
		byTitle[memory.NormTitle(e.Title)] = true
		out = append(out, e)
	}
	sort.SliceStable(out, func(i, j int) bool {
		ti, tj := typeIndex(out[i].Type), typeIndex(out[j].Type)
		if ti != tj {
			return ti < tj
		}
		return strings.ToLower(out[i].Title) < strings.ToLower(out[j].Title)
	})
	return out, nil
}

// Add writes one team entry, refreshes the primary index and, when the Qwen
// directory exists or was requested, the Qwen projection.
func (s *Store) Add(ctx context.Context, e memory.Entry, opts AddOptions) (WriteResult, error) {
	var result WriteResult
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if e.Type == memory.TypeUser {
		return result, fmt.Errorf("team memory cannot store personal user profile entries; use lesson, project, or reference")
	}
	if !e.Type.Valid() {
		return result, fmt.Errorf("invalid entry type %q", e.Type)
	}
	if strings.TrimSpace(e.Title) == "" || strings.TrimSpace(e.Body) == "" {
		return result, fmt.Errorf("team memory needs a title and a body")
	}
	if err := secretguard.Check(e.Title, e.Body); err != nil {
		return result, err
	}
	qwenActive, err := s.qwenEnabled(opts.ProjectQwen)
	if err != nil {
		return result, err
	}

	primaryRoot, err := s.ensurePrimary()
	if err != nil {
		return result, err
	}
	primary, err := s.readPrimary()
	if err != nil {
		return result, err
	}
	var existing *memory.Entry
	key := memory.NormTitle(e.Title)
	for i := range primary {
		if memory.NormTitle(primary[i].Title) == key {
			existing = &primary[i]
			break
		}
	}

	entry := e
	if existing != nil {
		entry = *existing
		if strings.Contains(entry.Body, e.Body) && !opts.Force {
			if qwenActive {
				if err := s.writeQwenEntry(entry); err != nil {
					return result, err
				}
				if err := s.writeQwenIndex(); err != nil {
					return result, err
				}
			}
			result.Entry = entry
			return result, nil
		}
		entry.Body = strings.TrimRight(entry.Body, "\n") + "\n\n" + strings.TrimSpace(e.Body)
		if e.Description != "" {
			entry.Description = e.Description
		}
		if e.Reason != "" {
			entry.Reason = e.Reason
		}
		if e.Tier != "" {
			entry.Tier = e.Tier
		}
		if entry.Project == "" {
			entry.Project = e.Project
		}
		if entry.Worktree == "" {
			entry.Worktree = e.Worktree
		}
		if e.Pinned {
			entry.Pinned = true
		}
		result.Appended = true
	} else {
		result.Created = true
	}

	rel, err := s.primaryRel(entry, existing != nil)
	if err != nil {
		return result, err
	}
	entry.Path = filepath.ToSlash(filepath.Join(primaryDir, rel))
	if err := s.writePrimaryEntry(primaryRoot, rel, entry); err != nil {
		return result, err
	}
	result.Entry = entry
	if err := s.writePrimaryIndex(primaryRoot); err != nil {
		return result, err
	}

	if qwenActive {
		if err := s.writeQwenEntry(entry); err != nil {
			return result, err
		}
		if err := s.writeQwenIndex(); err != nil {
			return result, err
		}
	}
	return result, nil
}

// primaryRel chooses the relative path of the primary entry. An existing
// entry keeps its path; a new one goes to corpus/<type>/<slug>.md.
func (s *Store) primaryRel(e memory.Entry, existing bool) (string, error) {
	if existing && strings.HasPrefix(e.Path, primaryDir+"/") {
		return strings.TrimPrefix(e.Path, primaryDir+"/"), nil
	}
	rel := filepath.ToSlash(filepath.Join("corpus", string(e.Type), memory.Slugify(e.Title)+".md"))
	return s.uniqueRel(filepath.Join(s.worktreeRoot, primaryDir), rel)
}

func (s *Store) writeQwenEntry(e memory.Entry) error {
	qwenRoot, err := s.ensureQwen()
	if err != nil {
		return err
	}
	entries, err := s.readEntries(qwenRoot, true)
	if err != nil {
		return err
	}
	var rel string
	for _, existing := range entries {
		if memory.NormTitle(existing.Title) == memory.NormTitle(e.Title) {
			rel = strings.TrimPrefix(existing.Path, qwenDir+"/")
			break
		}
	}
	if rel == "" {
		rel = filepath.ToSlash(filepath.Join(string(e.Type), memory.Slugify(e.Title)+".md"))
		rel, err = s.uniqueRel(qwenRoot, rel)
		if err != nil {
			return err
		}
	}
	path := filepath.Join(qwenRoot, filepath.FromSlash(rel))
	if err := ensureParent(qwenRoot, filepath.Dir(rel)); err != nil {
		return err
	}
	return safeWrite(path, native.RenderQwen(e))
}

func (s *Store) readPrimary() ([]memory.Entry, error) {
	root, ok, err := s.existingDir(".agents", "memory")
	if err != nil || !ok {
		return nil, err
	}
	return s.readEntries(root, false)
}

func (s *Store) readQwen() ([]memory.Entry, error) {
	root, ok, err := s.existingDir(".qwen", "team-memory")
	if err != nil || !ok {
		return nil, err
	}
	return s.readEntries(root, true)
}

func (s *Store) readEntries(root string, qwen bool) ([]memory.Entry, error) {
	var out []memory.Entry
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if d.Type()&os.ModeSymlink != 0 {
			return nil
		}
		if !strings.HasSuffix(d.Name(), ".md") || d.Name() == indexName {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		var e memory.Entry
		var ok bool
		if qwen {
			e, ok = native.ParseQwen(string(data), filepath.Base(rel))
			if ok {
				e.Native = string(native.TargetQwen)
				if e.Source == "" || e.Source == "qwen-memory" {
					e.Source = "qwen-team-memory"
				}
				e.Path = filepath.ToSlash(filepath.Join(qwenDir, rel))
			}
		} else {
			e, ok = memory.Parse(string(data))
			if ok {
				e.Path = filepath.ToSlash(filepath.Join(primaryDir, rel))
			}
		}
		if ok {
			out = append(out, e)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (s *Store) writePrimaryEntry(root, rel string, e memory.Entry) error {
	if err := ensureParent(root, filepath.Dir(rel)); err != nil {
		return err
	}
	return safeWrite(filepath.Join(root, filepath.FromSlash(rel)), memory.Format(e))
}

func (s *Store) writePrimaryIndex(root string) error {
	entries, err := s.readEntries(root, false)
	if err != nil {
		return err
	}
	var b strings.Builder
	for _, e := range entries {
		rel := strings.TrimPrefix(e.Path, primaryDir+"/")
		fmt.Fprintf(&b, "- [%s](%s) — %s\n", e.Title, rel, description(e))
	}
	return safeWrite(filepath.Join(root, indexName), b.String())
}

func (s *Store) writeQwenIndex() error {
	root, ok, err := s.existingDir(".qwen", "team-memory")
	if err != nil || !ok {
		return err
	}
	entries, err := s.readEntries(root, true)
	if err != nil {
		return err
	}
	var b strings.Builder
	for _, e := range entries {
		rel := strings.TrimPrefix(e.Path, qwenDir+"/")
		fmt.Fprintf(&b, "- [%s](%s) — %s\n", e.Title, rel, description(e))
	}
	return safeWrite(filepath.Join(root, indexName), b.String())
}

func description(e memory.Entry) string {
	if e.Description != "" {
		return e.Description
	}
	return e.Title
}

// ensurePrimary creates .agents/memory, refusing a symlink in any component.
func (s *Store) ensurePrimary() (string, error) {
	return ensureDir(s.worktreeRoot, ".agents", "memory")
}

func (s *Store) ensureQwen() (string, error) {
	return ensureDir(s.worktreeRoot, ".qwen", "team-memory")
}

// qwenEnabled reports whether the Qwen projection is active. An existing
// directory is enough; --native qwen creates it when missing.
func (s *Store) qwenEnabled(requested bool) (bool, error) {
	_, ok, err := s.existingDir(".qwen", "team-memory")
	if err != nil {
		return false, err
	}
	return ok || requested, nil
}

// existingDir walks path components without following symlinks.
func (s *Store) existingDir(parts ...string) (string, bool, error) {
	current := s.worktreeRoot
	for _, part := range parts {
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if err != nil {
			if os.IsNotExist(err) {
				return "", false, nil
			}
			return "", false, err
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return "", false, fmt.Errorf("refusing symlinked or non-directory team path: %s", current)
		}
	}
	return current, true, nil
}

// ensureDir creates path components under anchor, refusing a symlink at any
// step. It never follows a link out of the worktree.
func ensureDir(anchor string, parts ...string) (string, error) {
	current := anchor
	for _, part := range parts {
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if err != nil {
			if !os.IsNotExist(err) {
				return "", err
			}
			if err := os.Mkdir(current, 0o755); err != nil {
				return "", err
			}
			continue
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return "", fmt.Errorf("refusing symlinked or non-directory team path: %s", current)
		}
	}
	return current, nil
}

// ensureParent creates the parent of a relative file inside root.
func ensureParent(root, dir string) error {
	if dir == "." || dir == "" {
		return nil
	}
	parts := strings.Split(filepath.ToSlash(dir), "/")
	_, err := ensureDir(root, parts...)
	return err
}

// safeWrite refuses a symlink at the target leaf.
func safeWrite(path, content string) error {
	if info, err := os.Lstat(path); err == nil {
		if info.Mode()&os.ModeSymlink != 0 || info.IsDir() {
			return fmt.Errorf("refusing to write through %s", path)
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	return os.WriteFile(path, []byte(content), 0o644)
}

// uniqueRel adds a counter when a different note already owns the name.
func (s *Store) uniqueRel(root, rel string) (string, error) {
	path := filepath.Join(root, filepath.FromSlash(rel))
	if _, err := os.Lstat(path); os.IsNotExist(err) {
		return rel, nil
	}
	base := strings.TrimSuffix(rel, ".md")
	for n := 2; n < 1000; n++ {
		candidate := fmt.Sprintf("%s-%d.md", base, n)
		if _, err := os.Lstat(filepath.Join(root, filepath.FromSlash(candidate))); os.IsNotExist(err) {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("cannot find a free team memory name for %s", rel)
}

func typeIndex(t memory.Type) int {
	for i, known := range memory.Types {
		if t == known {
			return i
		}
	}
	return len(memory.Types)
}
