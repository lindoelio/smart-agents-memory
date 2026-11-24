---
name: smart-agents-memory
description: Use this skill whenever you need to know the user's preferences, past decisions, project context, or durable technical lessons — before choosing an approach, before advising on how to work, and before reporting status. Provides the `smart-agents-memory` CLI for searching the shared corpus and recording new durable knowledge. Also use it when the user states a lasting preference, a correction, a decision, or when you learn a non-obvious lesson that would otherwise be rediscovered. Not for reading project code, git history, or files.
---

# Shared memory

The user works with **several AIs at the same time** — OpenCode, MiniMax Code,
Codex, Cursor, Qwen Code, Claude Code, Gemini CLI. So that they do not
disagree with each other, the memory lives in **one place**: a single corpus,
reached through the `smart-agents-memory` CLI.

**The memory is not in your context.** It is fetched on demand. This skill
explains how to fetch it and how to record into it.

## Language of stored memory

**Write memory entries in English, including the text inside them.** That is
the default for the whole corpus: it keeps the entries native for the models
and it gives the user English reading practice.

Two exceptions, and only two:

- the user explicitly asks for another language;
- the entry quotes something that has to stay verbatim in another language
  (a command, an identifier, a message they pasted).

Replies to the user stay in the user's own language. This rule is about the
stored memory, not about the conversation.

## When to query

Before deciding on an approach, before recommending something, and before
reporting status, it is worth checking:

| Moment | Query |
|---|---|
| "how does the user prefer I do X?" | `smart-agents-memory search "<topic>"` |
| "have we tried this? is there a known trap?" | `smart-agents-memory search "<topic>"` |
| starting work on a project | `smart-agents-memory brief` (auto-detects the repo) |
| before giving an opinion on scope/deadline | `smart-agents-memory brief` |
| after learning something durable | `smart-agents-memory add` (see below) |

Query **before** asserting anything about the user's preferences or history.
Memory orients, it does not prove: if it cites a file, flag, endpoint or
command, **verify it still exists** before acting.

## How to query

```bash
smart-agents-memory search "neon"           # search the whole corpus
smart-agents-memory brief                   # profile + index + active repo/worktree
smart-agents-memory brief --global          # ignore the working directory
smart-agents-memory brief --project <slug>  # another project
smart-agents-memory recall --tree           # the category router
smart-agents-memory recall --query "flaky test"
smart-agents-memory recall --category testing_standard
smart-agents-memory recall --ref "lesson:a-flaky-test"
smart-agents-memory search "flutter" --limit 5
smart-agents-memory path                    # where the store lives
```

Output comes from the **corpus**, the single source of truth:

- `user/` — the person and how to work with them
- `lesson/` — durable technical lessons (what already cost time)
- `project/` — decisions and project context
- `reference/` — pointers to docs, dashboards, runbooks

## How to record

Durable knowledge (still true after this session) goes into the memory.
Always use the CLI — it keeps the format, projects into the native format of
your tool and records it in the write log:

```bash
smart-agents-memory add --scope lesson --title "Short title" --body "Rule... Why..."
smart-agents-memory add --scope project --title "..." --body "..."
smart-agents-memory add --scope lesson --team --title "Shared rule" --body "..."
smart-agents-memory add --scope user --title "..." --body "..." --tier hot
```

Scopes: `user` (a fact about the person), `project` (decision/context),
`lesson` (durable technical lesson), `reference` (external pointer).

Inside a Git repository, `--scope project` resolves the repository slug.
Use `--worktree auto` only when the fact belongs to the current branch or
worktree; otherwise the decision is repository-wide.

**Golden rule: always the why.** The why is the body. An empty body is
rejected. The tool does not judge the prose.

Write the body in English.

### What NOT to record

- Things recoverable from the repo (paths, signatures, architecture)
- Git facts (diff, blame, commit, who changed what)
- Temporary state (current branch, work in progress)
- Activity logs, PR lists, work journals
- Blame and guesses about people

### Tier: hot and cold

Every entry has a `tier` in its frontmatter:

- **`hot`** — enters **every** session. Only for what is always true: the
  user's profile and permanent rules. There are few; do not overuse it.
- **`cold`** — the default. Stays in the corpus and is fetched on demand.
  Technical lessons, project context, references.

If a new entry is universally true (a permanent working rule), pass
`--tier hot`. Otherwise leave it `cold`, which is the default. `hot` is
written into the block every harness reads.

### Pinned: user-curated entries

Some entries are curated by the person and must survive automatic cleanup.
Record one with `--pinned`, or protect an existing entry:

```bash
smart-agents-memory pin "Short title"
smart-agents-memory unpin "Short title"
```

A pinned entry is never overwritten, renamed or deleted by imports or
consolidation. In Qwen Code it is projected under `pinned/`, the protected
directory its background workers cannot modify. Do not pin automatically
generated memories; pin only what the person explicitly curated.

### Structured recall

Every entry has a semantic `category` and optional `usage_scenarios`. When
recording, set `--category` and `--scenario` when the defaults are ambiguous.
Use `recall --tree` to see the map, `--category` for a focused subtree,
`--query` for ranked context and `--ref` for an exact body. The MCP server
exposes the same operations as `memory_tree`, `memory_recall`,
`memory_search`, `memory_brief` and `memory_add`.

### Project and worktree scope

`brief` detects the repository and the linked worktree from the working
directory. Use `--global` to ignore them and `--project <slug>` for another
repository.

A project decision is repository-wide. A note that only applies to the
current branch needs `--worktree auto`. When a worktree is removed, `doctor`
reports the leftovers and `prune-worktrees --apply` removes the unpinned,
cold ones. A pinned or hot entry, and the note of its worktree, are kept.

### Team memory

`--team` writes a durable, project-wide fact to `.agents/memory/` inside the
repository. It is committed and shared with every collaborator, so use it for
conventions and shared references — not for personal or fast-decaying notes.
`--scope user` is rejected. A detected credential blocks the write.

If `.qwen/team-memory/` exists, the entry is mirrored there for Qwen Code.
`brief` shows the team store under `## Team Memory (<repo-slug>)`, and
`search` marks its hits with `[team] `.

## After writing

`smart-agents-memory add` writes to the corpus and projects into your tool's
native format. If you wrote directly in your tool's native store (the
MiniMax `memory` tool, for example), you do not need to do anything:
`smart-agents-memory index` imports the change into the corpus.

## When something is wrong

```bash
smart-agents-memory doctor    # integrity: links, projections, index
smart-agents-memory log       # what was recorded, and by whom
smart-agents-memory index     # reconciles corpus <-> native and regenerates the index
```

**Stale memory is a lead, not a fact.** If the code contradicts it, fix the
memory — do not trust the old entry.

See `references/cli-reference.md` for the full command surface,
`references/corpus-format.md` for the entry format, and
`references/integrations.md` for what each tool receives.
