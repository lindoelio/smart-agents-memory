# Corpus format

The corpus is the single source of truth. One memory is one note: YAML
frontmatter, then the title as a heading, then the body.

```markdown
---
title: "A durable technical lesson"
type: lesson
date: 2026-11-07
source: "smart-agents-memory-cli"
native: minimax-agent
tier: cold
native_type: reference
---

## A durable technical lesson

The rule. Why it matters. What it cost.
```

## Fields

| Field | Meaning |
|---|---|
| `title` | The identity of the entry. Two entries never share one. |
| `type` | `user`, `lesson`, `project`, `reference` |
| `date` | The day the entry was recorded |
| `source` | Where it came from: the CLI, or the native store of a tool |
| `native` | The projection: `minimax-user`, `minimax-agent`, `qwen` |
| `tier` | `hot` (in every session) or `cold` (on demand) |
| `pinned` | `true` protects the entry from automated edits, imports and consolidation |
| `project` | The repository or project slug |
| `worktree` | The linked git worktree identifier |
| `category` | Semantic category from the memory tree |
| `usage_scenarios` | Situations in which the entry applies |
| `native_type` | The type of the entry in the native format |
| `native_file` | The file name of the native projection |
| `reason` | Why the entry was appended to the user profile |
| `description` | The one-line summary the runtime injects |
| `keywords` | A list of keywords |

Unknown keys are preserved: a round-trip never drops what was written by
hand.

## Types

| Type | About |
|---|---|
| `user` | The person, and how to work with them |
| `lesson` | A durable technical lesson |
| `project` | A decision, or the context of a project |
| `reference` | A pointer to something outside the memory |

## Tiers

`hot` enters every session: the profile and the permanent rules. There are
few of them. `cold` is the default and is fetched on demand — in MiniMax
Code, as a topic file where only the name and the description enter the
context.

## Pinned entries

`pinned: true` marks a curated entry that automated imports, consolidation
and extraction must never overwrite, rename or delete. It does not move the
corpus note: the property lives in the same `corpus/<type>/<slug>.md`.

The Qwen projection materializes the protection as
`native/qwen/pinned/<file>.md`, the top-level directory Qwen Code treats as
read-only for its automatic workers. A note placed by hand under a `pinned/`
folder is read as pinned even without the property.

## Project and worktree scope

`project` is the repository slug. `worktree` is the linked worktree id and
is present only for branch-local notes. The project note lives at
`projects/<project>.md`; the worktree note at
`projects/<project>@<worktree>.md`.

A project decision is repository-wide by default. Use the worktree only when
the fact is specific to that branch or checkout.

## Team memory

Inside a Git repository, `add --team` writes to `.agents/memory/`:

```
.agents/memory/
  MEMORY.md
  corpus/<type>/<slug>.md
```

It is committed and shared through Git. When `.qwen/team-memory/` exists or
the write asks for the Qwen projection, the same entry is mirrored there in
the Qwen topic format. A credential detected by the SecretGuard blocks the
write.

## The native projections

The corpus is read by every tool, in the format each one already expects.

| Target | Format |
|---|---|
| `minimax-user` | `### Title` plus the body, in the injected `user.md` |
| `minimax-agent` | `### Title (YYYY-MM-DD)` plus a `Type:` line, in the injected `MEMORY.md` |
| `minimax-agent` (cold) | A topic file: only the description enters the context |
| `qwen` | Frontmatter with `name`, `description`, `type`, `category` |
| `qwen`, pinned | The same topic, under `pinned/<file>.md` |

`category` is one of the twenty semantic categories, or `uncategorized`.
`usage_scenarios` is a YAML list. `recall --tree` groups by category;
`recall --category` returns a subtree; `recall --query` ranks by title,
keywords, category, scenarios, description and body.

A projection is never merged: it is regenerated from the corpus.
