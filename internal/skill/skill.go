// Package skill ships the agent skill inside the binary.
//
// The skill is what makes the memory discoverable: each tool loads it by
// its own skill mechanism. Embedding it means `brew install` plus
// `smart-agents-memory install` is enough — no clone of the repository is
// needed.
package skill

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	assets "github.com/lindoelio/smart-agents-memory"
)

// Name is the name of the skill.
const Name = "smart-agents-memory"

// Materialize writes the embedded skill to dir and returns its path.
func Materialize(dir string) (string, error) {
	root := filepath.Join(dir, "skills", Name)
	err := fs.WalkDir(assets.SkillFS, "skills", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel("skills", path)
		if err != nil {
			return err
		}
		target := filepath.Join(root, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := assets.SkillFS.ReadFile(path)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
	if err != nil {
		return "", fmt.Errorf("cannot materialize the skill: %w", err)
	}
	return root, nil
}
