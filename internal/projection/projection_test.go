package projection

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lindoelio/smart-agents-memory/internal/native"
)

func TestNewAndTargetDir(t *testing.T) {
	root := t.TempDir()
	store := New(root)
	if store.Root() != root {
		t.Fatalf("the root is %q", store.Root())
	}
	want := filepath.Join(root, "native", string(native.TargetQwen))
	if got := store.TargetDir(native.TargetQwen); got != want {
		t.Fatalf("the target dir is %q, want %q", got, want)
	}
}

func TestWriteReadAndModTime(t *testing.T) {
	store := New(t.TempDir())
	if _, err := store.Read(native.TargetQwen, "MEMORY.md"); err == nil {
		t.Fatal("a missing file was read")
	}
	if _, err := store.ModTime(native.TargetQwen, "MEMORY.md"); err == nil {
		t.Fatal("a missing file has a modification time")
	}
	if store.Exists(native.TargetQwen, "MEMORY.md") {
		t.Fatal("a missing file exists")
	}

	if err := store.Write(native.TargetQwen, "topics/a-topic.md", "content"); err != nil {
		t.Fatal(err)
	}
	if !store.Exists(native.TargetQwen, "topics/a-topic.md") {
		t.Fatal("the written file does not exist")
	}
	content, err := store.Read(native.TargetQwen, "topics/a-topic.md")
	if err != nil || content != "content" {
		t.Fatalf("the read is %q %v", content, err)
	}
	mod, err := store.ModTime(native.TargetQwen, "topics/a-topic.md")
	if err != nil || mod.IsZero() {
		t.Fatalf("the modification time is %v %v", mod, err)
	}
}

func TestWriteIfChangedKeepsTheModTime(t *testing.T) {
	store := New(t.TempDir())
	wrote, err := store.WriteIfChanged(native.TargetQwen, "MEMORY.md", "one")
	if err != nil || !wrote {
		t.Fatalf("the first write is %v %v", wrote, err)
	}
	before, err := store.ModTime(native.TargetQwen, "MEMORY.md")
	if err != nil {
		t.Fatal(err)
	}
	// Make the difference visible even on a coarse filesystem clock.
	if err := os.Chtimes(filepath.Join(store.TargetDir(native.TargetQwen), "MEMORY.md"),
		before.Add(-time.Hour), before.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	wrote, err = store.WriteIfChanged(native.TargetQwen, "MEMORY.md", "one")
	if err != nil || wrote {
		t.Fatalf("an unchanged write is %v %v", wrote, err)
	}
	after, err := store.ModTime(native.TargetQwen, "MEMORY.md")
	if err != nil {
		t.Fatal(err)
	}
	if !after.Equal(before.Add(-time.Hour)) {
		t.Fatalf("the unchanged file was rewritten: %v -> %v", before, after)
	}

	wrote, err = store.WriteIfChanged(native.TargetQwen, "MEMORY.md", "two")
	if err != nil || !wrote {
		t.Fatalf("the changed write is %v %v", wrote, err)
	}
	content, err := store.Read(native.TargetQwen, "MEMORY.md")
	if err != nil || content != "two" {
		t.Fatalf("the content is %q %v", content, err)
	}
}

func TestWriteReportsItsFailures(t *testing.T) {
	root := t.TempDir()
	store := New(root)
	// A file where the target folder belongs fails the write.
	if err := os.MkdirAll(filepath.Join(root, "native"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "native", string(native.TargetQwen)), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := store.Write(native.TargetQwen, "MEMORY.md", "content"); err == nil {
		t.Fatal("the file was written under a file")
	}

	// A folder where the file belongs fails the write.
	other := New(t.TempDir())
	if err := os.MkdirAll(filepath.Join(other.TargetDir(native.TargetQwen), "MEMORY.md"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := other.Write(native.TargetQwen, "MEMORY.md", "content"); err == nil {
		t.Fatal("the file was written over a folder")
	}
	if _, err := other.WriteIfChanged(native.TargetQwen, "MEMORY.md", "content"); err == nil {
		t.Fatal("WriteIfChanged did not report the failure")
	}
}

func TestList(t *testing.T) {
	store := New(t.TempDir())
	if files, err := store.List(native.TargetMiniMaxAgent, "topics"); err != nil || files != nil {
		t.Fatalf("a missing folder listed %v %v", files, err)
	}

	if err := store.Write(native.TargetMiniMaxAgent, "topics/b-topic.md", "b"); err != nil {
		t.Fatal(err)
	}
	if err := store.Write(native.TargetMiniMaxAgent, "topics/a-topic.md", "a"); err != nil {
		t.Fatal(err)
	}
	if err := store.Write(native.TargetMiniMaxAgent, "topics/ignored.txt", "x"); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(store.TargetDir(native.TargetMiniMaxAgent), "topics", "nested"), 0o755); err != nil {
		t.Fatal(err)
	}

	files, err := store.List(native.TargetMiniMaxAgent, "topics")
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 2 || files[0] != "topics/a-topic.md" || files[1] != "topics/b-topic.md" {
		t.Fatalf("the topics are %v", files)
	}

	root, err := store.List(native.TargetMiniMaxAgent, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(root) != 0 {
		t.Fatalf("the root list is %v", root)
	}

	// A file where the folder belongs is an error, not an empty list.
	broken := New(t.TempDir())
	if err := os.MkdirAll(filepath.Join(broken.Root(), "native"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(broken.TargetDir(native.TargetQwen), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := broken.List(native.TargetQwen, ""); err == nil {
		t.Fatal("a target file was listed as a folder")
	}
}

func TestDelete(t *testing.T) {
	store := New(t.TempDir())
	if err := store.Delete(native.TargetQwen, "missing.md"); err != nil {
		t.Fatalf("deleting a missing file failed: %v", err)
	}
	if err := store.Write(native.TargetQwen, "MEMORY.md", "content"); err != nil {
		t.Fatal(err)
	}
	if err := store.Delete(native.TargetQwen, "MEMORY.md"); err != nil {
		t.Fatal(err)
	}
	if store.Exists(native.TargetQwen, "MEMORY.md") {
		t.Fatal("the file survived the delete")
	}

	// A non-empty folder is not deleted as a file.
	if err := os.MkdirAll(filepath.Join(store.TargetDir(native.TargetQwen), "folder.md", "child"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := store.Delete(native.TargetQwen, "folder.md"); err == nil {
		t.Fatal("a folder was deleted as a file")
	}
}

func TestLastProjection(t *testing.T) {
	store := New(t.TempDir())
	if !store.LastProjection().IsZero() {
		t.Fatal("a fresh store has a projection time")
	}
	if err := os.WriteFile(filepath.Join(store.Root(), StateFile), []byte("not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !store.LastProjection().IsZero() {
		t.Fatal("a broken state has a projection time")
	}

	want := time.Date(2026, 11, 7, 10, 30, 0, 0, time.UTC)
	if err := store.SetLastProjection(want); err != nil {
		t.Fatal(err)
	}
	if got := store.LastProjection(); !got.Equal(want) {
		t.Fatalf("the projection time is %v, want %v", got, want)
	}
	data, err := os.ReadFile(filepath.Join(store.Root(), StateFile))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(string(data), "\n") {
		t.Fatalf("the state file has no trailing newline: %q", data)
	}

	// A file where the store root belongs fails the write.
	file := filepath.Join(t.TempDir(), "root-file")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := New(file).SetLastProjection(want); err == nil {
		t.Fatal("the state was written into a file")
	}
}

func TestEnsureDirs(t *testing.T) {
	root := t.TempDir()
	store := New(root)
	if err := store.EnsureDirs(); err != nil {
		t.Fatal(err)
	}
	for _, target := range native.Targets {
		if info, err := os.Stat(store.TargetDir(target)); err != nil || !info.IsDir() {
			t.Fatalf("the %s target is missing: %v", target, err)
		}
	}
	if info, err := os.Stat(filepath.Join(store.TargetDir(native.TargetMiniMaxAgent), "topics")); err != nil || !info.IsDir() {
		t.Fatalf("the topics folder is missing: %v", err)
	}

	file := filepath.Join(t.TempDir(), "root-file")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := New(file).EnsureDirs(); err == nil {
		t.Fatal("the targets were created under a file")
	}
}
