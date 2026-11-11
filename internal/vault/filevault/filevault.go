// Package filevault is the shared filesystem base of the Obsidian and disk
// gateways.
//
// Both store one note per memory, with YAML frontmatter and the body in
// Markdown. They differ in how they discover notes and how they publish the
// index, not in how they read or write them.
package filevault

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/lindoelio/smart-agents-memory/internal/memory"
	"github.com/lindoelio/smart-agents-memory/internal/vault"
)

// Names of the Obsidian-native index.
const (
	obsidianIndexTitle = "Smart Agents Memory"
	obsidianIndexName  = "Smart Agents Memory.md"
)

// Options tune how a store discovers notes and where it writes them.
type Options struct {
	// Recursive discovers notes anywhere under the root, the way Obsidian
	// itself does, instead of only under corpus/<type>/.
	Recursive bool
	// ObsidianIndex publishes an Obsidian-native index note with wikilinks.
	ObsidianIndex bool
	// Kind is the registered name of the gateway.
	Kind string
}

// Store is a corpus kept in a directory of Markdown notes.
type Store struct {
	root  string
	opts  Options
	nowFn func() time.Time
}

// New builds a store rooted at dir.
func New(dir string, opts Options) *Store {
	return &Store{root: dir, opts: opts, nowFn: time.Now}
}

// Kind is the registered name of the gateway.
func (s *Store) Kind() string { return s.opts.Kind }

// Root is where the store lives.
func (s *Store) Root() string { return s.root }

// Ready reports whether the store directory exists.
func (s *Store) Ready(_ context.Context) error {
	info, err := os.Stat(s.root)
	if err != nil || !info.IsDir() {
		return fmt.Errorf("no memory store at %s — run `smart-agents-memory install` to create one", s.root)
	}
	return nil
}

// Ensure creates the folders of a file store. It does not write entries.
func (s *Store) Ensure(_ context.Context) error {
	for _, dir := range []string{
		filepath.Join(s.CorpusDir(), string(memory.TypeUser)),
		filepath.Join(s.CorpusDir(), string(memory.TypeLesson)),
		filepath.Join(s.CorpusDir(), string(memory.TypeProject)),
		filepath.Join(s.CorpusDir(), string(memory.TypeReference)),
		filepath.Join(s.root, "projects"),
		s.LogDir(),
	} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	return nil
}

// Problems lists the notes that could not be read. A note without
// frontmatter is not an entry, and it is not reported here.
func (s *Store) Problems(_ context.Context) ([]string, error) {
	var problems []string
	err := filepath.WalkDir(s.root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			problems = append(problems, s.rel(path)+": "+err.Error())
			return nil
		}
		if d.IsDir() {
			if skipDir(d.Name()) && path != s.root {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(d.Name(), ".md") {
			return nil
		}
		f, err := os.Open(path)
		if err != nil {
			problems = append(problems, s.rel(path)+": "+err.Error())
			return nil
		}
		f.Close()
		return nil
	})
	if err != nil && !os.IsNotExist(err) {
		return problems, err
	}
	return problems, nil
}

// CorpusDir is the folder where new entries are written.
func (s *Store) CorpusDir() string { return filepath.Join(s.root, "corpus") }

// LogDir is the folder holding the append-only write log.
func (s *Store) LogDir() string { return filepath.Join(s.root, "log") }

// Entries returns every entry in the store.
func (s *Store) Entries(_ context.Context) ([]memory.Entry, error) {
	paths, err := s.discover()
	if err != nil {
		return nil, err
	}
	out := make([]memory.Entry, 0, len(paths))
	for _, p := range paths {
		entry, ok, err := s.readEntry(p)
		if err != nil {
			continue
		}
		if !ok {
			continue
		}
		out = append(out, entry)
	}
	sortEntries(out)
	return out, nil
}

// Create writes a new entry and returns it as persisted.
func (s *Store) Create(_ context.Context, e memory.Entry) (memory.Entry, error) {
	if !e.Type.Valid() {
		return e, fmt.Errorf("invalid entry type %q (use %s)", e.Type, typeList())
	}
	dir := filepath.Join(s.CorpusDir(), string(e.Type))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return e, err
	}
	target := filepath.Join(dir, memory.Slugify(e.Title)+".md")
	target, err := s.uniquePath(target)
	if err != nil {
		return e, err
	}
	if err := os.WriteFile(target, []byte(memory.Format(e)), 0o644); err != nil {
		return e, err
	}
	e.Path = s.rel(target)
	if info, err := os.Stat(target); err == nil {
		e.ModTime = info.ModTime()
	}
	return e, nil
}

// Update overwrites an existing entry.
func (s *Store) Update(_ context.Context, e memory.Entry) error {
	if e.Path == "" {
		return fmt.Errorf("entry has no path")
	}
	target := filepath.Join(s.root, e.Path)
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	return os.WriteFile(target, []byte(memory.Format(e)), 0o644)
}

// Rename writes an entry under a new title and removes the old one.
func (s *Store) Rename(_ context.Context, e memory.Entry, title string) (memory.Entry, error) {
	if e.Path == "" {
		return e, fmt.Errorf("entry has no path")
	}
	old := filepath.Join(s.root, e.Path)
	e.Title = title
	target := filepath.Join(s.CorpusDir(), string(e.Type), memory.Slugify(title)+".md")
	if target == old {
		if err := os.WriteFile(old, []byte(memory.Format(e)), 0o644); err != nil {
			return e, err
		}
		e.Path = s.rel(old)
		return e, nil
	}
	target, err := s.uniquePath(target)
	if err != nil {
		return e, err
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return e, err
	}
	if err := os.WriteFile(target, []byte(memory.Format(e)), 0o644); err != nil {
		return e, err
	}
	if err := os.Remove(old); err != nil && !os.IsNotExist(err) {
		return e, err
	}
	e.Path = s.rel(target)
	if info, err := os.Stat(target); err == nil {
		e.ModTime = info.ModTime()
	}
	return e, nil
}

// Remove deletes an entry from the store.
func (s *Store) Remove(_ context.Context, e memory.Entry) error {
	if e.Path == "" {
		return fmt.Errorf("entry has no path")
	}
	err := os.Remove(filepath.Join(s.root, e.Path))
	if err != nil && os.IsNotExist(err) {
		return nil
	}
	return err
}

// Search returns the lines of the store that match the query.
func (s *Store) Search(_ context.Context, query string, limit int) ([]vault.Hit, error) {
	if limit <= 0 {
		limit = 30
	}
	matcher, err := compileMatcher(query)
	if err != nil {
		return nil, err
	}
	files, err := s.searchableFiles()
	if err != nil {
		return nil, err
	}
	var hits []vault.Hit
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		title := s.titleOf(f)
		for i, line := range strings.Split(string(data), "\n") {
			if !matcher(line) {
				continue
			}
			hits = append(hits, vault.Hit{
				Path:  s.rel(f),
				Line:  i + 1,
				Text:  strings.TrimRight(line, "\r"),
				Title: title,
			})
			if len(hits) >= limit*10 {
				break
			}
		}
	}
	return hits, nil
}

// AppendLog appends one line to the write log of the current day.
func (s *Store) AppendLog(_ context.Context, line string) error {
	dir := s.LogDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	now := s.nowFn()
	path := filepath.Join(dir, now.Format("2006-01-02")+".md")
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = fmt.Fprintf(f, "- %s | %s\n", now.Format("15:04"), line)
	return err
}

// LogLines returns the lines of the write log, newest first.
func (s *Store) LogLines(_ context.Context, limit int) ([]string, error) {
	dir := s.LogDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".md") {
			names = append(names, e.Name())
		}
	}
	sort.Sort(sort.Reverse(sort.StringSlice(names)))
	var out []string
	for _, n := range names {
		data, err := os.ReadFile(filepath.Join(dir, n))
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(data), "\n") {
			if strings.TrimSpace(line) != "" {
				out = append(out, line)
			}
		}
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// IndexPath is where the index of the corpus lives.
func (s *Store) IndexPath() string {
	if s.opts.ObsidianIndex {
		return filepath.Join(s.root, obsidianIndexName)
	}
	return filepath.Join(s.CorpusDir(), "INDEX.md")
}

// WriteIndex publishes the index of the corpus.
//
// The Obsidian gateway writes an Obsidian-native note with wikilinks, so the
// memory is navigable from the vault. The disk gateway writes a plain
// Markdown index.
func (s *Store) WriteIndex(_ context.Context, corpus []memory.Entry) (string, error) {
	if s.opts.ObsidianIndex {
		return s.writeObsidianIndex(corpus)
	}
	return s.writePlainIndex(corpus)
}

// writeObsidianIndex writes the Obsidian-native index note.
func (s *Store) writeObsidianIndex(corpus []memory.Entry) (string, error) {
	var b strings.Builder
	b.WriteString("# " + obsidianIndexTitle + "\n\n")
	b.WriteString(fmt.Sprintf("_Updated at %s. One corpus, many harnesses._\n\n", time.Now().Format("2006-01-02 15:04")))
	for _, typ := range memory.Types {
		var subset []memory.Entry
		for _, e := range corpus {
			if e.Type == typ {
				subset = append(subset, e)
			}
		}
		if len(subset) == 0 {
			continue
		}
		b.WriteString("## " + string(typ) + "\n\n")
		sort.SliceStable(subset, func(i, j int) bool {
			return strings.ToLower(subset[i].Title) < strings.ToLower(subset[j].Title)
		})
		for _, e := range subset {
			b.WriteString("- [[" + e.Title + "]]")
			if e.EffectiveTier() == memory.TierHot {
				b.WriteString(" — hot")
			}
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}
	path := filepath.Join(s.root, obsidianIndexName)
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		return "", err
	}
	return s.rel(path), nil
}

// writePlainIndex writes the plain Markdown index.
func (s *Store) writePlainIndex(corpus []memory.Entry) (string, error) {
	var b strings.Builder
	b.WriteString("# Corpus index\n\n")
	b.WriteString(fmt.Sprintf("_Updated at %s. The corpus is the single source of truth; `native/` is a projection._\n\n",
		time.Now().Format("2006-01-02 15:04")))
	for _, typ := range memory.Types {
		var subset []memory.Entry
		for _, e := range corpus {
			if e.Type == typ {
				subset = append(subset, e)
			}
		}
		if len(subset) == 0 {
			continue
		}
		b.WriteString("## " + string(typ) + "\n\n")
		sort.SliceStable(subset, func(i, j int) bool {
			return strings.ToLower(subset[i].Title) < strings.ToLower(subset[j].Title)
		})
		for _, e := range subset {
			rel := string(e.Type) + "/" + memory.Slugify(e.Title) + ".md"
			b.WriteString("- [" + e.Title + "](" + rel + ")\n")
		}
		b.WriteString("\n")
	}
	path := filepath.Join(s.CorpusDir(), "INDEX.md")
	if err := os.MkdirAll(s.CorpusDir(), 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		return "", err
	}
	return s.rel(path), nil
}

// ProjectNote returns the note of a project, when the store has one.
func (s *Store) ProjectNote(_ context.Context, name string) (string, bool, error) {
	path := filepath.Join(s.root, "projects", name+".md")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", false, nil
		}
		return "", false, err
	}
	return string(data), true, nil
}

// discover finds the notes that hold entries.
func (s *Store) discover() ([]string, error) {
	var out []string
	if s.opts.Recursive {
		err := filepath.WalkDir(s.root, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.IsDir() {
				if skipDir(d.Name()) && path != s.root {
					return filepath.SkipDir
				}
				return nil
			}
			if strings.HasSuffix(d.Name(), ".md") {
				out = append(out, path)
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	} else {
		entries, err := os.ReadDir(s.CorpusDir())
		if err != nil {
			if os.IsNotExist(err) {
				return nil, nil
			}
			return nil, err
		}
		for _, dir := range entries {
			if !dir.IsDir() {
				continue
			}
			files, err := os.ReadDir(filepath.Join(s.CorpusDir(), dir.Name()))
			if err != nil {
				continue
			}
			for _, f := range files {
				if !f.IsDir() && strings.HasSuffix(f.Name(), ".md") {
					out = append(out, filepath.Join(s.CorpusDir(), dir.Name(), f.Name()))
				}
			}
		}
	}
	sort.Strings(out)
	return out, nil
}

// searchableFiles returns the notes a search should scan.
//
// Only the entries, the notes of the projects and the hot tier. The index
// notes and the readme of the store are not memory: searching them would
// repeat every entry twice.
func (s *Store) searchableFiles() ([]string, error) {
	var out []string
	corpus, err := s.discover()
	if err != nil {
		return nil, err
	}
	for _, f := range corpus {
		if _, ok, err := s.readEntry(f); err != nil || !ok {
			continue
		}
		out = append(out, f)
	}
	projects := filepath.Join(s.root, "projects")
	if files, err := os.ReadDir(projects); err == nil {
		for _, f := range files {
			if !f.IsDir() && strings.HasSuffix(f.Name(), ".md") {
				out = append(out, filepath.Join(projects, f.Name()))
			}
		}
	}
	hot := filepath.Join(s.root, "MEMORY.md")
	if _, err := os.Stat(hot); err == nil {
		out = append(out, hot)
	}
	return out, nil
}

// readEntry parses a note into an entry.
func (s *Store) readEntry(path string) (memory.Entry, bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return memory.Entry{}, false, err
	}
	entry, ok := memory.Parse(string(data))
	if !ok {
		return memory.Entry{}, false, nil
	}
	// A note with no type is not an entry: it is a project note, an
	// index, or a projection. A note with a legacy type is an entry
	// waiting to be migrated, and the doctor reports the vocabulary.
	if entry.Type == "" {
		return memory.Entry{}, false, nil
	}
	entry.Path = s.rel(path)
	if info, err := os.Stat(path); err == nil {
		entry.ModTime = info.ModTime()
	}
	return entry, true, nil
}

// titleOf returns the title of the entry stored in a note.
func (s *Store) titleOf(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	if entry, ok := memory.Parse(string(data)); ok {
		return entry.Title
	}
	return ""
}

// uniquePath appends a counter until the path is free.
func (s *Store) uniquePath(target string) (string, error) {
	if _, err := os.Stat(target); os.IsNotExist(err) {
		return target, nil
	}
	base := strings.TrimSuffix(target, ".md")
	for n := 2; n < 1000; n++ {
		candidate := fmt.Sprintf("%s-%d.md", base, n)
		if _, err := os.Stat(candidate); os.IsNotExist(err) {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("cannot find a free name for %s", target)
}

// rel returns the path relative to the root of the store.
func (s *Store) rel(path string) string {
	rel, err := filepath.Rel(s.root, path)
	if err != nil {
		return path
	}
	return filepath.ToSlash(rel)
}

// skipDir reports whether a directory is never part of the corpus.
func skipDir(name string) bool {
	switch name {
	case "native", "log", ".obsidian", ".trash", ".git", "node_modules", "skills", "bin", "docs":
		return true
	}
	return strings.HasPrefix(name, ".")
}

// sortEntries orders entries by type, then title.
func sortEntries(entries []memory.Entry) {
	sort.SliceStable(entries, func(i, j int) bool {
		ti, tj := typeOrder(entries[i].Type), typeOrder(entries[j].Type)
		if ti != tj {
			return ti < tj
		}
		return strings.ToLower(entries[i].Title) < strings.ToLower(entries[j].Title)
	})
}

// typeOrder gives every type a fixed position.
func typeOrder(t memory.Type) int {
	for i, known := range memory.Types {
		if t == known {
			return i
		}
	}
	return len(memory.Types)
}

// typeList renders the valid types for an error message.
func typeList() string {
	parts := make([]string, 0, len(memory.Types))
	for _, t := range memory.Types {
		parts = append(parts, string(t))
	}
	return strings.Join(parts, ", ")
}

// compileMatcher builds the line matcher of a query.
//
// The query is a regular expression when it compiles, and a
// case-insensitive substring match otherwise, so a plain word always works.
func compileMatcher(query string) (func(string) bool, error) {
	if re, err := regexp.Compile("(?i)" + query); err == nil {
		return func(line string) bool { return re.MatchString(line) }, nil
	}
	needle := strings.ToLower(query)
	return func(line string) bool { return strings.Contains(strings.ToLower(line), needle) }, nil
}
