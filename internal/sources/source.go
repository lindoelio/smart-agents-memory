// Package sources discovers the transcript artifacts that coding harnesses
// write and normalizes them into the turns used by extraction.
//
// The package knows formats, not products: a source is a descriptor, and a
// format is a pure parser. Adding a harness that already uses a known format
// is a registry entry, not a new integration.
package sources

import (
	"path/filepath"
	"strings"
)

// Kind is how a source is read.
type Kind string

const (
	// KindFiles walks a directory tree and matches relative patterns.
	KindFiles Kind = "files"
	// KindSQLite reads a session database read-only.
	KindSQLite Kind = "sqlite"
)

// Format is a transcript shape, not a harness.
type Format string

const (
	FormatMessagesJSONL        Format = "messages-jsonl"
	FormatACPJSONL             Format = "acp-jsonl"
	FormatStepJSONL            Format = "step-jsonl"
	FormatResponseItemJSONL    Format = "response-item-jsonl"
	FormatSessionMessageSQLite Format = "session-message-sqlite"
)

// Source describes where one kind of transcript lives.
type Source struct {
	// ID is the label used by config, --source and doctor. It never selects
	// a code path; Format does.
	ID string
	// Kind is files or sqlite.
	Kind Kind
	// Format is the parser to use.
	Format Format
	// Roots are directories. "~" is expanded.
	Roots []string
	// Patterns are slash-separated globs relative to a root. They support
	// "**" for any number of directories.
	Patterns []string
	// Exclude are slash-separated globs that remove a match.
	Exclude []string
	// MaxBytes caps one read from one file. Zero means the default.
	MaxBytes int64
	// Enabled is false when the source is disabled by configuration.
	Enabled bool
}

// Registry is the ordered set of sources.
type Registry struct {
	Sources []Source
}

// DefaultMaxBytes is the read cap of one file in one sync run.
const DefaultMaxBytes int64 = 2 * 1024 * 1024

// Defaults returns the built-in source registry.
func Defaults() Registry {
	return Registry{Sources: []Source{
		{
			ID: "claude", Kind: KindFiles, Format: FormatMessagesJSONL,
			Roots:    []string{"~/.claude/projects"},
			Patterns: []string{"**/*.jsonl"},
			Exclude:  []string{"**/subagents/**", "**/compaction/**"},
			Enabled:  true,
		},
		{
			ID: "qwen", Kind: KindFiles, Format: FormatMessagesJSONL,
			Roots:    []string{"~/.qwen/projects"},
			Patterns: []string{"*/chats/*.jsonl"},
			Enabled:  true,
		},
		{
			ID: "cursor", Kind: KindFiles, Format: FormatMessagesJSONL,
			Roots:    []string{"~/.cursor/projects"},
			Patterns: []string{"*/agent-transcripts/*/*.jsonl"},
			Exclude:  []string{"**/subagents/**", "**/compaction/**"},
			Enabled:  true,
		},
		{
			ID: "grok", Kind: KindFiles, Format: FormatACPJSONL,
			Roots:    []string{"~/.grok/sessions"},
			Patterns: []string{"*/*/updates.jsonl"},
			Enabled:  true,
		},
		{
			ID: "antigravity", Kind: KindFiles, Format: FormatStepJSONL,
			Roots:    []string{"~/.gemini/antigravity-cli/brain"},
			Patterns: []string{"*/.system_generated/logs/transcript_full.jsonl"},
			Enabled:  true,
		},
		{
			ID: "codex", Kind: KindFiles, Format: FormatResponseItemJSONL,
			Roots:    []string{"~/.codex/sessions"},
			Patterns: []string{"**/rollout-*.jsonl"},
			Enabled:  true,
		},
		{
			ID: "opencode", Kind: KindSQLite, Format: FormatSessionMessageSQLite,
			Roots:   []string{"~/.local/share/opencode/opencode.db"},
			Enabled: true,
		},
	}}
}

// Build applies configuration overrides to the defaults.
//
// roots replaces the roots of a source by ID. disabled hides sources.
// extraDirs become one additional messages-jsonl source named "custom".
func Build(roots map[string][]string, disabled []string, maxBytes int64, extraDirs []string) Registry {
	reg := Defaults()
	off := map[string]bool{}
	for _, id := range disabled {
		off[strings.TrimSpace(id)] = true
	}
	for i := range reg.Sources {
		s := &reg.Sources[i]
		if len(roots[s.ID]) > 0 {
			s.Roots = append([]string(nil), roots[s.ID]...)
		}
		if off[s.ID] {
			s.Enabled = false
		}
		if maxBytes > 0 {
			s.MaxBytes = maxBytes
		} else if s.MaxBytes == 0 {
			s.MaxBytes = DefaultMaxBytes
		}
	}
	if len(extraDirs) > 0 {
		reg.Sources = append(reg.Sources, Source{
			ID: "custom", Kind: KindFiles, Format: FormatMessagesJSONL,
			Roots:    append([]string(nil), extraDirs...),
			Patterns: []string{"**/*.jsonl"},
			MaxBytes: maxBytes, Enabled: true,
		})
	}
	return reg
}

// Enabled returns the enabled sources.
func (r Registry) Enabled() []Source {
	var out []Source
	for _, s := range r.Sources {
		if s.Enabled {
			out = append(out, s)
		}
	}
	return out
}

// ByID finds a source by its label.
func (r Registry) ByID(id string) (Source, bool) {
	for _, s := range r.Sources {
		if s.ID == id {
			return s, true
		}
	}
	return Source{}, false
}

// ExpandPath expands a leading ~ in a path.
func ExpandPath(path string) string {
	if path == "~" {
		return homeDir()
	}
	if strings.HasPrefix(path, "~/") {
		return filepath.Join(homeDir(), path[2:])
	}
	return path
}
