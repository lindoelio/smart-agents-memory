package sources

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDiscoverMatchesPatternsAndExcludes(t *testing.T) {
	root := t.TempDir()
	files := map[string]string{
		"project/agent-transcripts/s/s.jsonl":            "{}",
		"project/agent-transcripts/s/subagents/x.jsonl":  "{}",
		"project/agent-transcripts/s/compaction/x.jsonl": "{}",
		"project/agent-transcripts/s/.hidden.jsonl":      "{}",
		"project/agent-transcripts/s/s.jsonl.lock":       "{}",
	}
	for name, content := range files {
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	source := Source{
		ID: "cursor", Kind: KindFiles, Format: FormatMessagesJSONL,
		Roots: []string{root}, Patterns: []string{"*/agent-transcripts/*/*.jsonl"},
		Exclude: []string{"**/subagents/**", "**/compaction/**"}, Enabled: true,
	}
	got := Discover(Registry{Sources: []Source{source}})
	if len(got) != 1 || filepath.Base(got[0].Path) != "s.jsonl" {
		t.Fatalf("the discovery is %+v", got)
	}
}

func TestDiscoverSkipsMissingRootsAndSQLite(t *testing.T) {
	root := t.TempDir()
	db := filepath.Join(root, "opencode.db")
	if err := os.WriteFile(db, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	reg := Registry{Sources: []Source{
		{ID: "missing", Kind: KindFiles, Format: FormatMessagesJSONL, Roots: []string{filepath.Join(root, "nope")}, Patterns: []string{"**/*.jsonl"}, Enabled: true},
		{ID: "db", Kind: KindSQLite, Format: FormatSessionMessageSQLite, Roots: []string{db}, Enabled: true},
		{ID: "dir", Kind: KindSQLite, Format: FormatSessionMessageSQLite, Roots: []string{root}, Enabled: true},
	}}
	got := Discover(reg)
	if len(got) != 1 || got[0].Path != db {
		t.Fatalf("the discovery is %+v", got)
	}
	if got[0].Size != 1 || got[0].ModTime.IsZero() {
		t.Fatalf("the candidate is %+v", got[0])
	}
}

func TestSourceMatchesKeepsUnknownExtensions(t *testing.T) {
	source := Source{Patterns: []string{"**/*.txt"}, Enabled: true}
	if !sourceMatches(source, "/root", "/root/a/b.txt") {
		t.Fatal("the txt file did not match")
	}
	if sourceMatches(source, "/root", "/root/a/b.jsonl") {
		t.Fatal("the jsonl file matched a txt pattern")
	}
}

func TestCandidateSortIsDeterministic(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"b.jsonl", "a.jsonl"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("{}"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	got := Discover(Registry{Sources: []Source{{ID: "x", Kind: KindFiles, Format: FormatMessagesJSONL, Roots: []string{root}, Patterns: []string{"**/*.jsonl"}, Enabled: true}}})
	if len(got) != 2 || filepath.Base(got[0].Path) != "a.jsonl" {
		t.Fatalf("the order is %+v", got)
	}
}

func TestDiscoverSkipsAFileRoot(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "not-a-dir")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := Discover(Registry{Sources: []Source{{ID: "x", Kind: KindFiles, Format: FormatMessagesJSONL, Roots: []string{file}, Patterns: []string{"**/*.jsonl"}, Enabled: true}}})
	if len(got) != 0 {
		t.Fatalf("a file root produced %+v", got)
	}
}
