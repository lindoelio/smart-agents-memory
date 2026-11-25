package app

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lindoelio/smart-agents-memory/internal/memory"
)

func TestTypeListNamesEveryType(t *testing.T) {
	list := typeList()
	for _, typ := range memory.Types {
		if !strings.Contains(list, string(typ)) {
			t.Fatalf("the list %q is missing %q", list, typ)
		}
	}
	if strings.Count(list, ", ") != len(memory.Types)-1 {
		t.Fatalf("the list is %q", list)
	}
}

func TestLegacyTypeMapping(t *testing.T) {
	cases := map[string]memory.Type{
		"usuario":    memory.TypeUser,
		"licao":      memory.TypeLesson,
		"projeto":    memory.TypeProject,
		"referencia": memory.TypeReference,
		"":           "",
		"unknown":    "",
	}
	for legacy, want := range cases {
		if got := legacyType(legacy); got != want {
			t.Fatalf("legacyType(%q) = %q, want %q", legacy, got, want)
		}
	}
}

func TestStripFrontmatterEdges(t *testing.T) {
	cases := []struct{ name, in, want string }{
		{"no frontmatter", "plain text", "plain text"},
		{"unterminated", "---\ntitle: A\n\nBody.", "---\ntitle: A\n\nBody."},
		{"closed", "---\ntitle: A\n---\n\nBody.", "\nBody."},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := stripFrontmatter(tc.in); got != tc.want {
				t.Fatalf("stripFrontmatter(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestExpandPathEdges(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if got := expandPath("~"); got != home {
		t.Fatalf("expandPath(~) = %q", got)
	}
	if got := expandPath("~/chats"); got != filepath.Join(home, "chats") {
		t.Fatalf("expandPath(~/chats) = %q", got)
	}
	if got := expandPath("/absolute/path"); got != "/absolute/path" {
		t.Fatalf("expandPath kept %q", got)
	}
}

func TestValidScopeNameMatrix(t *testing.T) {
	cases := map[string]bool{
		"":           false,
		".":          false,
		"..":         false,
		"a/b":        false,
		"a b":        false,
		"feature":    true,
		"feature-2":  true,
		"feature_2":  true,
		"feature.2":  true,
		"branch@two": false,
	}
	for name, want := range cases {
		if got := validScopeName(name); got != want {
			t.Fatalf("validScopeName(%q) = %v, want %v", name, got, want)
		}
	}
}

func TestResolveScopeCWD(t *testing.T) {
	dir := t.TempDir()
	if _, ok := resolveScopeCWD(dir); ok {
		t.Fatal("a plain directory was resolved as a repository")
	}
	repo := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	info, ok := resolveScopeCWD(repo)
	if !ok || info.RepoSlug == "" {
		t.Fatalf("the repository was not resolved: %+v %v", info, ok)
	}
	t.Chdir(repo)
	info, ok = resolveScopeCWD("")
	if !ok || info.RepoSlug == "" {
		t.Fatalf("the process directory was not resolved: %+v %v", info, ok)
	}
}

func TestDreamDescriptionEdges(t *testing.T) {
	if got := dreamDescription(memory.Entry{Title: "A title", Body: "\n\n"}); got != "A title" {
		t.Fatalf("the empty body fell back to %q", got)
	}
	short := dreamDescription(memory.Entry{Title: "A title", Body: "## The rule\n\nMore."})
	if short != "The rule" {
		t.Fatalf("the description is %q", short)
	}
	long := strings.Repeat("word ", 40)
	got := dreamDescription(memory.Entry{Title: "A title", Body: long})
	if len(got) > 120 || !strings.HasSuffix(got, "...") {
		t.Fatalf("the long description is %q", got)
	}
	unbroken := strings.Repeat("x", 200)
	got = dreamDescription(memory.Entry{Title: "A title", Body: unbroken})
	if len(got) != 120 || !strings.HasSuffix(got, "...") {
		t.Fatalf("the unbroken description is %q", got)
	}
}

func TestLoadDreamStateDefaults(t *testing.T) {
	dir := t.TempDir()
	a := &App{LocalRoot: dir}
	path := filepath.Join(dir, dreamStateFile)
	if err := os.WriteFile(path, []byte(`{"dirty_mutations":4}`), 0o644); err != nil {
		t.Fatal(err)
	}
	state := a.loadDreamState()
	if state.Version != 1 || state.DirtyMutations != 4 {
		t.Fatalf("the state is %+v", state)
	}
	if err := os.WriteFile(path, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if state := a.loadDreamState(); state.Version != 1 || state.DirtyMutations != 0 {
		t.Fatalf("the broken state is %+v", state)
	}
}

func TestSaveDreamStateAndBumpDirtyReportFailures(t *testing.T) {
	file := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(file, []byte("not a directory"), 0o644); err != nil {
		t.Fatal(err)
	}
	a := &App{LocalRoot: file, Err: &bytes.Buffer{}}
	if err := a.saveDreamState(DreamState{}); err == nil {
		t.Fatal("the save failure was swallowed")
	}
	a.bumpDirty()
	if !strings.Contains(a.Err.(*bytes.Buffer).String(), "cannot update dream state") {
		t.Fatalf("the warning is %q", a.Err)
	}
	if _, err := a.acquireDreamLock(); err == nil {
		t.Fatal("the lock failure was swallowed")
	}
}

func TestConfigureSystemdWritesDryRunsAndRemoves(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	result, err := configureSystemd("/tmp/smart-agents-memory", SchedulerOptions{Interval: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Paths) != 2 {
		t.Fatalf("the result is %+v", result)
	}
	for _, path := range result.Paths {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("the unit %s: %v", path, err)
		}
	}
	dry, err := configureSystemd("/tmp/smart-agents-memory", SchedulerOptions{Interval: time.Minute, DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	if !dry.DryRun || dry.Content == "" {
		t.Fatalf("the dry run is %+v", dry)
	}
	removed, err := configureSystemd("/tmp/smart-agents-memory", SchedulerOptions{Remove: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range removed.Paths {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("the unit %s survived", path)
		}
	}
}

func TestConfigureSystemdReportsAnUnwritableDirectory(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := os.MkdirAll(filepath.Join(home, ".config"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".config", "systemd"), []byte("not a directory"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := configureSystemd("/tmp/smart-agents-memory", SchedulerOptions{Interval: time.Minute}); err == nil {
		t.Fatal("the directory failure was swallowed")
	}
}

func TestLaunchdPlistFallsBackToFiveMinutes(t *testing.T) {
	plist := launchdPlist("/tmp/bin", "/tmp/log", 0)
	if !strings.Contains(plist, "<integer>300</integer>") {
		t.Fatalf("the plist is %s", plist)
	}
}

func TestRankRecallWithoutTerms(t *testing.T) {
	if got := rankRecall([]recallItem{{entry: memory.Entry{Title: "A rule"}}}, "   ", nil, ""); got != nil {
		t.Fatalf("the ranking is %+v", got)
	}
}

func TestFilterRecallItemsByCategory(t *testing.T) {
	items := []recallItem{
		{entry: memory.Entry{Title: "A test rule", Category: "testing_standard"}},
		{entry: memory.Entry{Title: "A build rule", Category: "build_configuration"}},
	}
	if got := filterRecallItems(items, nil); len(got) != 2 {
		t.Fatalf("the unfiltered list is %+v", got)
	}
	got := filterRecallItems(items, []string{"testing_standard"})
	if len(got) != 1 || got[0].entry.Title != "A test rule" {
		t.Fatalf("the filtered list is %+v", got)
	}
	if got := filterRecallItems(items, []string{"missing_category"}); len(got) != 0 {
		t.Fatalf("the empty selection is %+v", got)
	}
}

func TestRenderRecallItemsEdges(t *testing.T) {
	if got := renderRecallItems(nil, 100); got != "" {
		t.Fatalf("the empty render is %q", got)
	}
	items := []recallItem{{entry: memory.Entry{Title: "A rule", Description: "A description", Body: strings.Repeat("body ", 20)}}}
	text := renderRecallItems(items, 10)
	if len(text) != 10 {
		t.Fatalf("the clamp is %q", text)
	}
}
