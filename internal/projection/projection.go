// Package projection holds the native stores the AI tools read.
//
// The projections are dictated by the tools, not by the vault: MiniMax Code
// and Qwen each have their own shape. They are always local, because the
// tools read them from the local disk.
package projection

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/lindoelio/smart-agents-memory/internal/native"
)

// Store is the local directory holding the native projections.
type Store struct {
	root string
}

// New builds a projection store rooted at dir.
func New(root string) *Store { return &Store{root: root} }

// Root is the local root of the projections.
func (s *Store) Root() string { return s.root }

// TargetDir is the directory of one native target.
func (s *Store) TargetDir(target native.Target) string {
	return filepath.Join(s.root, "native", string(target))
}

// Read returns the content of one file of a target.
func (s *Store) Read(target native.Target, rel string) (string, error) {
	data, err := os.ReadFile(filepath.Join(s.TargetDir(target), rel))
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// ModTime returns when a file of a target was last changed.
func (s *Store) ModTime(target native.Target, rel string) (time.Time, error) {
	info, err := os.Stat(filepath.Join(s.TargetDir(target), rel))
	if err != nil {
		return time.Time{}, err
	}
	return info.ModTime(), nil
}

// Write stores one file of a target.
func (s *Store) Write(target native.Target, rel, content string) error {
	path := filepath.Join(s.TargetDir(target), rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(content), 0o644)
}

// Exists reports whether a file of a target is there.
func (s *Store) Exists(target native.Target, rel string) bool {
	_, err := os.Stat(filepath.Join(s.TargetDir(target), rel))
	return err == nil
}

// List returns the files of a target, relative to its directory.
func (s *Store) List(target native.Target, sub string) ([]string, error) {
	dir := filepath.Join(s.TargetDir(target), sub)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		rel := e.Name()
		if sub != "" {
			rel = filepath.Join(sub, e.Name())
		}
		out = append(out, filepath.ToSlash(rel))
	}
	return out, nil
}

// Delete removes one file of a target.
func (s *Store) Delete(target native.Target, rel string) error {
	err := os.Remove(filepath.Join(s.TargetDir(target), rel))
	if err != nil && os.IsNotExist(err) {
		return nil
	}
	return err
}

// StateFile is where the state of the projections lives.
const StateFile = ".smart-agents-memory-state.json"

// projectionState is the small state the reconciliation needs.
type projectionState struct {
	// LastProjection is when the projections were last written.
	LastProjection time.Time `json:"last_projection"`
}

// LastProjection returns when the projections were last written. The zero
// time means never.
func (s *Store) LastProjection() time.Time {
	data, err := os.ReadFile(filepath.Join(s.root, StateFile))
	if err != nil {
		return time.Time{}
	}
	var state projectionState
	if err := json.Unmarshal(data, &state); err != nil {
		return time.Time{}
	}
	return state.LastProjection
}

// SetLastProjection records that the projections were written now.
func (s *Store) SetLastProjection(t time.Time) error {
	if err := os.MkdirAll(s.root, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(projectionState{LastProjection: t}, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(s.root, StateFile), append(data, '\n'), 0o644)
}

// EnsureDirs creates the directories of every target.
func (s *Store) EnsureDirs() error {
	for _, target := range native.Targets {
		if err := os.MkdirAll(s.TargetDir(target), 0o755); err != nil {
			return fmt.Errorf("cannot create %s: %w", target, err)
		}
	}
	return os.MkdirAll(filepath.Join(s.TargetDir(native.TargetMiniMaxAgent), "topics"), 0o755)
}
