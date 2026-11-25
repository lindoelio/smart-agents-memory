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

func TestMaterializeFailsUnderAFile(t *testing.T) {
	blocker := filepath.Join(t.TempDir(), "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := skill.Materialize(blocker); err == nil {
		t.Fatal("Materialize under a file succeeded")
	}
}

func TestMaterializeFailsWhenSkillsIsAFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "skills"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := skill.Materialize(dir); err == nil {
		t.Fatal("Materialize accepted a skills file")
	}
}

func TestMaterializeReportsAFileInPlaceOfTheDirectory(t *testing.T) {
	file := filepath.Join(t.TempDir(), "a-file")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := skill.Materialize(file); err == nil {
		t.Fatal("the skill was materialized under a file")
	}
}

func TestMaterializeReportsAReadOnlyDirectory(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("the permission case needs a non-root user")
	}
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
	if _, err := skill.Materialize(dir); err == nil {
		t.Fatal("the skill was materialized in a read-only directory")
	}
}
