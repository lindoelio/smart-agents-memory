// Package memory holds the domain model of Smart Agents Memory: what a
// memory is, how it is typed, tiered, compared, and addressed.
//
// The domain knows nothing about where entries live. That belongs to the
// vault gateway, behind the vault.Vault port.
package memory

import (
	"strings"
	"time"
)

// Type is the scope of an entry: what the memory is about.
type Type string

const (
	// TypeUser is a fact about the person and how to work with them.
	TypeUser Type = "user"
	// TypeLesson is a durable technical lesson.
	TypeLesson Type = "lesson"
	// TypeProject is a decision or context about a project.
	TypeProject Type = "project"
	// TypeReference is a pointer to something outside the memory.
	TypeReference Type = "reference"
)

// Types lists every entry type, in the order the index groups them.
var Types = []Type{TypeUser, TypeLesson, TypeProject, TypeReference}

// Valid reports whether t is a known entry type.
func (t Type) Valid() bool {
	for _, known := range Types {
		if t == known {
			return true
		}
	}
	return false
}

// NativeTarget returns the native projection a new entry of this type targets.
func (t Type) NativeTarget() string {
	if t == TypeUser {
		return "minimax-user"
	}
	return "minimax-agent"
}

// NativeType returns the native_type value written in the frontmatter.
func (t Type) NativeType() string {
	if t == TypeUser {
		return "user"
	}
	return "reference"
}

// Tier decides whether an entry is injected into every session.
type Tier string

const (
	// TierHot enters every session. Reserved for what is always true.
	TierHot Tier = "hot"
	// TierCold is the default: fetched on demand.
	TierCold Tier = "cold"
)

// Entry is one memory: a title, a body, and the metadata that decides where
// it is projected into the native format of each tool.
type Entry struct {
	Title       string
	Type        Type
	Date        string
	Body        string
	Tier        Tier
	Source      string
	Native      string
	NativeType  string
	NativeFile  string
	Reason      string
	Description string
	Keywords    []string
	// Extra keeps frontmatter keys this build does not know about, so a
	// round-trip never silently drops what the user typed.
	Extra map[string]string
	// ModTime is when the entry was last changed. The reconciliation rule
	// uses it to decide which side wins when the corpus and a native store
	// disagree. Every vault adapter supplies it.
	ModTime time.Time
	// Path is the entry's address inside the vault. It is opaque: a file
	// path on disk, a row id in SQLite, a page id in Notion.
	Path string
}

// EffectiveTier returns the tier, defaulting to cold. An entry without a
// tier is cold: only what is explicitly hot enters every session.
func (e Entry) EffectiveTier() Tier {
	if e.Tier == TierHot {
		return TierHot
	}
	return TierCold
}

// NormTitle normalizes a title for comparison: lowercase, collapsed spaces,
// no quotes, no code fences. The date suffix is kept on purpose: in the
// user store a title may legitimately end with "(2026-11-07)".
func NormTitle(title string) string {
	t := strings.TrimSpace(title)
	t = strings.Trim(t, "\"'")
	t = strings.ReplaceAll(t, "`", "")
	return strings.ToLower(strings.Join(strings.Fields(t), " "))
}

// NormBody normalizes a body for comparison: whitespace collapsed.
func NormBody(body string) string {
	return strings.Join(strings.Fields(body), " ")
}
