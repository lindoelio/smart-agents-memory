# Integrations

`smart-agents-memory install` wires the tools to the store. It is idempotent,
and it repairs a link that a tool overwrote.

## Two channels

The memory reaches a tool through two channels, because they solve
different problems.

1. **The skill** — the tool discovers it by its own skill mechanism and
   decides when to load it. The skill teaches the protocol: when to query,
   how to record, what not to record, hot and cold.
2. **The global instruction slot** — a small file that guarantees the tool
   knows the memory exists and how to reach it.

Neither carries heavy content.

## What each tool receives

| Tool | Link | Receives |
|---|---|---|
| MiniMax Code | `~/.minimax/memory/` | The profile, injected in every session |
| MiniMax Code | `~/.minimax/agents/main/memory/` | The lessons, as topic files |
| MiniMax Code | `~/.minimax/memories/` | The new runtime store |
| Qwen Code | `~/.qwen/memories/` | Its topics |
| OpenCode | `~/.config/opencode/AGENTS.md` | The hot tier |
| Codex | `~/.codex/AGENTS.md` | The hot tier |
| Claude Code | `~/.claude/CLAUDE.md` | The hot tier |
| Gemini CLI | `~/.gemini/GEMINI.md` | The hot tier |
| Qwen Code | `~/.qwen/QWEN.md` | The hot tier |
| Cursor | `~/.cursor/rules/smart-agents-memory.mdc` | The hot tier |

The skill is linked into the skill directory of each tool:
`~/.agents/skills`, `~/.config/opencode/skills`, `~/.minimax/skills`,
`~/.qwen/skills`, `~/.cursor/skills-cursor`, `~/.claude/skills`,
`~/.codex/skills`, `~/.gemini/skills`.

## Directory symlink, file symlink

A memory directory is a symlink of a **directory**, never of a file. The
MiniMax runtime writes with a temporary file plus a rename, and a rename over
a symlink of a file replaces the symlink itself — the integration would be
silently disconnected.

A global instruction slot is a symlink of a **file**: those tools only read.

## The Cursor rule

Cursor only applies a rule file automatically when the frontmatter carries
`alwaysApply: true`. The hot tier has that key for this reason.

## Context budget

The memory is not dumped in the context. The runtime injects the minimum:
the profile, and only the name and the description of each topic. The body of
a lesson is read when the tool decides it needs it, through the topic reader
or through `smart-agents-memory search`.

## Another machine

The store is a directory: copy it, or point a new machine at it. The skill
is embedded in the binary, so a machine only needs:

```bash
smart-agents-memory install
```
