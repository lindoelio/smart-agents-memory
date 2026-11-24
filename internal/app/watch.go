package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// WatchOptions selects one transcript scan.
type WatchOptions struct {
	// Dirs are extra transcript directories.
	Dirs []string
	// AutoDream runs Dream when the dirty threshold is reached.
	AutoDream bool
}

// WatchReport is the outcome of one scan.
type WatchReport struct {
	Files     int
	Ignored   int
	Extracted int
	Added     int
	Errors    []string
}

// WatchOnce scans the configured transcript directories and runs Extract on
// every new file. Individual file failures are reported, not fatal.
func (a *App) WatchOnce(ctx context.Context, opts WatchOptions) (WatchReport, error) {
	report := WatchReport{}
	reported := map[string]string{}
	for _, file := range a.watchFiles(opts.Dirs) {
		if strings.TrimSpace(file) == "" {
			continue
		}
		content, err := os.ReadFile(file)
		if err != nil {
			report.Errors = append(report.Errors, fmt.Sprintf("%s: %v", file, err))
			continue
		}
		report.Files++
		result, err := a.Extract(ctx, ExtractOptions{
			Transcript: string(content), TranscriptPath: file, SessionID: file,
			Strict: true, AllowPlainText: allowsPlainText(filepath.Ext(file)),
			AutoDream: opts.AutoDream,
		})
		if err != nil {
			if reported[file] == "" {
				report.Errors = append(report.Errors, fmt.Sprintf("%s: %v", file, err))
				reported[file] = err.Error()
			}
			continue
		}
		if result.SkippedReason == "not_a_transcript" {
			report.Ignored++
			continue
		}
		report.Extracted += result.Candidates
		report.Added += result.Added
	}
	return report, nil
}

// allowsPlainText reports whether a file extension is textual by definition.
// A JSON file is never a plain transcript: the strict parser would otherwise
// send a configuration file to the model as one user message.
func allowsPlainText(ext string) bool {
	switch strings.ToLower(ext) {
	case ".txt", ".log":
		return true
	default:
		return false
	}
}

func (a *App) watchFiles(extra []string) []string {
	home, err := os.UserHomeDir()
	if err != nil {
		home = os.Getenv("HOME")
	}
	dirs := defaultWatchDirs(home)
	dirs = append(dirs, a.Cfg.WatchDirs...)
	dirs = append(dirs, extra...)
	seen := map[string]bool{}
	var files []string
	for _, dir := range dirs {
		dir = expandPath(dir)
		if seen[dir] {
			continue
		}
		seen[dir] = true
		info, err := os.Stat(dir)
		if err != nil || !info.IsDir() {
			continue
		}
		_ = filepath.WalkDir(dir, func(path string, entry os.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if entry.IsDir() {
				return nil
			}
			switch strings.ToLower(filepath.Ext(path)) {
			case ".jsonl", ".json", ".log", ".txt":
				files = append(files, path)
			}
			return nil
		})
	}
	return files
}

func defaultWatchDirs(home string) []string {
	dirs := []string{
		filepath.Join(home, ".local", "share", "opencode"),
		filepath.Join(home, ".claude", "projects"),
	}
	if matches, err := filepath.Glob(filepath.Join(home, ".qwen", "projects", "*", "chats")); err == nil {
		dirs = append(dirs, matches...)
	}
	return dirs
}

func expandPath(path string) string {
	if path == "~" {
		if home, err := os.UserHomeDir(); err == nil {
			return home
		}
	}
	if strings.HasPrefix(path, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, path[2:])
		}
	}
	return path
}
