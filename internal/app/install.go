package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/lindoelio/smart-agents-memory/internal/config"
	"github.com/lindoelio/smart-agents-memory/internal/link"
	"github.com/lindoelio/smart-agents-memory/internal/native"
	"github.com/lindoelio/smart-agents-memory/internal/skill"
)

// LinkTarget is one symlink the install command creates.
type LinkTarget struct {
	// Label names the tool the link belongs to.
	Label string
	// Path is where the link is created.
	Path string
	// Source is what it points to.
	Source string
	// Dir tells whether the link points to a directory.
	Dir bool
}

// SkillRoot is where the skill is materialized.
func SkillRoot() string {
	return filepath.Join(config.ShareDir(), "skills", skill.Name)
}

// LinkTargets returns every symlink of the installation.
//
// The memory directories are symlinks of a directory: the MiniMax runtime
// writes with a temporary file plus a rename, which replaces a symlink of a
// file and would break the integration. The global instruction slots are
// symlinks of a file, because those tools only read.
func (a *App) LinkTargets() []LinkTarget {
	home, err := os.UserHomeDir()
	if err != nil {
		home = os.Getenv("HOME")
	}
	nativeRoot := filepath.Join(a.LocalRoot, "native")
	hot := a.HotPath()
	skillRoot := SkillRoot()

	targets := []LinkTarget{
		{"MiniMax Code (user)", filepath.Join(home, ".minimax", "memory"), filepath.Join(nativeRoot, string(native.TargetMiniMaxUser)), true},
		{"MiniMax Code (agent)", filepath.Join(home, ".minimax", "agents", "main", "memory"), filepath.Join(nativeRoot, string(native.TargetMiniMaxAgent)), true},
		{"MiniMax Code (store)", filepath.Join(home, ".minimax", "memories"), filepath.Join(nativeRoot, string(native.TargetMiniMaxStore)), true},
		{"Qwen Code (memories)", filepath.Join(home, ".qwen", "memories"), filepath.Join(nativeRoot, string(native.TargetQwen)), true},

		{"OpenCode", filepath.Join(home, ".config", "opencode", "AGENTS.md"), hot, false},
		{"Codex", filepath.Join(home, ".codex", "AGENTS.md"), hot, false},
		{"Claude Code", filepath.Join(home, ".claude", "CLAUDE.md"), hot, false},
		{"Gemini CLI", filepath.Join(home, ".gemini", "GEMINI.md"), hot, false},
		{"Qwen Code", filepath.Join(home, ".qwen", "QWEN.md"), hot, false},
		{"Cursor", filepath.Join(home, ".cursor", "rules", "smart-agents-memory.mdc"), hot, false},
	}

	for _, dir := range skillDirs(home) {
		targets = append(targets, LinkTarget{
			Label:  "skill: " + filepath.Base(dir),
			Path:   filepath.Join(dir, skill.Name),
			Source: skillRoot,
			Dir:    true,
		})
	}
	return targets
}

// skillDirs returns the skill directories of every tool.
func skillDirs(home string) []string {
	return []string{
		filepath.Join(home, ".agents", "skills"),
		filepath.Join(home, ".config", "opencode", "skills"),
		filepath.Join(home, ".minimax", "skills"),
		filepath.Join(home, ".qwen", "skills"),
		filepath.Join(home, ".cursor", "skills-cursor"),
		filepath.Join(home, ".claude", "skills"),
		filepath.Join(home, ".codex", "skills"),
		filepath.Join(home, ".gemini", "skills"),
	}
}

// InstallResult is the outcome of the install command.
type InstallResult struct {
	// Lines describe what was done.
	Lines []string
	// SkillRoot is where the skill was materialized.
	SkillRoot string
}

// Install links the tools to the store and materializes the skill.
//
// It is idempotent: running it again changes nothing.
func (a *App) Install(ctx context.Context) (InstallResult, error) {
	result := InstallResult{}
	if err := a.Projections.EnsureDirs(); err != nil {
		return result, err
	}

	root, err := skill.Materialize(config.ShareDir())
	if err != nil {
		return result, err
	}
	result.SkillRoot = root
	result.Lines = append(result.Lines, "skill: "+root)

	for _, target := range a.LinkTargets() {
		msg, err := link.Link(target.Path, target.Source, target.Dir)
		if err != nil {
			return result, fmt.Errorf("%s: %w", target.Label, err)
		}
		result.Lines = append(result.Lines, target.Label+": "+msg)
	}

	// The configuration is written so that the next run does not need
	// discovery. An existing file is never overwritten.
	if a.ConfigPath != "" {
		if _, err := os.Stat(a.ConfigPath); os.IsNotExist(err) {
			file := config.File{
				Vault:          a.Cfg.Backend,
				MemoryHome:     a.Cfg.MemoryHome,
				SQLitePath:     a.Cfg.SQLitePath,
				NotionDatabase: a.Cfg.NotionDatabase,
				ProjectionRoot: a.Cfg.ProjectionRoot,
			}
			if err := config.Save(a.ConfigPath, file); err == nil {
				result.Lines = append(result.Lines, "config: "+a.ConfigPath)
			}
		}
	}

	sort.Strings(result.Lines[1:])
	return result, nil
}
