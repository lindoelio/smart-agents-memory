# Security

## What this project touches

Smart Agents Memory writes to two places: the store, and the home directory
of the user.

| Path | What lives there |
|---|---|
| The store | The memory: one note per entry, in Markdown with YAML properties |
| `~/.config/smart-agents-memory/` | The configuration file, and no token |
| `~/.local/share/smart-agents-memory/` | The materialized skill, and the local projections of the gateways that are not a local directory |
| `~/.minimax/`, `~/.qwen/`, `~/.codex/`, `~/.claude/`, `~/.gemini/`, `~/.config/opencode/`, `~/.cursor/` | The symlinks that connect the tools to the store |

The tool never reads anything else, and never writes outside the store and
those directories.

## The store is private

The store is **not** part of this repository, and it must never be committed
to one. It holds whatever the owner decided to remember: preferences,
decisions, project context. The repository ships the tool; the memory stays
with the person.

Before publishing anything that came out of a store, read it. The
`examples/corpus` folder of this repository is the only corpus that belongs
here, and it is synthetic.

## Secrets

- **No secret is ever written to the configuration file.** The token of the
  Notion gateway comes from the environment — `NOTION_TOKEN`, or the variable
  named by `notion_token_env` — and is not persisted.
- **No secret is ever written to the store.** The write log records what
  changed and where it came from, never a credential.
- The `add` command warns when the body of an entry does not look like
  English, but it does not inspect the content for secrets: **do not put a
  credential in a memory entry**.

## The symlinks

The installation replaces files in the home directory of the user with
symlinks. When a target exists and is not a symlink — a tool rewrote it —
the previous content is preserved next to it, with the `.bak-smart-agents-memory`
suffix, before the link is recreated. Nothing is deleted.

The store directories are symlinks of a **directory**, and the global
instruction slots are symlinks of a **file**. The distinction is deliberate:
the MiniMax runtime writes with a temporary file plus a rename, and a rename
over a symlink of a file would replace the link and silently disconnect the
integration.

## The Notion gateway

The Notion gateway talks to the Notion API over HTTPS, with the token in the
`Authorization` header. It reads and writes only the database it is given.
The database id and the token come from the configuration and the
environment; neither is logged.

## Reporting a vulnerability

Open a private security advisory on the repository, or write to the
maintainer. Please include the version, the platform, and the steps to
reproduce. A fix and a release follow the report.
