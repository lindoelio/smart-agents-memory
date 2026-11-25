package filevault

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lindoelio/smart-agents-memory/internal/memory"
)

func newStore(t *testing.T, opts Options) *Store {
	t.Helper()
	root := t.TempDir()
	return New(root, opts)
}

func mustEnsure(t *testing.T, store *Store) {
	t.Helper()
	if err := store.Ensure(context.Background()); err != nil {
		t.Fatalf("cannot create the store: %v", err)
	}
}

func TestKindRootAndLayout(t *testing.T) {
	root := t.TempDir()
	plain := New(root, Options{Kind: "disk"})
	if plain.Kind() != "disk" {
		t.Fatalf("the kind is %q", plain.Kind())
	}
	if plain.Root() != root {
		t.Fatalf("the root is %q", plain.Root())
	}
	if plain.CorpusDir() != filepath.Join(root, "corpus") {
		t.Fatalf("the corpus dir is %q", plain.CorpusDir())
	}
	if plain.LogDir() != filepath.Join(root, "log") {
		t.Fatalf("the log dir is %q", plain.LogDir())
	}
	if plain.IndexPath() != filepath.Join(root, "corpus", "INDEX.md") {
		t.Fatalf("the plain index is %q", plain.IndexPath())
	}

	obsidian := New(root, Options{Kind: "obsidian", ObsidianIndex: true})
	if obsidian.IndexPath() != filepath.Join(root, obsidianIndexName) {
		t.Fatalf("the obsidian index is %q", obsidian.IndexPath())
	}
}

func TestReadyAndEnsure(t *testing.T) {
	ctx := context.Background()
	store := New(filepath.Join(t.TempDir(), "store"), Options{Kind: "disk"})
	if err := store.Ready(ctx); err == nil {
		t.Fatal("a missing store is ready")
	}
	if err := store.Ready(ctx); !strings.Contains(err.Error(), "install") {
		t.Fatalf("the error does not point at install: %v", err)
	}

	mustEnsure(t, store)
	if err := store.Ready(ctx); err != nil {
		t.Fatalf("the created store is not ready: %v", err)
	}
	for _, dir := range []string{
		filepath.Join(store.CorpusDir(), "user"),
		filepath.Join(store.CorpusDir(), "lesson"),
		filepath.Join(store.CorpusDir(), "project"),
		filepath.Join(store.CorpusDir(), "reference"),
		filepath.Join(store.Root(), "projects"),
		store.LogDir(),
	} {
		if info, err := os.Stat(dir); err != nil || !info.IsDir() {
			t.Fatalf("the layout is missing %s: %v", dir, err)
		}
	}
	// A second Ensure is idempotent.
	mustEnsure(t, store)

	// A file where the store should be is not ready.
	file := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := New(file, Options{}).Ready(ctx); err == nil {
		t.Fatal("a file passed as the store is ready")
	}
}

func TestProblemsReportsUnreadableNotesAndMissingRoot(t *testing.T) {
	ctx := context.Background()
	missing := New(filepath.Join(t.TempDir(), "absent"), Options{})
	problems, err := missing.Problems(ctx)
	if err != nil {
		t.Fatalf("a missing store is an error: %v", err)
	}
	if len(problems) != 1 || !strings.Contains(problems[0], "absent") {
		t.Fatalf("a missing store has problems: %v", problems)
	}

	store := newStore(t, Options{Kind: "disk"})
	mustEnsure(t, store)
	if problems, err := store.Problems(ctx); err != nil || len(problems) != 0 {
		t.Fatalf("an empty store has problems: %v %v", problems, err)
	}

	if os.Geteuid() == 0 {
		t.Skip("the permission case needs a non-root user")
	}
	lesson := filepath.Join(store.CorpusDir(), "lesson")
	locked := filepath.Join(lesson, "locked.md")
	if err := os.WriteFile(locked, []byte("x"), 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o644) })
	problems, err = store.Problems(ctx)
	if err != nil {
		t.Fatalf("cannot list the problems: %v", err)
	}
	if len(problems) != 1 || !strings.Contains(problems[0], "locked.md") {
		t.Fatalf("the unreadable note was not reported: %v", problems)
	}
}

func TestEntriesSkipsWhatIsNotAnEntry(t *testing.T) {
	store := newStore(t, Options{Kind: "disk"})
	mustEnsure(t, store)
	ctx := context.Background()
	lesson := filepath.Join(store.CorpusDir(), "lesson")
	write := func(name, content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(lesson, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("b-lesson.md", "---\ntitle: \"B lesson\"\ntype: lesson\ndate: 2026-11-07\n---\n\nBody.\n")
	write("a-lesson.md", "---\ntitle: \"A lesson\"\ntype: lesson\ndate: 2026-11-07\n---\n\nBody.\n")
	write("no-type.md", "---\ntitle: \"A project note\"\ndescription: x\n---\n\nBody.\n")
	write("malformed.md", "no frontmatter here\n")
	write("not-markdown.txt", "ignored")
	if err := os.MkdirAll(filepath.Join(lesson, "nested"), 0o755); err != nil {
		t.Fatal(err)
	}

	entries, err := store.Entries(ctx)
	if err != nil {
		t.Fatalf("cannot read the entries: %v", err)
	}
	if len(entries) != 2 || entries[0].Title != "A lesson" || entries[1].Title != "B lesson" {
		t.Fatalf("the entries are %+v", entries)
	}
	if entries[0].Path != "corpus/lesson/a-lesson.md" {
		t.Fatalf("the path is %q", entries[0].Path)
	}
	if entries[0].ModTime.IsZero() {
		t.Fatal("the entry has no modification time")
	}
}

func TestPinnedFolderMarksTheEntry(t *testing.T) {
	store := newStore(t, Options{Kind: "obsidian", Recursive: true})
	mustEnsure(t, store)
	pinned := filepath.Join(store.CorpusDir(), "PINNED")
	if err := os.MkdirAll(pinned, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pinned, "a-rule.md"),
		[]byte("---\ntitle: A rule\ntype: lesson\n---\n\nBody.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	entries, err := store.Entries(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || !entries[0].Pinned {
		t.Fatalf("the pinned folder did not protect the entry: %+v", entries)
	}
}

func TestCreateRejectsAnUnknownType(t *testing.T) {
	store := newStore(t, Options{Kind: "disk"})
	_, err := store.Create(context.Background(), memory.Entry{Title: "x", Type: memory.Type("nonsense")})
	if err == nil || !strings.Contains(err.Error(), "user, lesson, project, reference") {
		t.Fatalf("the error does not list the types: %v", err)
	}
}

func TestCreateGivesEveryEntryItsOwnPath(t *testing.T) {
	store := newStore(t, Options{Kind: "disk"})
	mustEnsure(t, store)
	ctx := context.Background()
	first, err := store.Create(ctx, memory.Entry{Title: "Same title", Type: memory.TypeLesson, Body: "One."})
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.Create(ctx, memory.Entry{Title: "Same title", Type: memory.TypeLesson, Body: "Two."})
	if err != nil {
		t.Fatal(err)
	}
	if first.Path == second.Path {
		t.Fatalf("the second create reused %q", first.Path)
	}
	if !strings.HasSuffix(second.Path, "-2.md") {
		t.Fatalf("the unique path is %q", second.Path)
	}
	if first.ModTime.IsZero() {
		t.Fatal("the created entry has no modification time")
	}
}

func TestUpdateAndItsErrors(t *testing.T) {
	store := newStore(t, Options{Kind: "disk"})
	mustEnsure(t, store)
	ctx := context.Background()
	if err := store.Update(ctx, memory.Entry{Title: "x", Type: memory.TypeLesson}); err == nil {
		t.Fatal("an entry without a path was updated")
	}

	created, err := store.Create(ctx, memory.Entry{Title: "Editable", Type: memory.TypeLesson, Body: "One."})
	if err != nil {
		t.Fatal(err)
	}
	created.Body = "Two."
	if err := store.Update(ctx, created); err != nil {
		t.Fatalf("cannot update: %v", err)
	}
	entries, err := store.Entries(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Body != "Two." {
		t.Fatalf("the update did not land: %+v", entries)
	}

	// Updating into a missing directory creates it.
	nested := memory.Entry{Title: "Nested", Type: memory.TypeLesson, Body: "Body.", Path: "corpus/lesson/deep/a-note.md"}
	if err := store.Update(ctx, nested); err != nil {
		t.Fatalf("cannot create the parent: %v", err)
	}
	if _, err := os.Stat(filepath.Join(store.Root(), nested.Path)); err != nil {
		t.Fatal(err)
	}
}

func TestRename(t *testing.T) {
	store := newStore(t, Options{Kind: "disk"})
	mustEnsure(t, store)
	ctx := context.Background()
	if _, err := store.Rename(ctx, memory.Entry{Title: "x", Type: memory.TypeLesson}, "New"); err == nil {
		t.Fatal("an entry without a path was renamed")
	}

	created, err := store.Create(ctx, memory.Entry{Title: "Old title", Type: memory.TypeLesson, Body: "Body."})
	if err != nil {
		t.Fatal(err)
	}
	// Renaming to the same slug rewrites the same file.
	same, err := store.Rename(ctx, created, "Old title")
	if err != nil {
		t.Fatalf("cannot rename in place: %v", err)
	}
	if same.Path != created.Path {
		t.Fatalf("the in-place rename moved %q to %q", created.Path, same.Path)
	}

	renamed, err := store.Rename(ctx, created, "New title")
	if err != nil {
		t.Fatalf("cannot rename: %v", err)
	}
	if renamed.Path == created.Path || !strings.HasSuffix(renamed.Path, "new-title.md") {
		t.Fatalf("the renamed path is %q", renamed.Path)
	}
	if _, err := os.Stat(filepath.Join(store.Root(), created.Path)); !os.IsNotExist(err) {
		t.Fatal("the old note survived the rename")
	}
	if renamed.ModTime.IsZero() {
		t.Fatal("the renamed entry has no modification time")
	}

	// A different entry already owns the target name.
	other, err := store.Create(ctx, memory.Entry{Title: "New title", Type: memory.TypeLesson, Body: "Other."})
	if err != nil {
		t.Fatal(err)
	}
	moved, err := store.Rename(ctx, other, "New title")
	if err != nil {
		t.Fatalf("cannot rename onto a taken name: %v", err)
	}
	if moved.Path == other.Path {
		t.Fatalf("the taken name was reused: %q", moved.Path)
	}
}

func TestRemove(t *testing.T) {
	store := newStore(t, Options{Kind: "disk"})
	mustEnsure(t, store)
	ctx := context.Background()
	if err := store.Remove(ctx, memory.Entry{Title: "x"}); err == nil {
		t.Fatal("an entry without a path was removed")
	}
	created, err := store.Create(ctx, memory.Entry{Title: "Gone", Type: memory.TypeLesson, Body: "Body."})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Remove(ctx, created); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(store.Root(), created.Path)); !os.IsNotExist(err) {
		t.Fatal("the note was not removed")
	}
	// A missing note is not an error.
	if err := store.Remove(ctx, created); err != nil {
		t.Fatalf("removing a missing note failed: %v", err)
	}
}

func TestSearchFilesAndMatchers(t *testing.T) {
	store := newStore(t, Options{Kind: "obsidian", Recursive: true})
	mustEnsure(t, store)
	ctx := context.Background()
	if _, err := store.Create(ctx, memory.Entry{
		Title: "A pipefail lesson", Type: memory.TypeLesson, Body: "The pipeline aborts.",
	}); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(store.Root(), "projects"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(store.Root(), "projects", "demo.md"), []byte("The demo project."), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(store.Root(), "MEMORY.md"), []byte("The hot tier."), 0o644); err != nil {
		t.Fatal(err)
	}
	// The index and the native projections are not memory.
	if err := os.WriteFile(filepath.Join(store.Root(), obsidianIndexName), []byte("pipefail"), 0o644); err != nil {
		t.Fatal(err)
	}
	native := filepath.Join(store.Root(), "native", "qwen")
	if err := os.MkdirAll(native, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(native, "pipefail.md"), []byte("pipefail"), 0o644); err != nil {
		t.Fatal(err)
	}

	for _, query := range []string{"pipefail", "PIPEFAIL"} {
		hits, err := store.Search(ctx, query, 10)
		if err != nil {
			t.Fatalf("cannot search %q: %v", query, err)
		}
		if len(hits) == 0 {
			t.Fatalf("the search for %q found nothing", query)
		}
		for _, hit := range hits {
			if strings.Contains(hit.Path, "native/") || hit.Path == obsidianIndexName {
				t.Fatalf("a projection was searched: %+v", hit)
			}
		}
	}
	hits, err := store.Search(ctx, "demo project", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || hits[0].Path != "projects/demo.md" {
		t.Fatalf("the project note was not searched: %+v", hits)
	}
	hits, err = store.Search(ctx, "hot tier", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || hits[0].Path != "MEMORY.md" {
		t.Fatalf("the hot tier was not searched: %+v", hits)
	}

	// An invalid regular expression falls back to a literal match.
	hits, err = store.Search(ctx, "(", 10)
	if err != nil {
		t.Fatalf("an invalid expression is an error: %v", err)
	}
	if len(hits) != 0 {
		t.Fatalf("the literal fallback matched: %+v", hits)
	}
}

func TestAppendLogAndLogLines(t *testing.T) {
	store := newStore(t, Options{Kind: "disk"})
	ctx := context.Background()
	if err := store.AppendLog(ctx, "first"); err != nil {
		t.Fatal(err)
	}
	if err := store.AppendLog(ctx, "second"); err != nil {
		t.Fatal(err)
	}
	lines, err := store.LogLines(ctx, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 2 || !strings.HasSuffix(lines[0], "first") || !strings.HasSuffix(lines[1], "second") {
		t.Fatalf("the log is %v", lines)
	}
	limited, err := store.LogLines(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(limited) != 1 || limited[0] != lines[0] {
		t.Fatalf("the limited log is %v", limited)
	}

	// A missing log folder is empty, not an error.
	missing := New(filepath.Join(t.TempDir(), "absent"), Options{})
	if lines, err := missing.LogLines(ctx, 10); err != nil || lines != nil {
		t.Fatalf("a missing log returned %v %v", lines, err)
	}

	// Only .md files at the top of the log folder are log lines.
	if err := os.WriteFile(filepath.Join(store.LogDir(), "notes.txt"), []byte("- ignored\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(store.LogDir(), "nested"), 0o755); err != nil {
		t.Fatal(err)
	}
	lines, err = store.LogLines(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 2 {
		t.Fatalf("the log picked up non-log files: %v", lines)
	}
}

func TestWriteIndex(t *testing.T) {
	ctx := context.Background()
	corpus := []memory.Entry{
		{Title: "Beta", Type: memory.TypeLesson, Body: "B."},
		{Title: "Alpha", Type: memory.TypeLesson, Body: "A.", Tier: memory.TierHot},
		{Title: "A decision", Type: memory.TypeProject, Body: "D."},
	}

	plain := newStore(t, Options{Kind: "disk"})
	mustEnsure(t, plain)
	path, err := plain.WriteIndex(ctx, corpus)
	if err != nil {
		t.Fatal(err)
	}
	if path != "corpus/INDEX.md" {
		t.Fatalf("the index path is %q", path)
	}
	data, err := os.ReadFile(filepath.Join(plain.Root(), "corpus", "INDEX.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "[Alpha](lesson/alpha.md)") ||
		!strings.Contains(string(data), "## project") {
		t.Fatalf("the plain index is:\n%s", data)
	}
	if strings.Index(string(data), "Alpha") > strings.Index(string(data), "Beta") {
		t.Fatalf("the index is not sorted by title:\n%s", data)
	}

	obsidian := newStore(t, Options{Kind: "obsidian", ObsidianIndex: true})
	path, err = obsidian.WriteIndex(ctx, corpus)
	if err != nil {
		t.Fatal(err)
	}
	if path != obsidianIndexName {
		t.Fatalf("the obsidian index path is %q", path)
	}
	data, err = os.ReadFile(filepath.Join(obsidian.Root(), obsidianIndexName))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "[[Alpha]] — hot") || !strings.Contains(string(data), "[[A decision]]") {
		t.Fatalf("the obsidian index is:\n%s", data)
	}
}

func TestWriteIndexReportsAWriteFailure(t *testing.T) {
	root := t.TempDir()
	store := New(root, Options{Kind: "disk"})
	// corpus is a file, so the folder cannot be created.
	if err := os.WriteFile(filepath.Join(root, "corpus"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := store.WriteIndex(context.Background(), nil); err == nil {
		t.Fatal("the index was written into a file")
	}
}

func TestProjectNotes(t *testing.T) {
	store := newStore(t, Options{Kind: "obsidian", Recursive: true})
	ctx := context.Background()

	// A store without projects/ has no notes.
	names, err := store.ProjectNoteNames(ctx)
	if err != nil || names != nil {
		t.Fatalf("a missing projects folder returned %v %v", names, err)
	}
	if _, ok, err := store.ProjectNote(ctx, "demo"); err != nil || ok {
		t.Fatalf("a missing note returned %v %v", ok, err)
	}

	dir := filepath.Join(store.Root(), "projects")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{
		"demo.md":         "repo-wide",
		"demo@feature.md": "branch-local",
		"alpha.md":        "first",
		"ignored.txt":     "not a note",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Join(dir, "a-folder"), 0o755); err != nil {
		t.Fatal(err)
	}

	note, ok, err := store.ProjectNote(ctx, "demo")
	if err != nil || !ok || note != "repo-wide" {
		t.Fatalf("the project note is %q %v %v", note, ok, err)
	}
	note, ok, err = store.WorktreeNote(ctx, "demo", "feature")
	if err != nil || !ok || note != "branch-local" {
		t.Fatalf("the worktree note is %q %v %v", note, ok, err)
	}
	names, err = store.ProjectNoteNames(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != 3 || names[0] != "alpha" || names[1] != "demo" || names[2] != "demo@feature" {
		t.Fatalf("the note names are %v", names)
	}

	for _, bad := range []string{"", ".", "..", "../outside", "a/b", `a\b`} {
		if _, _, err := store.ProjectNote(ctx, bad); err == nil {
			t.Fatalf("the project note %q was accepted", bad)
		}
		if err := store.RemoveProjectNote(ctx, bad); err == nil {
			t.Fatalf("removing the project note %q was accepted", bad)
		}
	}

	if err := store.RemoveProjectNote(ctx, "demo@feature"); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := store.WorktreeNote(ctx, "demo", "feature"); err != nil || ok {
		t.Fatalf("the worktree note survived: %v %v", ok, err)
	}
	// Removing a missing note is not an error.
	if err := store.RemoveProjectNote(ctx, "missing"); err != nil {
		t.Fatalf("removing a missing note failed: %v", err)
	}
}

func TestDiscoverHonoursRecursiveAndCorpusOnly(t *testing.T) {
	root := t.TempDir()
	note := "---\ntitle: A note\ntype: lesson\n---\n\nBody.\n"
	for _, rel := range []string{"notes/a.md", "native/qwen/b.md", "log/c.md", ".trash/d.md"} {
		path := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(note), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	recursive := New(root, Options{Kind: "obsidian", Recursive: true})
	files, err := recursive.discover()
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || !strings.HasSuffix(files[0], "notes/a.md") {
		t.Fatalf("the recursive discovery found %v", files)
	}

	plain := New(root, Options{Kind: "disk"})
	files, err = plain.discover()
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 0 {
		t.Fatalf("the plain discovery left corpus/ and found %v", files)
	}

	// corpus/<type> is the only place the plain gateway looks.
	if err := os.MkdirAll(filepath.Join(root, "corpus", "lesson"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "corpus", "lesson", "x.md"), []byte(note), 0o644); err != nil {
		t.Fatal(err)
	}
	files, err = plain.discover()
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 {
		t.Fatalf("the plain discovery found %v", files)
	}
}

func TestRelAndMatcherFallbacks(t *testing.T) {
	store := New("/tmp/some-store", Options{})
	if got := store.rel("/tmp/some-store/corpus/a.md"); got != "corpus/a.md" {
		t.Fatalf("rel = %q", got)
	}
	if got := store.rel("/elsewhere/a.md"); got == "" {
		t.Fatal("rel dropped a path outside the store")
	}

	matcher, err := compileMatcher("[")
	if err != nil {
		t.Fatalf("the fallback is an error: %v", err)
	}
	if !matcher("a [ bracket") || matcher("nothing") {
		t.Fatal("the literal fallback is wrong")
	}
	matcher, err = compileMatcher("^a.*b$")
	if err != nil {
		t.Fatal(err)
	}
	if !matcher("axxb") || matcher("bxxa") {
		t.Fatal("the regular expression matcher is wrong")
	}
}

func TestSortOrderAndTypeList(t *testing.T) {
	entries := []memory.Entry{
		{Title: "z", Type: memory.Type("unknown")},
		{Title: "b", Type: memory.TypeReference},
		{Title: "a", Type: memory.TypeUser},
		{Title: "A", Type: memory.TypeLesson},
	}
	sortEntries(entries)
	want := []memory.Type{memory.TypeUser, memory.TypeLesson, memory.TypeReference, memory.Type("unknown")}
	for i, typ := range want {
		if entries[i].Type != typ {
			t.Fatalf("position %d is %q, want %q", i, entries[i].Type, typ)
		}
	}
	if typeOrder(memory.Type("unknown")) != len(memory.Types) {
		t.Fatal("an unknown type is not last")
	}
	if typeList() != "user, lesson, project, reference" {
		t.Fatalf("the type list is %q", typeList())
	}
}

func TestCreateReportsAWriteFailure(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("the permission case needs a non-root user")
	}
	store := newStore(t, Options{Kind: "disk"})
	mustEnsure(t, store)
	lesson := filepath.Join(store.CorpusDir(), "lesson")
	if err := os.Chmod(lesson, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(lesson, 0o755) })
	if _, err := store.Create(context.Background(), memory.Entry{
		Title: "Cannot write", Type: memory.TypeLesson, Body: "Body.",
	}); err == nil {
		t.Fatal("the entry was written into a read-only folder")
	}
}

func TestEnsureReportsAMkdirFailure(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "corpus"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := New(root, Options{Kind: "disk"}).Ensure(context.Background()); err == nil {
		t.Fatal("the layout was created under a file")
	}
}

func TestEntriesReportsADiscoveryFailure(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("the permission case needs a non-root user")
	}
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "corpus"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := New(root, Options{Kind: "disk"}).Entries(context.Background()); err == nil {
		t.Fatal("the entries were read from a corpus file")
	}
}

func TestEntriesSkipsANoteItCannotRead(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("the permission case needs a non-root user")
	}
	store := newStore(t, Options{Kind: "disk"})
	mustEnsure(t, store)
	lesson := filepath.Join(store.CorpusDir(), "lesson")
	if err := os.WriteFile(filepath.Join(lesson, "locked.md"),
		[]byte("---\ntitle: Locked\ntype: lesson\n---\n\nBody.\n"), 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(filepath.Join(lesson, "locked.md"), 0o644) })
	entries, err := store.Entries(context.Background())
	if err != nil {
		t.Fatalf("one unreadable note failed the whole read: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("the unreadable note came back: %+v", entries)
	}
}

func TestCreateReportsAMkdirFailure(t *testing.T) {
	store := newStore(t, Options{Kind: "disk"})
	mustEnsure(t, store)
	lesson := filepath.Join(store.CorpusDir(), "lesson")
	if err := os.RemoveAll(lesson); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(lesson, []byte("not a folder"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Create(context.Background(), memory.Entry{
		Title: "Cannot write", Type: memory.TypeLesson, Body: "Body.",
	}); err == nil {
		t.Fatal("the entry was written where the type folder is a file")
	}
}

func TestUpdateReportsAMkdirFailure(t *testing.T) {
	store := newStore(t, Options{Kind: "disk"})
	mustEnsure(t, store)
	lesson := filepath.Join(store.CorpusDir(), "lesson")
	if err := os.RemoveAll(lesson); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(lesson, []byte("not a folder"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := store.Update(context.Background(), memory.Entry{
		Title: "x", Type: memory.TypeLesson, Path: "corpus/lesson/x.md",
	})
	if err == nil {
		t.Fatal("the update created a folder under a file")
	}
}

func TestRenameReportsItsFailures(t *testing.T) {
	ctx := context.Background()
	store := newStore(t, Options{Kind: "disk"})
	mustEnsure(t, store)
	created, err := store.Create(ctx, memory.Entry{Title: "Movable", Type: memory.TypeLesson, Body: "Body."})
	if err != nil {
		t.Fatal(err)
	}

	// The target folder is a file: the rename cannot create it.
	lesson := filepath.Join(store.CorpusDir(), "lesson")
	if err := os.RemoveAll(lesson); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(lesson, []byte("not a folder"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Rename(ctx, created, "New name"); err == nil {
		t.Fatal("the rename created a folder under a file")
	}
	if err := os.Remove(lesson); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(lesson, 0o755); err != nil {
		t.Fatal(err)
	}

	// The old path is a non-empty folder: it cannot be removed.
	blocked := filepath.Join(lesson, "blocked")
	if err := os.MkdirAll(filepath.Join(blocked, "child"), 0o755); err != nil {
		t.Fatal(err)
	}
	entry := memory.Entry{Title: "Blocked", Type: memory.TypeLesson, Body: "Body.", Path: "corpus/lesson/blocked"}
	if _, err := store.Rename(ctx, entry, "Blocked again"); err == nil {
		t.Fatal("a non-empty folder was removed by the rename")
	}
}

func TestSearchReportsAProjectFolderFailure(t *testing.T) {
	store := newStore(t, Options{Kind: "disk"})
	mustEnsure(t, store)
	if err := os.RemoveAll(filepath.Join(store.Root(), "projects")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(store.Root(), "projects"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	// A file where the projects folder belongs is skipped, not fatal.
	if _, err := store.Search(context.Background(), "anything", 10); err != nil {
		t.Fatalf("a projects file failed the search: %v", err)
	}
}

func TestSearchLimitsTheMatches(t *testing.T) {
	store := newStore(t, Options{Kind: "disk"})
	mustEnsure(t, store)
	body := strings.Repeat("needle\n", 40)
	if _, err := store.Create(context.Background(), memory.Entry{
		Title: "Many matches", Type: memory.TypeLesson, Body: body,
	}); err != nil {
		t.Fatal(err)
	}
	hits, err := store.Search(context.Background(), "needle", 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 10 {
		t.Fatalf("the capped search returned %d hits", len(hits))
	}
}

func TestSearchSkipsAnUnreadableProjectNote(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("the permission case needs a non-root user")
	}
	store := newStore(t, Options{Kind: "disk"})
	mustEnsure(t, store)
	note := filepath.Join(store.Root(), "projects", "locked.md")
	if err := os.WriteFile(note, []byte("needle"), 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(note, 0o644) })
	hits, err := store.Search(context.Background(), "needle", 10)
	if err != nil {
		t.Fatalf("one unreadable note failed the search: %v", err)
	}
	if len(hits) != 0 {
		t.Fatalf("the unreadable note was searched: %+v", hits)
	}
}

func TestSearchReportsADiscoveryFailure(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "corpus"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := New(root, Options{Kind: "disk"}).Search(context.Background(), "x", 10); err == nil {
		t.Fatal("the search read a corpus file")
	}
}

func TestSearchSkipsAProjectFolder(t *testing.T) {
	store := newStore(t, Options{Kind: "disk"})
	mustEnsure(t, store)
	if err := os.MkdirAll(filepath.Join(store.Root(), "projects", "a-folder"), 0o755); err != nil {
		t.Fatal(err)
	}
	hits, err := store.Search(context.Background(), "anything", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 0 {
		t.Fatalf("a project folder was searched: %+v", hits)
	}
}

func TestAppendLogReportsItsFailures(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("the permission case needs a non-root user")
	}
	root := t.TempDir()
	store := New(root, Options{Kind: "disk"})
	if err := os.WriteFile(filepath.Join(root, "log"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := store.AppendLog(context.Background(), "line"); err == nil {
		t.Fatal("the log was appended into a file")
	}

	if err := os.Remove(filepath.Join(root, "log")); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(store.LogDir(), 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(store.LogDir(), 0o755) })
	if err := store.AppendLog(context.Background(), "line"); err == nil {
		t.Fatal("the log was appended into a read-only folder")
	}
}

func TestLogLinesReportsItsReadFailures(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("the permission case needs a non-root user")
	}
	ctx := context.Background()
	store := newStore(t, Options{Kind: "disk"})
	if err := store.AppendLog(ctx, "first"); err != nil {
		t.Fatal(err)
	}

	// A log file that cannot be read is skipped, not fatal.
	entries, err := os.ReadDir(store.LogDir())
	if err != nil {
		t.Fatal(err)
	}
	locked := filepath.Join(store.LogDir(), entries[0].Name())
	if err := os.Chmod(locked, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o644) })
	lines, err := store.LogLines(ctx, 10)
	if err != nil {
		t.Fatalf("one unreadable log file failed the read: %v", err)
	}
	if len(lines) != 0 {
		t.Fatalf("the unreadable log file was read: %v", lines)
	}

	// An unreadable log folder is an error.
	if err := os.Chmod(store.LogDir(), 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(store.LogDir(), 0o755) })
	if _, err := store.LogLines(ctx, 10); err == nil {
		t.Fatal("an unreadable log folder was not an error")
	}
}

func TestWriteIndexReportsAWriteFailureOnTheIndexFile(t *testing.T) {
	ctx := context.Background()

	plain := newStore(t, Options{Kind: "disk"})
	mustEnsure(t, plain)
	index := filepath.Join(plain.CorpusDir(), "INDEX.md")
	if err := os.MkdirAll(index, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := plain.WriteIndex(ctx, nil); err == nil {
		t.Fatal("the plain index was written over a folder")
	}

	obsidian := newStore(t, Options{Kind: "obsidian", ObsidianIndex: true})
	if err := os.MkdirAll(filepath.Join(obsidian.Root(), obsidianIndexName), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := obsidian.WriteIndex(ctx, nil); err == nil {
		t.Fatal("the obsidian index was written over a folder")
	}
}

func TestProjectNoteReportsItsFailures(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("the permission case needs a non-root user")
	}
	ctx := context.Background()
	store := newStore(t, Options{Kind: "disk"})
	mustEnsure(t, store)
	dir := filepath.Join(store.Root(), "projects")

	// An unreadable note is an error, not a missing note.
	locked := filepath.Join(dir, "locked.md")
	if err := os.WriteFile(locked, []byte("x"), 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o644) })
	if _, _, err := store.ProjectNote(ctx, "locked"); err == nil {
		t.Fatal("an unreadable project note was reported as missing")
	}

	// A file where projects/ belongs is an error.
	broken := newStore(t, Options{Kind: "disk"})
	mustEnsure(t, broken)
	if err := os.RemoveAll(filepath.Join(broken.Root(), "projects")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(broken.Root(), "projects"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := broken.ProjectNoteNames(ctx); err == nil {
		t.Fatal("a projects file was listed as a folder")
	}

	// A note that is a non-empty folder cannot be removed.
	blocked := filepath.Join(dir, "blocked.md", "child")
	if err := os.MkdirAll(blocked, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := store.RemoveProjectNote(ctx, "blocked"); err == nil {
		t.Fatal("a non-empty project folder was removed")
	}
}

func TestTitleOfAnUnreadableNoteIsEmpty(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("the permission case needs a non-root user")
	}
	store := newStore(t, Options{Kind: "disk"})
	mustEnsure(t, store)
	note := filepath.Join(store.CorpusDir(), "lesson", "locked.md")
	if err := os.WriteFile(note, []byte("---\ntitle: Locked\ntype: lesson\n---\n"), 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(note, 0o644) })
	if got := store.titleOf(note); got != "" {
		t.Fatalf("titleOf returned %q", got)
	}
	if got := store.titleOf(filepath.Join(store.Root(), "missing.md")); got != "" {
		t.Fatalf("titleOf of a missing note returned %q", got)
	}
}
