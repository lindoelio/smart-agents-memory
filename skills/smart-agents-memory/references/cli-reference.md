# CLI reference

Every command reads the same configuration: the backend, the store, and the
local projections.

## Global flags

| Flag | Meaning |
|---|---|
| `--version` | Print the version and exit. It needs no store and no configuration |
| `--vault <name>` | Backend: `obsidian` (default), `disk`, `sqlite` |
| `--memory-home <path>` | Root of the store, for the file gateways |
| `--config <path>` | Configuration file (default `~/.config/smart-agents-memory/config.json`) |
| `--database <path>` | Database of the SQLite gateway |

## Commands

### `search <query> [--limit N]`

Searches the corpus, the project notes and the hot tier. When inside a Git
repository it also searches team memory; those hits are marked `[team] `.
The query is a regular expression when it compiles, and a case-insensitive
substring match otherwise. Output is `path:line: text`.

### `brief [--project <slug>] [--worktree <id>] [--global] [--query Q] [--limit N]`

Prints the hot tier and the index of the corpus (type, title, description);
the index does not carry cold bodies. It also detects the repository and
linked worktree from the working directory and prints `## Project` and
`## Worktree` sections with the bodies of the scoped entries. Inside a Git
repository it adds `## Team Memory (<repo-slug>)`. `--project` adds the
notes of another project; `--global` disables project/worktree
auto-detection, but the team store is still read. With `--query` it also
runs the search.

### `add --scope <type> --title <title> --body <body>`

Records one durable memory.

| Flag | Meaning |
|---|---|
| `--scope` | `user`, `lesson` (default), `project`, `reference` |
| `--title` | The title, which is the identity of the entry |
| `--body` | The content. The why lives here; an empty body is rejected |
| `--source` | Where the entry came from (default `smart-agents-memory-cli`) |
| `--native` | `minimax-user`, `minimax-agent`, `qwen` — where to project it |
| `--tier` | `hot` or `cold` (default `cold`). `hot` enters every session |
| `--pinned` | Protect the entry from automated edits, imports and consolidation |
| `--project` | Repository or project slug; auto-detected for `--scope project` |
| `--worktree` | Worktree id, or `auto` to use the current linked worktree |
| `--team` | Write to the Git-backed `.agents/memory/` instead of the private vault |
| `--description` | The one line a runtime injects for a cold entry |
| `--reason` | Why the entry was recorded, stored beside the body |
| `--tags` | Keywords, written to the frontmatter |
| `--force` | Write even when the title already exists |
| `--allow-other-language` | Skip the English-default warning |

`--team` requires a Git repository and rejects `--scope user`; team memory
cannot hold a personal profile entry. It writes `.agents/memory/` and, when
the Qwen team directory exists or `--native qwen` is passed, mirrors the
entry into `.qwen/team-memory/`. A detected credential blocks the write.

When the title already exists, the body is appended to the entry instead of
creating a second one. A pinned entry refuses the append unless `--force` is
passed; a forced append keeps the pin and records `forced on pinned` in the
write log. `--pinned=false` does not release an entry: use `unpin`.

### `pin <title>`

Protects an existing entry from automated edits, imports and consolidation.
The corpus keeps the note where it is; the Qwen projection moves to
`native/qwen/pinned/<file>.md`. The write log records
`pin | <type> | <title>`.

### `unpin <title>`

Releases an entry. The Qwen projection moves back to the root and the write
log records `unpin | <type> | <title>`. An entry physically stored under a
`pinned/` directory must be moved out of that folder first.

### `log [--limit N]`

Prints the write log, newest first.

### `prune-worktrees [--apply]`

Lists the memories scoped to a worktree that is no longer registered in the
current repository. Without `--apply` it only reports. With `--apply` it
removes the unpinned, cold entries and their project notes, then reexports
the projections. Pinned and hot entries are reported as protected and are
never removed; a note whose worktree still has a protected entry is kept too.

### `index`

Reconciles the corpus with the native projections and regenerates the index.
Idempotent: a second run reports nothing to reconcile.

### `install`

Links the tools to the store and materializes the skill. Idempotent.

### `doctor`

Checks the links, the projections, the index and the store. Exits non-zero
when something is wrong.

### `path`

Prints the root of the store.

### `migrate [--apply]`

Migrates a legacy store to the current layout and vocabulary. Without
`--apply` it only reports what it would do.

### `version`

Prints the version.

## Environment variables

| Variable | Meaning |
|---|---|
| `SMART_AGENTS_MEMORY_HOME` | Root of the store |
| `SMART_AGENTS_MEMORY_VAULT` | Backend |
| `SMART_AGENTS_MEMORY_DATABASE` | Database of the SQLite gateway |
| `SMART_AGENTS_MEMORY_SOURCE` | Source recorded on new entries |
