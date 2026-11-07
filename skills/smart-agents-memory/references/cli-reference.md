# CLI reference

Every command reads the same configuration: the backend, the store, and the
local projections.

## Global flags

| Flag | Meaning |
|---|---|
| `--vault <name>` | Backend: `obsidian` (default), `disk`, `sqlite`, `notion` |
| `--memory-home <path>` | Root of the store, for the file gateways |
| `--config <path>` | Configuration file (default `~/.config/smart-agents-memory/config.json`) |
| `--database <path>` | Database of the SQLite gateway |
| `--notion-token <token>` | Token of the Notion gateway |
| `--notion-database <id>` | Database id of the Notion gateway |

## Commands

### `search <query> [--limit N]`

Searches the corpus, the project notes and the hot tier. The query is a
regular expression when it compiles, and a case-insensitive substring match
otherwise. Output is `path:line: text`.

### `brief [--project <slug>] [--query Q] [--limit N]`

Prints the hot tier — the profile and the permanent rules — followed by the
notes of a project when `--project` is given. With `--query` it also runs the
search.

### `add --scope <type> --title <title> --body <body>`

Records one durable memory.

| Flag | Meaning |
|---|---|
| `--scope` | `user`, `lesson` (default), `project`, `reference` |
| `--title` | The title, which is the identity of the entry |
| `--body` | The content, always with the why |
| `--source` | Where the entry came from (default `smart-agents-memory-cli`) |
| `--native` | `minimax-user`, `minimax-agent`, `qwen` — where to project it |
| `--tags` | Keywords, written to the frontmatter |
| `--force` | Write even when the title already exists |
| `--allow-other-language` | Skip the English-default warning |

When the title already exists, the body is appended to the entry instead of
creating a second one.

### `log [--limit N]`

Prints the write log, newest first.

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
| `NOTION_TOKEN` | Token of the Notion gateway |
