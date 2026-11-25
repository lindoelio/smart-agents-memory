package link

import (
	"fmt"
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

func TestCheckReportsMissingNotASymlinkWrongAndMissingDestination(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "AGENTS.md")
	source := filepath.Join(dir, "MEMORY.md")
	if err := os.WriteFile(source, []byte("hot"), 0o644); err != nil {
		t.Fatal(err)
	}
	if ok, reason := Check(target, source); ok || !strings.Contains(reason, "missing") {
		t.Fatalf("missing: %v %q", ok, reason)
	}
	if err := os.WriteFile(target, []byte("plain"), 0o644); err != nil {
		t.Fatal(err)
	}
	if ok, reason := Check(target, source); ok || !strings.Contains(reason, "not a symlink") {
		t.Fatalf("plain: %v %q", ok, reason)
	}
	if err := os.Remove(target); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(source, target); err != nil {
		t.Fatal(err)
	}
	if ok, reason := Check(target, source); !ok {
		t.Fatalf("correct link: %v %q", ok, reason)
	}
	if ok, reason := Check(target, filepath.Join(dir, "other")); ok || !strings.Contains(reason, "wrong place") {
		t.Fatalf("wrong place: %v %q", ok, reason)
	}
	if err := os.Remove(source); err != nil {
		t.Fatal(err)
	}
	if ok, reason := Check(target, ""); ok || !strings.Contains(reason, "missing destination") {
		t.Fatalf("missing destination: %v %q", ok, reason)
	}
}

func TestLinkIsIdempotentAndCreatesTheParent(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "MEMORY.md")
	if err := os.WriteFile(source, []byte("hot"), 0o644); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(dir, "nested", "AGENTS.md")
	if _, err := Link(target, source, false); err != nil {
		t.Fatal(err)
	}
	if msg, err := Link(target, source, false); err != nil || !strings.Contains(msg, "already linked") {
		t.Fatalf("second link: %q %v", msg, err)
	}
}

func TestLinkFailsWhenTheTargetParentIsAFile(t *testing.T) {
	dir := t.TempDir()
	blocker := filepath.Join(dir, "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Link(filepath.Join(blocker, "AGENTS.md"), blocker, false); err == nil {
		t.Fatal("Link under a file succeeded")
	}
}

func TestLinkCreatesAndReusesASymlink(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "MEMORY.md")
	if err := os.WriteFile(source, []byte("hot"), 0o644); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(dir, "nested", "AGENTS.md")

	message, err := Link(target, source, false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(message, "linked:") {
		t.Fatalf("the message is %q", message)
	}
	dest, err := os.Readlink(target)
	if err != nil || dest != source {
		t.Fatalf("the link points at %q (%v)", dest, err)
	}
	if ok, reason := Check(target, source); !ok {
		t.Fatalf("the fresh link does not check out: %s", reason)
	}

	again, err := Link(target, source, false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(again, "already linked:") {
		t.Fatalf("the second link is %q", again)
	}
}

func TestLinkReplacesASymlinkThatPointsElsewhere(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "MEMORY.md")
	if err := os.WriteFile(source, []byte("hot"), 0o644); err != nil {
		t.Fatal(err)
	}
	old := filepath.Join(dir, "old.md")
	if err := os.WriteFile(old, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(dir, "AGENTS.md")
	if err := os.Symlink(old, target); err != nil {
		t.Fatal(err)
	}
	if _, err := Link(target, source, false); err != nil {
		t.Fatal(err)
	}
	dest, err := os.Readlink(target)
	if err != nil || dest != source {
		t.Fatalf("the link still points at %q (%v)", dest, err)
	}
}

func TestLinkReportsItsFailures(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "MEMORY.md")
	if err := os.WriteFile(source, []byte("hot"), 0o644); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "a-file")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	// The parent of the target is a file.
	if _, err := Link(filepath.Join(file, "AGENTS.md"), source, false); err == nil {
		t.Fatal("a link was created under a file")
	}
	// A symlink whose destination is missing cannot be read back as the
	// expected one; Link removes it and creates the right one.
	dangling := filepath.Join(dir, "dangling.md")
	if err := os.Symlink(filepath.Join(dir, "missing.md"), dangling); err != nil {
		t.Fatal(err)
	}
	if _, err := Link(dangling, source, false); err != nil {
		t.Fatalf("a dangling link was not replaced: %v", err)
	}
}

func TestCheck(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "MEMORY.md")
	if err := os.WriteFile(source, []byte("hot"), 0o644); err != nil {
		t.Fatal(err)
	}
	missing := filepath.Join(dir, "missing.md")
	if ok, reason := Check(missing, source); ok || !strings.Contains(reason, "missing") {
		t.Fatalf("a missing target checks out: %q", reason)
	}

	plain := filepath.Join(dir, "plain.md")
	if err := os.WriteFile(plain, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if ok, reason := Check(plain, source); ok || !strings.Contains(reason, "not a symlink") {
		t.Fatalf("a plain file checks out: %q", reason)
	}

	dangling := filepath.Join(dir, "dangling.md")
	if err := os.Symlink(filepath.Join(dir, "gone.md"), dangling); err != nil {
		t.Fatal(err)
	}
	if ok, reason := Check(dangling, source); ok || !strings.Contains(reason, "missing destination") {
		t.Fatalf("a dangling link checks out: %q", reason)
	}

	wrong := filepath.Join(dir, "wrong.md")
	other := filepath.Join(dir, "other.md")
	if err := os.WriteFile(other, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(other, wrong); err != nil {
		t.Fatal(err)
	}
	if ok, reason := Check(wrong, source); ok || !strings.Contains(reason, "wrong place") {
		t.Fatalf("a link to another file checks out: %q", reason)
	}
	if ok, reason := Check(wrong, ""); !ok {
		t.Fatalf("a link to an existing file does not check out without an expected source: %q", reason)
	}

	good := filepath.Join(dir, "good.md")
	if err := os.Symlink(source, good); err != nil {
		t.Fatal(err)
	}
	if ok, reason := Check(good, source); !ok {
		t.Fatalf("a good link does not check out: %q", reason)
	}
}

func TestLinkReportsAReadOnlyFolder(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("the permission case needs a non-root user")
	}
	dir := t.TempDir()
	source := filepath.Join(dir, "MEMORY.md")
	if err := os.WriteFile(source, []byte("hot"), 0o644); err != nil {
		t.Fatal(err)
	}
	locked := filepath.Join(dir, "locked")
	if err := os.MkdirAll(locked, 0o755); err != nil {
		t.Fatal(err)
	}
	// A missing target cannot be created in a read-only folder.
	if err := os.Chmod(locked, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o755) })
	if _, err := Link(filepath.Join(locked, "AGENTS.md"), source, false); err == nil {
		t.Fatal("a link was created in a read-only folder")
	}

	// An existing wrong symlink cannot be removed from a read-only folder.
	target := filepath.Join(locked, "wrong.md")
	other := filepath.Join(dir, "other.md")
	if err := os.WriteFile(other, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Create the symlink while the folder is writable again.
	if err := os.Chmod(locked, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(other, target); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(locked, 0o500); err != nil {
		t.Fatal(err)
	}
	if _, err := Link(target, source, false); err == nil {
		t.Fatal("a link in a read-only folder was replaced")
	}
}

func TestLinkReportsAnExhaustedBackupName(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "MEMORY.md")
	if err := os.WriteFile(source, []byte("hot"), 0o644); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(dir, "AGENTS.md")
	if err := os.WriteFile(target, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	base := target + BackupSuffix
	if err := os.WriteFile(base, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	for n := 2; n < 1000; n++ {
		if err := os.WriteFile(fmt.Sprintf("%s-%d", base, n), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := Link(target, source, false); err == nil {
		t.Fatal("a link was created with no free backup name")
	}
	if _, err := backupPath(target); err == nil {
		t.Fatal("backupPath found a free name that does not exist")
	}
}
