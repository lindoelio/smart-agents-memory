package link

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestASecondOverwriteKeepsTheFirstBackup(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "AGENTS.md")
	source := filepath.Join(dir, "MEMORY.md")
	if err := os.WriteFile(source, []byte("hot"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("first"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Link(target, source, false); err != nil {
		t.Fatal(err)
	}
	first := target + BackupSuffix
	data, err := os.ReadFile(first)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "first" {
		t.Fatalf("the first backup is %q", data)
	}

	if err := os.Remove(target); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("second"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Link(target, source, false); err != nil {
		t.Fatal(err)
	}
	kept, err := os.ReadFile(first)
	if err != nil {
		t.Fatalf("the first backup was removed: %v", err)
	}
	if string(kept) != "first" {
		t.Fatalf("the first backup changed to %q", kept)
	}
	second, err := os.ReadFile(first + "-2")
	if err != nil {
		t.Fatal(err)
	}
	if string(second) != "second" {
		t.Fatalf("the second backup is %q", second)
	}
	if _, err := os.Lstat(target); err != nil {
		t.Fatal(err)
	}
	dest, err := os.Readlink(target)
	if err != nil || !strings.HasSuffix(dest, "MEMORY.md") {
		t.Fatalf("the link points at %q (%v)", dest, err)
	}
}
