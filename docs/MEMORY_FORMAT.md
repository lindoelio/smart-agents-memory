# The format of the memory

## One entry, one note

An entry is a Markdown note with YAML properties at the top.

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

The rule, and what it costs. The why is what makes it reusable.
```

## The properties

| Property | Meaning |
|---|---|
| `title` | The identity of the entry. Two entries never share one. |
| `type` | `user`, `lesson`, `project`, `reference` |
| `date` | The day the entry was recorded |
| `source` | Where it came from: the CLI, or the native store of a tool |
| `native` | The projection: `minimax-user`, `minimax-agent`, `qwen` |
| `tier` | `hot` or `cold` |
| `native_type` | The type of the entry in the native format |
| `native_file` | The name of the file of the native projection |
| `reason` | Why the entry was appended to the user profile |
| `description` | The one line the runtime injects |
| `keywords` | A list of keywords |

A property the product does not know is **preserved**. A round trip never
drops what was written by hand.

## The types

| Type | About | Default projection |
|---|---|---|
| `user` | The person, and how to work with them | `minimax-user` |
| `lesson` | A durable technical lesson | `minimax-agent` |
| `project` | A decision, or the context of a project | `minimax-agent` |
| `reference` | A pointer to something outside the memory | `minimax-agent` |

## The tiers

`hot` enters every session. There are few of them: the profile, and the
permanent rules. Everything else is `cold`, and is fetched on demand.

In MiniMax Code, a `hot` entry is inside the injected store, and a `cold`
entry is a topic file: only its name and its description enter the context.

## The address of a note

The file name is the slug of the title: lowercase, with the accents kept,
and dashes between the words. It is only an address — the full title lives
in the property — so a long title is shortened with a short hash, and never
truncated in the middle.

Two entries with the same title never share a file: the second one gets a
counter.

## The projection formats

| Target | Shape |
|---|---|
| `MEMORY.md` | The hot tier. Every `hot` entry, between the markers. The frontmatter is kept |
| `minimax-user` | The same hot block, inside the injected `user.md` |
| `minimax-agent`, hot | `### Title (YYYY-MM-DD)`, a `Type:` line, then the body |
| `minimax-agent`, cold | A topic file: a YAML `description`, a marker with the title, then the body |
| `qwen` | Properties `name`, `description`, `type`, `category`, then the body |

A topic description is a YAML scalar. A space stays a space.

The first `index` after 0.2.0 imports a hot block that has no `###` headings
once, as a `user` entry titled `Profile`, and then regenerates the block
from the hot entries.

The name of a topic file must match `^[a-z0-9][a-z0-9-]{0,63}$`: the runtime
of MiniMax Code only recognizes a topic it can name. That is why the topic
name is ASCII, while the name of a corpus note keeps the accents.

## The write log

Every write is recorded, append-only, in `log/YYYY-MM-DD.md`:

```
- 18:24 | add | lesson | A durable lesson | smart-agents-memory-cli
```

The log is a record of what happened, not a place to store content. It is
never rewritten.
