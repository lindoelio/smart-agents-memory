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

## The native projections

The corpus is read by every tool, in the format each one already expects.

| Target | Format |
|---|---|
| `minimax-user` | `### Title` plus the body, in the injected `user.md` |
| `minimax-agent` | `### Title (YYYY-MM-DD)` plus a `Type:` line, in the injected `MEMORY.md` |
| `minimax-agent` (cold) | A topic file: only the description enters the context |
| `qwen` | Frontmatter with `name`, `description`, `type`, `category` |

A projection is never merged: it is regenerated from the corpus.
