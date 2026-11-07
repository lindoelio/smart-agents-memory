package skill_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/lindoelio/smart-agents-memory/internal/skill"
)

func TestTheSkillLandsWhereTheToolsLookForIt(t *testing.T) {
	dir := t.TempDir()
	root, err := skill.Materialize(dir)
	if err != nil {
		t.Fatalf("cannot materialize the skill: %v", err)
	}
	want := filepath.Join(dir, "skills", skill.Name)
	if root != want {
		t.Fatalf("the skill landed in %q, want %q", root, want)
	}
	manifest := filepath.Join(root, "SKILL.md")
	if _, err := os.Stat(manifest); err != nil {
		t.Fatalf("the manifest is missing: %v", err)
	}
	data, err := os.ReadFile(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) == 0 {
		t.Fatal("the manifest is empty")
	}
	// The references travel with the skill.
	references := filepath.Join(root, "references")
	entries, err := os.ReadDir(references)
	if err != nil {
		t.Fatalf("the references are missing: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("the references are empty")
	}
}
