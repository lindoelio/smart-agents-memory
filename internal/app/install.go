package app

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/lindoelio/smart-agents-memory/internal/config"
	"github.com/lindoelio/smart-agents-memory/internal/link"
	"github.com/lindoelio/smart-agents-memory/internal/native"
	"github.com/lindoelio/smart-agents-memory/internal/projection"
	"github.com/lindoelio/smart-agents-memory/internal/skill"
	"github.com/lindoelio/smart-agents-memory/internal/vault"
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

// ensureStore creates the store when it is not there.
//
// The corpus layout belongs to the gateway. The hot tier file is local,
// because every tool reads it from disk, whichever gateway holds the corpus.
func (a *App) ensureStore(ctx context.Context) error {
	if a.LocalRoot == "" {
		a.LocalRoot = config.DefaultStorePath()
		a.Projections = projection.New(a.LocalRoot)
	}
	if layout, ok := a.Vault.(vault.Layout); ok {
		if err := layout.Ensure(ctx); err != nil {
			return err
		}
	}
	if err := os.MkdirAll(a.LocalRoot, 0o755); err != nil {
		return err
	}
	if _, err := os.Stat(a.HotPath()); os.IsNotExist(err) {
		hot := "---\n" +
			"description: Shared memory — the profile and the permanent rules. Read before working.\n" +
			"type: index\n" +
			"alwaysApply: true\n" +
			"---\n\n" + native.HotProfileStart + "\n" + native.HotProfileEnd + "\n"
		if err := os.WriteFile(a.HotPath(), []byte(hot), 0o644); err != nil {
			return err
		}
	}
	return nil
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
	// Store is where the memory lives.
	Store string
	// Lines describe what was done.
	Lines []string
	// SkillRoot is where the skill was materialized.
	SkillRoot string
}

// HooksDir is where the post-turn hook and its README live.
func HooksDir() string {
	return filepath.Join(config.ShareDir(), "hooks")
}

// InstallHooks materializes the post-turn hook script and returns the lines
// that describe the harness wiring. The script always exits zero so a missing
// LLM endpoint cannot block a coding session.
//
// The command carries the absolute path of the running binary: the PATH of a
// harness process is not the PATH of the shell that installed the memory.
func (a *App) InstallHooks() (string, []string, error) {
	dir := HooksDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", nil, err
	}
	bin := hookBinary()
	path := filepath.Join(dir, "post-turn.sh")
	script := hookScript(bin)
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		return "", nil, err
	}
	snippet := hookSnippet(path)
	readme := hookReadme(path, bin, snippet)
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte(readme), 0o644); err != nil {
		return "", nil, err
	}
	lines := []string{
		"hook: " + path,
		"binary: " + bin,
		"OpenCode: " + snippet,
		"Qwen Code: " + snippet,
		"Claude Code: " + snippet,
	}
	return path, lines, nil
}

// hookBinary is the absolute path of the running executable. A test binary or
// a development build is still the right command for the machine that ran
// install.
func hookBinary() string {
	bin, err := os.Executable()
	if err != nil || strings.TrimSpace(bin) == "" {
		return "smart-agents-memory"
	}
	if absolute, err := filepath.Abs(bin); err == nil {
		return absolute
	}
	return bin
}

// hookScript renders the shell script. Every failure is swallowed on purpose:
// a post-turn hook must never block the harness.
func hookScript(bin string) string {
	return "#!/bin/sh\n" +
		"# Generated by smart-agents-memory install --hooks.\n" +
		"#\n" +
		"# The harness pipes the finished turn to standard input. The hook always\n" +
		"# exits zero, so a missing model endpoint or a malformed payload never\n" +
		"# blocks the coding session.\n" +
		"bin=" + shellQuote(bin) + "\n" +
		"if [ -n \"${SMART_AGENTS_MEMORY_SESSION_ID:-}\" ]; then\n" +
		"  \"$bin\" extract --stdin --auto-dream --quiet --session-id \"$SMART_AGENTS_MEMORY_SESSION_ID\" || true\n" +
		"else\n" +
		"  \"$bin\" extract --stdin --auto-dream --quiet || true\n" +
		"fi\n"
}

// hookSnippet is the command block a harness expects, with the path quoted.
func hookSnippet(path string) string {
	encoded, err := json.Marshal(path)
	if err != nil {
		encoded = []byte(`"` + path + `"`)
	}
	return `{"command":` + string(encoded) + `}`
}

// hookReadme documents the generated script and the wiring it does not do.
func hookReadme(scriptPath, bin, snippet string) string {
	return "# Post-turn hook\n\n" +
		"Generated by `smart-agents-memory install --hooks`.\n\n" +
		"## The script\n\n" +
		"`post-turn.sh` reads one finished turn from standard input and runs:\n\n" +
		"    " + bin + " extract --stdin --auto-dream --quiet\n\n" +
		"The command is an absolute path, so the hook does not depend on the\n" +
		"PATH of the harness process. The script always exits zero: a missing\n" +
		"model endpoint or a malformed payload never blocks a coding session.\n\n" +
		"## Wiring\n\n" +
		"Point the post-turn hook of the harness at " + scriptPath + ".\n" +
		"The same command snippet works for OpenCode, Qwen Code and Claude\n" +
		"Code:\n\n" +
		"```json\n" + snippet + "\n```\n\n" +
		"This command does not edit the configuration of a harness it does not\n" +
		"know; copy the snippet into the hook configuration yourself.\n\n" +
		"## Sessions\n\n" +
		"Without `SMART_AGENTS_MEMORY_SESSION_ID`, every payload is one complete\n" +
		"transcript and the extraction cursor is not used, so two consecutive\n" +
		"turns are both extracted. Export the variable from the harness when it\n" +
		"knows the session identifier: extraction then resumes where the\n" +
		"previous call stopped.\n"
}

// shellQuote wraps a value in single quotes for POSIX sh.
func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
}

// Install links the tools to the store and materializes the skill.
//
// It is idempotent: running it again changes nothing. It also creates the
// store when there is none, so a machine that just installed the binary is
// wired up in one command.
func (a *App) Install(ctx context.Context) (InstallResult, error) {
	result := InstallResult{}
	if err := a.ensureStore(ctx); err != nil {
		return result, err
	}
	result.Store = a.Vault.Root()
	if err := a.Projections.EnsureDirs(); err != nil {
		return result, err
	}
	// The projections and the index are materialized right away: a store
	// that was just created is already whole, and the doctor has nothing
	// to complain about.
	if err := a.ensureSkeleton(); err != nil {
		return result, err
	}
	if _, err := a.Index(ctx); err != nil {
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
				ProjectionRoot: a.Cfg.ProjectionRoot,
				LLMProvider:    a.Cfg.LLMProvider,
				LLMEnabled:     a.Cfg.LLMEnabled,
				LLMBaseURL:     a.Cfg.LLMBaseURL,
				LLMModel:       a.Cfg.LLMModel,
				LLMKeyEnv:      a.Cfg.LLMKeyEnv,
				WatchDirs:      a.Cfg.WatchDirs,
				SyncRoots:      a.Cfg.SyncRoots,
				SyncDisabled:   a.Cfg.SyncDisabled,
				SyncMaxBytes:   a.Cfg.SyncMaxBytes,
				SyncInterval:   a.Cfg.SyncInterval,
			}
			if err := config.Save(a.ConfigPath, file); err == nil {
				result.Lines = append(result.Lines, "config: "+a.ConfigPath)
			}
		}
	}

	sort.Strings(result.Lines[1:])
	return result, nil
}
