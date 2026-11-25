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
				candidate := ExpandPath(root)
				info, err := os.Stat(candidate)
				if err != nil || info.IsDir() {
					continue
				}
				c := Candidate{Source: source, Path: candidate, Size: info.Size(), ModTime: info.ModTime()}
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
			var primaries, fallbacks []Candidate
			primaryDirs := map[string]bool{}
			_ = filepath.WalkDir(root, func(p string, entry os.DirEntry, err error) error {
				if err != nil {
					return nil
				}
				if entry.IsDir() {
					name := entry.Name()
					if p != root && (name == "node_modules" || name == ".git") {
						return filepath.SkipDir
					}
					return nil
				}
				rel, err := filepath.Rel(root, p)
				if err != nil {
					return nil
				}
				rel = filepath.ToSlash(rel)
				if sourceExcluded(source, rel) {
					return nil
				}
				info, err := entry.Info()
				if err != nil {
					return nil
				}
				switch {
				case matchesAny(source.Patterns, rel):
					primaryDirs[filepath.Dir(p)] = true
					primaries = append(primaries, Candidate{Source: source, Path: p, Size: info.Size(), ModTime: info.ModTime()})
				case matchesAny(source.Fallback, rel):
					fallbackSource := source
					if source.FallbackFormat != "" {
						fallbackSource.Format = source.FallbackFormat
					}
					fallbacks = append(fallbacks, Candidate{Source: fallbackSource, Path: p, Size: info.Size(), ModTime: info.ModTime()})
				}
				return nil
			})
			for _, c := range primaries {
				key := string(c.Source.Format) + ":" + c.Path
				if seen[key] {
					continue
				}
				seen[key] = true
				out = append(out, c)
			}
			for _, c := range fallbacks {
				if primaryDirs[filepath.Dir(c.Path)] {
					continue
				}
				key := string(c.Source.Format) + ":" + c.Path
				if seen[key] {
					continue
				}
				seen[key] = true
				out = append(out, c)
			}
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

func matchesAny(patterns []string, rel string) bool {
	for _, pattern := range patterns {
		if matchGlob(pattern, rel) {
			return true
		}
	}
	return false
}

func sourceExcluded(source Source, rel string) bool {
	base := strings.ToLower(path.Base(rel))
	if strings.HasSuffix(base, ".lock") || strings.HasSuffix(base, ".tmp") || strings.HasPrefix(base, ".") {
		return true
	}
	return matchesAny(source.Exclude, rel)
}

// sourceMatches tests the relative path against the source patterns and
// exclusions. The path is slash-normalized so the same globs work everywhere.
func sourceMatches(source Source, root, p string) bool {
	rel, err := filepath.Rel(root, p)
	if err != nil {
		return false
	}
	rel = filepath.ToSlash(rel)
	if sourceExcluded(source, rel) {
		return false
	}
	return matchesAny(source.Patterns, rel)
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
