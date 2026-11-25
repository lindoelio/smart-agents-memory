package teammemory

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lindoelio/smart-agents-memory/internal/memory"
)

func teamEntry() memory.Entry {
	return memory.Entry{
		Title: "A shared rule", Type: memory.TypeLesson, Body: "Rule. Why it matters.",
		Description: "A shared rule", Native: "minimax-agent",
	}
}

func TestMergeUnique(t *testing.T) {
	got := mergeUnique([]string{"one", "two"}, []string{"two", "", "three", "one"})
	if len(got) != 3 || got[0] != "one" || got[1] != "two" || got[2] != "three" {
		t.Fatalf("the merged list is %v", got)
	}
	if merged := mergeUnique(nil, nil); len(merged) != 0 {
		t.Fatalf("the empty merge is %v", merged)
	}
	if merged := mergeUnique(nil, []string{"only"}); len(merged) != 1 || merged[0] != "only" {
		t.Fatalf("the first merge is %v", merged)
	}
}

func TestTypeIndex(t *testing.T) {
	if typeIndex(memory.TypeUser) != 0 || typeIndex(memory.TypeLesson) != 1 {
		t.Fatal("the type order is wrong")
	}
	if typeIndex(memory.Type("unknown")) != len(memory.Types) {
		t.Fatal("an unknown type is not last")
	}
}

func TestUniqueRelAddsACounter(t *testing.T) {
	root := t.TempDir()
	store := Open(root)
	rel, err := store.uniqueRel(root, "lesson/a-rule.md")
	if err != nil || rel != "lesson/a-rule.md" {
		t.Fatalf("the free name is %q %v", rel, err)
	}
	if err := os.MkdirAll(filepath.Join(root, "lesson"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "lesson", "a-rule.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	rel, err = store.uniqueRel(root, "lesson/a-rule.md")
	if err != nil || rel != "lesson/a-rule-2.md" {
		t.Fatalf("the taken name resolved to %q %v", rel, err)
	}
}

func TestAddValidation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Open(newRepo(t)).Add(ctx, teamEntry(), AddOptions{}); err == nil {
		t.Fatal("a canceled add succeeded")
	}

	cases := []struct {
		name  string
		entry memory.Entry
		want  string
	}{
		{"invalid type", memory.Entry{Title: "x", Type: memory.Type("nonsense"), Body: "y"}, "invalid entry type"},
		{"empty title", memory.Entry{Title: "  ", Type: memory.TypeLesson, Body: "y"}, "title and a body"},
		{"empty body", memory.Entry{Title: "x", Type: memory.TypeLesson, Body: "  "}, "title and a body"},
		{"user entry", memory.Entry{Title: "x", Type: memory.TypeUser, Body: "y"}, "personal user profile"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Open(newRepo(t)).Add(context.Background(), tc.entry, AddOptions{})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("the error is %v", err)
			}
		})
	}
}

func TestEntriesAndAddReportACanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Open(newRepo(t)).Entries(ctx); err == nil {
		t.Fatal("a canceled read succeeded")
	}
}

func TestAddAppendsAndMergesTheMetadata(t *testing.T) {
	ctx := context.Background()
	repo := newRepo(t)
	store := Open(repo)
	first := memory.Entry{
		Title: "A shared rule", Type: memory.TypeLesson, Body: "First.",
		Native: "minimax-agent", UsageScenarios: []string{"before a release"},
	}
	if _, err := store.Add(ctx, first, AddOptions{}); err != nil {
		t.Fatal(err)
	}
	second := memory.Entry{
		Title: "A shared rule", Type: memory.TypeLesson, Body: "Second.",
		Description: "Updated description", Reason: "because", Tier: memory.TierHot,
		Category: "testing_standard", UsageScenarios: []string{"before a release", "when a test fails"},
		Project: "demo", Worktree: "feature", Pinned: true,
	}
	result, err := store.Add(ctx, second, AddOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Appended || result.Created {
		t.Fatalf("the second write is %+v", result)
	}
	entry := result.Entry
	if !strings.Contains(entry.Body, "First.") || !strings.Contains(entry.Body, "Second.") {
		t.Fatalf("the appended body is %q", entry.Body)
	}
	if entry.Description != second.Description || entry.Reason != second.Reason ||
		entry.Tier != memory.TierHot || entry.Category != second.Category ||
		entry.Project != "demo" || entry.Worktree != "feature" || !entry.Pinned {
		t.Fatalf("the merged metadata is %+v", entry)
	}
	if len(entry.UsageScenarios) != 2 {
		t.Fatalf("the merged scenarios are %v", entry.UsageScenarios)
	}

	entries, err := store.Entries(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Title != "A shared rule" {
		t.Fatalf("the team entries are %+v", entries)
	}
}

func TestAddKeepsTheExistingScopeWhenTheNewOneIsEmpty(t *testing.T) {
	ctx := context.Background()
	store := Open(newRepo(t))
	first := memory.Entry{
		Title: "A scoped rule", Type: memory.TypeProject, Body: "First.",
		Project: "demo", Worktree: "feature",
	}
	if _, err := store.Add(ctx, first, AddOptions{}); err != nil {
		t.Fatal(err)
	}
	result, err := store.Add(ctx, memory.Entry{
		Title: "A scoped rule", Type: memory.TypeProject, Body: "Second.",
	}, AddOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if result.Entry.Project != "demo" || result.Entry.Worktree != "feature" {
		t.Fatalf("the scope was lost: %+v", result.Entry)
	}
}

func TestAddingTheSameBodyIsANoOpWithoutForce(t *testing.T) {
	ctx := context.Background()
	store := Open(newRepo(t))
	first := memory.Entry{Title: "A rule", Type: memory.TypeLesson, Body: "The body."}
	if _, err := store.Add(ctx, first, AddOptions{}); err != nil {
		t.Fatal(err)
	}
	result, err := store.Add(ctx, first, AddOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if result.Created || result.Appended {
		t.Fatalf("the duplicate write is %+v", result)
	}
	if result.Entry.Body != "The body." {
		t.Fatalf("the returned body is %q", result.Entry.Body)
	}

	forced, err := store.Add(ctx, first, AddOptions{Force: true})
	if err != nil {
		t.Fatal(err)
	}
	if !forced.Appended {
		t.Fatalf("the forced write is %+v", forced)
	}
	if strings.Count(forced.Entry.Body, "The body.") != 2 {
		t.Fatalf("the forced body is %q", forced.Entry.Body)
	}
}

func TestDistinctTitlesGetDistinctSlugs(t *testing.T) {
	ctx := context.Background()
	repo := newRepo(t)
	store := Open(repo)
	first := memory.Entry{Title: "Hello, World!", Type: memory.TypeLesson, Body: "One."}
	second := memory.Entry{Title: "Hello World", Type: memory.TypeLesson, Body: "Two."}
	if _, err := store.Add(ctx, first, AddOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Add(ctx, second, AddOptions{}); err != nil {
		t.Fatal(err)
	}
	matches, err := filepath.Glob(filepath.Join(repo, ".agents", "memory", "corpus", "lesson", "hello-world*.md"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 2 {
		t.Fatalf("the two titles share %v", matches)
	}
}

func TestQwenOnlyEntryIsUpdatedInPlace(t *testing.T) {
	ctx := context.Background()
	repo := newRepo(t)
	qwenDir := filepath.Join(repo, ".qwen", "team-memory", "lesson")
	if err := os.MkdirAll(qwenDir, 0o755); err != nil {
		t.Fatal(err)
	}
	existing := filepath.Join(qwenDir, "a-shared-rule.md")
	body := "---\nname: A shared rule\ndescription: A rule\ntype: reference\ncategory: practice_standard\n---\n\nOld.\n"
	if err := os.WriteFile(existing, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	store := Open(repo)
	if _, err := store.Add(ctx, teamEntry(), AddOptions{}); err != nil {
		t.Fatal(err)
	}
	matches, err := filepath.Glob(filepath.Join(qwenDir, "a-shared-rule*.md"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 1 {
		t.Fatalf("the Qwen entry was duplicated: %v", matches)
	}
	data, err := os.ReadFile(matches[0])
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "Rule. Why it matters.") {
		t.Fatalf("the Qwen entry was not updated:\n%s", data)
	}
}

func TestIndexesUseTheDescriptionOrTheTitle(t *testing.T) {
	ctx := context.Background()
	repo := newRepo(t)
	store := Open(repo)
	if _, err := store.Add(ctx, memory.Entry{
		Title: "Without a description", Type: memory.TypeLesson, Body: "Body.",
	}, AddOptions{ProjectQwen: true}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(repo, ".agents", "memory", "MEMORY.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "— Without a description") {
		t.Fatalf("the primary index is %q", data)
	}
	qdata, err := os.ReadFile(filepath.Join(repo, ".qwen", "team-memory", "MEMORY.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(qdata), "— Without a description") {
		t.Fatalf("the qwen index is %q", qdata)
	}
}

func TestSafeWriteRefusesALinkOrAFolder(t *testing.T) {
	root := t.TempDir()
	folder := filepath.Join(root, "folder")
	if err := os.MkdirAll(folder, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := safeWrite(folder, "x"); err == nil {
		t.Fatal("safeWrite wrote over a folder")
	}

	outside := filepath.Join(t.TempDir(), "outside.md")
	if err := os.WriteFile(outside, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "link.md")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	if err := safeWrite(link, "x"); err == nil {
		t.Fatal("safeWrite wrote through a symlink")
	}
}

func TestEnsureParentAcceptsTheRoot(t *testing.T) {
	root := t.TempDir()
	if err := ensureParent(root, "."); err != nil {
		t.Fatal(err)
	}
	if err := ensureParent(root, ""); err != nil {
		t.Fatal(err)
	}
	if err := ensureParent(root, "a/b"); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(filepath.Join(root, "a", "b")); err != nil || !info.IsDir() {
		t.Fatalf("the parent was not created: %v", err)
	}
}

func TestEntriesReportANonDirectoryTeamPath(t *testing.T) {
	repo := newRepo(t)
	if err := os.WriteFile(filepath.Join(repo, ".agents"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(repo).Entries(context.Background()); err == nil {
		t.Fatal("a file where .agents belongs was read")
	}
}

func TestAddRejectsAFileWhereTheQwenRootBelongs(t *testing.T) {
	repo := newRepo(t)
	if err := os.WriteFile(filepath.Join(repo, ".qwen"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Open(repo).Add(context.Background(), teamEntry(), AddOptions{ProjectQwen: true})
	if err == nil || !strings.Contains(err.Error(), "refusing") {
		t.Fatalf("the error is %v", err)
	}
}

func TestEntriesReportAnUnreadableNote(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("the permission case needs a non-root user")
	}
	repo := newRepo(t)
	lesson := filepath.Join(repo, ".agents", "memory", "lesson")
	if err := os.MkdirAll(lesson, 0o755); err != nil {
		t.Fatal(err)
	}
	note := filepath.Join(lesson, "locked.md")
	if err := os.WriteFile(note, []byte("---\ntitle: Locked\ntype: lesson\n---\n\nBody.\n"), 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(note, 0o644) })
	if _, err := Open(repo).Entries(context.Background()); err == nil {
		t.Fatal("an unreadable note did not fail the read")
	}
}

func TestAddReportsAWriteFailure(t *testing.T) {
	repo := newRepo(t)
	// A symlink where corpus/lesson belongs blocks the entry.
	lesson := filepath.Join(repo, ".agents", "memory", "corpus", "lesson")
	if err := os.MkdirAll(filepath.Dir(lesson), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(t.TempDir(), lesson); err != nil {
		t.Fatal(err)
	}
	_, err := Open(repo).Add(context.Background(), teamEntry(), AddOptions{})
	if err == nil || !strings.Contains(err.Error(), "refusing") {
		t.Fatalf("the error is %v", err)
	}
}

func TestAddReportsAIndexWriteFailure(t *testing.T) {
	repo := newRepo(t)
	index := filepath.Join(repo, ".agents", "memory", "MEMORY.md")
	if err := os.MkdirAll(index, 0o755); err != nil {
		t.Fatal(err)
	}
	_, err := Open(repo).Add(context.Background(), teamEntry(), AddOptions{})
	if err == nil || !strings.Contains(err.Error(), "refusing to write") {
		t.Fatalf("the error is %v", err)
	}
}

func TestAddReportsAQwenEntryWriteFailure(t *testing.T) {
	repo := newRepo(t)
	lesson := filepath.Join(repo, ".qwen", "team-memory", "lesson")
	if err := os.MkdirAll(filepath.Dir(lesson), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(t.TempDir(), lesson); err != nil {
		t.Fatal(err)
	}
	_, err := Open(repo).Add(context.Background(), teamEntry(), AddOptions{})
	if err == nil || !strings.Contains(err.Error(), "refusing") {
		t.Fatalf("the error is %v", err)
	}
}

func TestAddReportsAQwenIndexWriteFailure(t *testing.T) {
	repo := newRepo(t)
	index := filepath.Join(repo, ".qwen", "team-memory", "MEMORY.md")
	if err := os.MkdirAll(index, 0o755); err != nil {
		t.Fatal(err)
	}
	_, err := Open(repo).Add(context.Background(), teamEntry(), AddOptions{})
	if err == nil || !strings.Contains(err.Error(), "refusing to write") {
		t.Fatalf("the error is %v", err)
	}
}
