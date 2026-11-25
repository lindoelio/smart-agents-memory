package sources

import (
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// Candidate is one file or database discovered by the registry.
type Candidate struct {
	// Source is the descriptor that found it.
	Source Source
	// Path is the absolute path.
	Path string
	// Size is the size at discovery time.
	Size int64
	// ModTime is the modification time at discovery time.
	ModTime time.Time
}

// Discover returns the candidates of every enabled source.
//
// A missing root is not an error: the harness may not be installed. A walk
// error on one subtree is skipped so the other sources still run.
func Discover(reg Registry) []Candidate {
	var out []Candidate
	seen := map[string]bool{}
	for _, source := range reg.Enabled() {
		if source.Kind == KindSQLite {
			for _, root := range source.Roots {
				path := ExpandPath(root)
				info, err := os.Stat(path)
				if err != nil || info.IsDir() {
					continue
				}
				c := Candidate{Source: source, Path: path, Size: info.Size(), ModTime: info.ModTime()}
				key := string(source.ID) + ":" + c.Path
				if !seen[key] {
					seen[key] = true
					out = append(out, c)
				}
			}
			continue
		}
		for _, root := range source.Roots {
			root = ExpandPath(root)
			info, err := os.Stat(root)
			if err != nil || !info.IsDir() {
				continue
			}
			_ = filepath.WalkDir(root, func(p string, entry os.DirEntry, err error) error {
				if err != nil {
					return nil
				}
				if entry.IsDir() {
					name := entry.Name()
					if p != root && (strings.HasPrefix(name, ".") || name == "node_modules" || name == ".git") {
						return filepath.SkipDir
					}
					return nil
				}
				if !sourceMatches(source, root, p) {
					return nil
				}
				info, err := entry.Info()
				if err != nil {
					return nil
				}
				key := string(source.Format) + ":" + p
				if seen[key] {
					return nil
				}
				seen[key] = true
				out = append(out, Candidate{Source: source, Path: p, Size: info.Size(), ModTime: info.ModTime()})
				return nil
			})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Source.ID != out[j].Source.ID {
			return out[i].Source.ID < out[j].Source.ID
		}
		return out[i].Path < out[j].Path
	})
	return out
}

// sourceMatches tests the relative path against the source patterns and
// exclusions. The path is slash-normalized so the same globs work everywhere.
func sourceMatches(source Source, root, p string) bool {
	rel, err := filepath.Rel(root, p)
	if err != nil {
		return false
	}
	rel = filepath.ToSlash(rel)
	base := strings.ToLower(path.Base(rel))
	if strings.HasSuffix(base, ".lock") || strings.HasSuffix(base, ".tmp") || strings.HasPrefix(base, ".") {
		return false
	}
	matched := false
	for _, pattern := range source.Patterns {
		if matchGlob(pattern, rel) {
			matched = true
			break
		}
	}
	if !matched {
		return false
	}
	for _, pattern := range source.Exclude {
		if matchGlob(pattern, rel) {
			return false
		}
	}
	return true
}

// matchGlob matches a slash-separated glob with ** support.
func matchGlob(pattern, name string) bool {
	pattern = strings.TrimPrefix(filepath.ToSlash(pattern), "./")
	name = strings.TrimPrefix(filepath.ToSlash(name), "./")
	expr := globRegexp(pattern)
	ok, err := regexp.MatchString(expr, name)
	return err == nil && ok
}

func globRegexp(pattern string) string {
	var b strings.Builder
	b.WriteString("^")
	for i := 0; i < len(pattern); i++ {
		switch pattern[i] {
		case '*':
			if i+1 < len(pattern) && pattern[i+1] == '*' {
				i++
				if i+1 < len(pattern) && pattern[i+1] == '/' {
					i++
					b.WriteString("(?:.*/)?")
				} else {
					b.WriteString(".*")
				}
			} else {
				b.WriteString("[^/]*")
			}
		case '?':
			b.WriteString("[^/]")
		default:
			b.WriteString(regexp.QuoteMeta(string(pattern[i])))
		}
	}
	b.WriteString("$")
	return b.String()
}

func homeDir() string {
	if home, err := os.UserHomeDir(); err == nil {
		return home
	}
	return os.Getenv("HOME")
}
