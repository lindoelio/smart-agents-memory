# Integrations

## Two channels

The memory reaches a tool through two channels, because they solve different
problems.

1. **The skill.** Each tool discovers it by its own skill mechanism, and
   decides when to load it. The skill teaches the protocol: when to query,
   how to record, what not to record, hot and cold.
2. **The global instruction slot.** A small file that guarantees the tool
   knows the memory exists, and how to reach it.

Neither carries heavy content.

## The links

`smart-agents-memory install` creates seventeen symlinks.

### The stores — three links, to directories

| Tool | Link | Reads |
|---|---|---|
| MiniMax Code | `~/.minimax/memory/` | The hot tier, injected in every session |
| MiniMax Code | `~/.minimax/agents/main/memory/` | Cold entries as topic files, and hot agent entries in `MEMORY.md` |
| Qwen Code | `~/.qwen/memories/` | Its topics |

### The instruction slots — six links, to files

| Tool | Link | Reads |
|---|---|---|
| OpenCode | `~/.config/opencode/AGENTS.md` | The hot tier |
| Codex | `~/.codex/AGENTS.md` | The hot tier |
| Claude Code | `~/.claude/CLAUDE.md` | The hot tier |
| Gemini CLI | `~/.gemini/GEMINI.md` | The hot tier |
| Qwen Code | `~/.qwen/QWEN.md` | The hot tier |
| Cursor | `~/.cursor/rules/smart-agents-memory.mdc` | The hot tier |

### The skill — eight links, to directories

`~/.agents/skills`, `~/.config/opencode/skills`, `~/.minimax/skills`,
`~/.qwen/skills`, `~/.cursor/skills-cursor`, `~/.claude/skills`,
`~/.codex/skills`, `~/.gemini/skills` — each one with a
`smart-agents-memory` inside.

`~/.agents/skills` is the universal layout: it is read by MiniMax Code,
Qwen Code and Cursor without any configuration.

## Why a directory, and why a file

A memory directory is a symlink of a **directory**, never of a file. The
MiniMax runtime writes with a temporary file plus a rename, and a rename
over a symlink of a file replaces the symlink itself: the integration would
be silently disconnected, and the tool would keep writing to a file nobody
reads. A symlink of a directory is crossed by the write, and the memory ends
up in the store.

A global instruction slot is a symlink of a **file**, because those tools
only read it.

## The Cursor rule

Cursor only applies a rule file automatically when the frontmatter carries
`alwaysApply: true`. The hot tier has that key for this reason.

## The context budget

The memory is not dumped into the context. In MiniMax Code:

| Block | What it is |
|---|---|
| The user profile | The `hot` entries, inside the markers |
| The topic list | Only the name and the description of each `cold` entry |
| The agent memory | The `hot` entries whose projection is `minimax-agent`, and nothing when there are none |

The body of a lesson is read when the tool decides it needs it, through the
topic reader of the runtime or through `smart-agents-memory search`. That is
the difference between a context that grows with the memory and one that
does not.

## Another machine

The store is a folder: copy it, or point a new machine at it. The skill is
embedded in the binary, so a machine only needs:

```bash
smart-agents-memory install
```

With the Obsidian gateway, the store comes with the vault that iCloud
syncs, and the installation finds it on its own.

## Checking the installation

```bash
smart-agents-memory doctor
```

It reports the links that are broken or point at the wrong place, the skill
that is not installed, the projection without a counterpart in the corpus,
and the index that is stale. It exits non-zero when something is wrong.
