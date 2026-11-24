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
| `pinned` | `true` protects the entry from automated edits, imports and consolidation |
| `project` | The repository or project slug the entry belongs to |
| `worktree` | The linked git worktree identifier, when scoped to one worktree |
| `category` | The semantic category used by the memory tree |
| `usage_scenarios` | A list of situations in which the entry applies |
| `native_type` | The type of the entry in the native format |
| `native_file` | The name of the file of the native projection |
| `reason` | Why the entry was appended to the user profile |
| `description` | The one line the runtime injects |
| `keywords` | A list of keywords |

A property the product does not know is **preserved**. A round trip never
drops what was written by hand.

## The taxonomy

`category` is one of the twenty semantic categories used by the memory tree,
or `uncategorized`. `usage_scenarios` lists the situations in which the entry
applies. When the category is absent, `InferCategory` derives a deterministic
default from the type, title, description and keywords.

`recall --tree` renders the categories with their refs; `recall --category`
returns one subtree; `recall --query` ranks by title, keywords, category,
scenarios, description and body, with the active project and worktree boosted.

## Project and worktree scope

`project` is the repository slug. A project decision is repository-wide
unless it also carries `worktree`, which names a linked Git worktree.

The notes of a project live beside the corpus:

```
projects/<project>.md
projects/<project>@<worktree>.md
```

`brief` reads the notes and the scoped entries of the active repository and
worktree. The note is optional: the entries alone are enough.

## Team memory

Inside a Git repository, `add --team` writes to `.agents/memory/`:

```
.agents/memory/
  MEMORY.md                  the generated index
  corpus/<type>/<slug>.md    one note per entry, the standard format
```

The directory is committed and shared through the normal Git workflow. If
`.gitignore` excludes `.agents` or `.qwen`, re-include `.agents/memory/` and
`.qwen/team-memory/`, or the team store stays silently empty in Git. If
`.qwen/team-memory/` exists, or the write requests the Qwen projection, the
entry is mirrored there in the Qwen topic format. Both mirrors reject a
symlink that could lead outside the worktree.

A write containing a high-confidence credential is rejected by the
SecretGuard; the error names the rule, never the matched value.

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
| `qwen`, pinned | The same topic, under `pinned/<file>.md` instead of the root |

The `pinned` property never moves the corpus note: it stays in
`corpus/<type>/<slug>.md`. The Qwen projection materializes the protection as
the top-level `pinned/` directory that Qwen Code treats as read-only for its
automatic extraction and Dream workers. The index links the entry as
`pinned/<file>.md`.

A note placed by hand under a `pinned/` directory is read as pinned even when
the YAML property is absent. The path can add protection; it never removes it.

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
- 18:25 | pin | lesson | A durable lesson
- 18:26 | unpin | lesson | A durable lesson
- 18:27 | prune-worktrees | entries=1 notes=1
```

An append forced onto a pinned entry records `(append, forced on pinned)`.

The log is a record of what happened, not a place to store content. It is
never rewritten.
