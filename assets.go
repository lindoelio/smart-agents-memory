// Package smartagentsmemory is the root of the Smart Agents Memory module.
//
// It embeds the assets that ship inside the binary, so that a single
// installed executable carries everything a machine needs.
package smartagentsmemory

import "embed"

// SkillFS holds the agent skill: the SKILL.md and its references.
//
//go:embed all:skills
var SkillFS embed.FS
