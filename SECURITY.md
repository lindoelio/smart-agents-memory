# Security

## What this project touches

Smart Agents Memory writes to the store and to a few places in the home
directory of the user.

| Path | What lives there |
|---|---|
| The store | The memory: one Markdown note per entry, or a row in a SQLite database |
| `<repo>/.agents/memory/`, `<repo>/.qwen/team-memory/` | Team memory, committed to the repository |
| `~/.config/smart-agents-memory/` | The configuration file, and no token |
| `~/.local/share/smart-agents-memory/` | The materialized skill; for SQLite also the hot tier, the projection state and the local projections |
| `~/.minimax/`, `~/.qwen/`, `~/.codex/`, `~/.claude/`, `~/.gemini/`, `~/.config/opencode/`, `~/.cursor/`, `~/.agents/` | The symlinks that connect the tools to the store |

On the machine, the tool reads the store and the paths above. When no store
was named for a file gateway, it also looks in the iCloud folders where a
store may live:
`~/Library/Mobile Documents/iCloud~md~obsidian/Documents/*/smart-agents-memory`
and `~/Library/Mobile Documents/com~apple~CloudDocs/*/smart-agents-memory`.
It never writes outside the store and the paths above.

## The store is private

The store is **not** part of this repository, and it must never be committed
to one. It holds whatever the owner decided to remember: preferences,
decisions, project context. The repository ships the tool; the memory stays
with the person.

Team memory is the deliberate exception: `.agents/memory/` lives inside the
repository and is meant to be committed and shared.

Before publishing anything that came out of a store, read it. The
`examples/corpus` folder of this repository is the only corpus that belongs
here, and it is synthetic.

## Secrets

- **The store gateways do not talk to the network.** The product's memory
  gateways make no HTTP request. The optional LLM client does, and only to
  the configured OpenAI-compatible base URL.
- **No secret is ever written to the configuration file.** The file holds
  the backend, the paths and the **name** of the LLM key variable, never the
  key itself.
- **No secret is ever written to the store.** The write log records what
  changed and where it came from, never a credential.
- **SecretGuard blocks high-confidence credentials.** `add`, the native
  import and every team write scan the title and the body for the gitleaks
  patterns. A match rejects the write; the error names the rule, never the
  matched value. The MCP `memory_add` goes through the same path. The scan
  is a backstop, not a guarantee: do not put a credential in a memory entry.
- **Team memory is committed to the repository.** Treat a team note like
  any other file you commit. `.agents/memory/` and `.qwen/team-memory/`
  reject a symlink that could redirect a write outside the worktree.
- **Extraction cannot escalate a memory.** `extract` and `watch` force cold,
  unpinned candidates, run the SecretGuard, and refuse a pinned or hot title.
  `watch` parses strictly, so a configuration file is never sent to the
  model as a user turn. A failed write aborts the run before the cursor
  moves, so the candidate is retried instead of lost. The post-turn hook
  always exits zero, so a failing model never blocks the coding harness.

## The symlinks

The installation replaces files in the home directory of the user with
symlinks. When a target exists and is not a symlink — a tool rewrote it —
the previous content is preserved next to it, with the `.bak-smart-agents-memory`
suffix, before the link is recreated. The content of a file that a tool
wrote is never deleted: an earlier backup is kept, and the new one gets a
numeric suffix (`-2`, `-3`, …).

The store directories are symlinks of a **directory**, and the global
instruction slots are symlinks of a **file**. The distinction is deliberate:
the MiniMax runtime writes with a temporary file plus a rename, and a rename
over a symlink of a file would replace the link and silently disconnect the
integration.

## Reporting a vulnerability

Open a private security advisory on the repository, or write to the
maintainer. Please include the version, the platform, and the steps to
reproduce. A fix and a release follow the report.
