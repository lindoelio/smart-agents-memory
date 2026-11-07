// Package link manages the symlinks that wire the AI tools to the memory.
//
// A store directory is a symlink of a *directory*: the MiniMax runtime
// writes with a temporary file plus a rename, which replaces a symlink of a
// *file* and would silently break the integration.
//
// A global instruction slot is a symlink of a *file*: those tools only read.
package link

import (
	"fmt"
	"os"
	"path/filepath"
)

// BackupSuffix is appended to a file that was overwritten by a tool, before
// the symlink is recreated.
const BackupSuffix = ".bak-smart-agents-memory"

// Link creates a symlink, idempotently.
//
// When the target exists and is not a symlink, the tool rewrote it: the old
// content is preserved next to it and the symlink is recreated, so the
// integration is never silently disconnected.
func Link(target, source string, dir bool) (string, error) {
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return "", err
	}
	if info, err := os.Lstat(target); err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			if dest, err := os.Readlink(target); err == nil && dest == source {
				return "already linked: " + target, nil
			}
			if err := os.Remove(target); err != nil {
				return "", err
			}
		} else {
			backup := target + BackupSuffix
			if _, err := os.Stat(backup); err == nil {
				if err := os.Remove(backup); err != nil {
					return "", err
				}
			}
			if err := os.Rename(target, backup); err != nil {
				return "", fmt.Errorf("cannot move %s aside: %w", target, err)
			}
			if err := os.Symlink(source, target); err != nil {
				return "", err
			}
			return "linked: " + target + " -> " + source + " (previous file saved as " + filepath.Base(backup) + ")", nil
		}
	}
	if err := os.Symlink(source, target); err != nil {
		return "", err
	}
	return "linked: " + target + " -> " + source, nil
}

// Check reports whether a target is a symlink pointing at the expected
// source.
func Check(target, source string) (ok bool, reason string) {
	info, err := os.Lstat(target)
	if err != nil {
		return false, "missing: " + target
	}
	if info.Mode()&os.ModeSymlink == 0 {
		return false, "not a symlink (a tool may have overwritten it): " + target
	}
	dest, err := os.Readlink(target)
	if err != nil {
		return false, "unreadable symlink: " + target
	}
	if _, err := os.Stat(dest); err != nil {
		return false, "points to a missing destination: " + target + " -> " + dest
	}
	if source != "" && dest != source {
		return false, "points to the wrong place: " + target + " -> " + dest + " (expected " + source + ")"
	}
	return true, ""
}
