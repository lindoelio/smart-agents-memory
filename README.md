# Smart Agents Memory

[![M8ven Score](https://m8ven.ai/badge/mcp/lindoelio-smart-agents-memory-ssdwt7?v=e414dafe65df633b5442e5a9d3c7158a)](https://m8ven.ai/mcp/lindoelio-smart-agents-memory-ssdwt7?s=readme)

Shared memory for AI agents: **one corpus, many harnesses**.

OpenCode, MiniMax Code, Codex, Cursor, Qwen Code, Claude Code and the Gemini
CLI all read the same memory, in the format each one already expects. The
memory is written once, through a CLI, and reconciled in both directions — so
no tool ever holds a private version of the truth.

```
┌──────────────────────────────────────────────────────────────┐
│  the store — one corpus, the single source of truth          │
│  user/ · lesson/ · project/ · reference/                     │
└───────────────┬──────────────────────────────────────────────┘
                │  smart-agents-memory index
                ▼
┌──────────────────────────────────────────────────────────────┐
│  the projections — the same memory, in each tool's format    │
│  MiniMax Code · Qwen Code · …                               │
└──────────────────────────────────────────────────────────────┘
```

## The tool and the store are two things

The **tool** is this repository: a CLI, an agent skill, and the wiring that
connects your tools to your memory.

The **store** is your memory: a folder of notes. By default it is an
Obsidian vault, synced by iCloud, so you can read and edit it by hand — and
it never travels to a public repository.

| | |
|---|---|
| Tool | [github.com/lindoelio/smart-agents-memory](https://github.com/lindoelio/smart-agents-memory) — MIT |
| Store | Your folder. Obsidian by default, and it stays private. |

## Install

### Homebrew

```bash
brew install lindoelio/smart-agents-memory/smart-agents-memory
```

### GitHub Releases

Download the binary for your platform from the
[releases](https://github.com/lindoelio/smart-agents-memory/releases), and
put it on your `PATH`.

### From source

```bash
git clone https://github.com/lindoelio/smart-agents-memory.git
cd smart-agents-memory
go build -o smart-agents-memory ./cmd/smart-agents-memory
```

### Wire it up

```bash
smart-agents-memory install
```

That links your tools to the store and materializes the skill. It is
idempotent, and it repairs a link that a tool overwrote.

It also creates the store when there is none — at `~/Smart Agents Memory` by
default — so a machine that just installed the binary is ready in one
command. Point it somewhere else with `--memory-home`, or with the
`memory_home` key of the configuration file:

```bash
smart-agents-memory --memory-home ~/Documents/MyVault/Smart\ Agents\ Memory install
```

The store is a folder of Markdown notes. Put it inside your Obsidian vault
if you want to read and edit the memory by hand, or point it at a SQLite
database if you prefer a single file.

Inside a Git repository, `add --team` writes to `.agents/memory/`, committed
and shared with the team. `brief` shows it under
`## Team Memory (<repo-slug>)`, and `search` marks its hits with `[team] `.
If `.gitignore` excludes `.agents`, re-include `.agents/memory/`.

## Use it

```bash
smart-agents-memory search "neon"           # find something
smart-agents-memory brief                   # the hot tier, the active repo/worktree
smart-agents-memory brief --global          # ignore project/worktree detection
smart-agents-memory brief --project my-app  # + the notes of a project
smart-agents-memory add --scope lesson \
  --title "Short title" \
  --body "Rule... Why..."                   # record something durable
smart-agents-memory add --scope project \
  --title "Project decision" --body "..."   # scoped to the current repo
smart-agents-memory add --scope project --worktree auto \
  --title "Branch decision" --body "..."    # scoped to the current worktree
smart-agents-memory add --scope lesson --worktree auto \
  --title "Branch lesson" --body "..."      # any scope can be worktree-local
smart-agents-memory add --scope lesson --team \
  --title "Shared rule" --body "..."        # committed in .agents/memory/
smart-agents-memory recall --tree           # the category router
smart-agents-memory recall --query "flaky test"
smart-agents-memory recall --ref "lesson:a-flaky-test"
smart-agents-memory dream --deterministic-only
smart-agents-memory extract --transcript turn.jsonl  # one finished transcript
smart-agents-memory watch --once             # deprecated alias of sync
smart-agents-memory sync --dry-run           # see what would be captured
smart-agents-memory sync                     # capture new turns from the harnesses
smart-agents-memory install --scheduler      # run sync every five minutes
smart-agents-memory mcp                     # JSON-RPC tools over stdio
smart-agents-memory pin "Short title"       # protect a curated entry
smart-agents-memory unpin "Short title"     # release it again
smart-agents-memory prune-worktrees         # report scopes of deleted worktrees
smart-agents-memory log                     # what was recorded, and by whom
smart-agents-memory index                   # reconcile corpus <-> projections
smart-agents-memory doctor                  # check the installation
smart-agents-memory path                    # where the store is
```

The first `sync` seeds the state at the end of the transcripts already on
disk and extracts nothing; run `sync --backfill --since 7d` to process recent
history explicitly. `extract` processes one finished transcript on demand;
`watch` remains as a deprecated alias of `sync`. `install --scheduler` keeps
the corpus current every five minutes without a daemon or a hook in any
harness.

The full surface is in
[the CLI reference](skills/smart-agents-memory/references/cli-reference.md).

### Optional LLM

`recall --llm` and the semantic pass of `dream` use an OpenAI-compatible
endpoint. The client is off until `llm_enabled` is true (or a base URL/model
is configured); when it is on and no URL is set, the local
`smart-llm-gateway` is the default, and OpenAI is used when the key
environment variable exists. A key exported for another tool does not enable
the client by itself. The configuration stores the **name** of the key
variable, never the key:

```json
{
  "llm_provider": "openai",
  "llm_enabled": true,
  "llm_base_url": "http://127.0.0.1:8080/v1",
  "llm_model": "gpt-4o-mini",
  "llm_key_env": "SMART_AGENTS_MEMORY_LLM_API_KEY"
}
```

`watch_dirs` (or `sync_roots` for a specific source) is an optional list of
extra transcript directories for `sync`. The first `sync` seeds the state and
does not extract history; `sync --backfill` is the explicit opt-in for that.
`install --scheduler` writes a launchd or systemd user timer that runs
`sync --quiet` every five minutes. `install --hooks` remains as an
experimental manual hook and is not required.

## Where the memory lives

The store is a **gateway**, and you choose it:

| Backend | What it is |
|---|---|
| `obsidian` | **The default.** Notes in an Obsidian vault, with properties, tags and wikilinks. The index is an Obsidian-native note. |
| `disk` | A plain directory of Markdown notes, for people who do not use Obsidian. |
| `sqlite` | One database file, for a memory that travels with the machine. |

```bash
smart-agents-memory --vault sqlite search "neon"
```

The choice is configuration, not a fork:

```json
{
  "vault": "obsidian",
  "memory_home": "~/Documents/MyVault/Smart Agents Memory"
}
```

## What each tool receives

| Tool | What it gets |
|---|---|
| MiniMax Code | The profile, injected in every session, and the lessons as topic files |
| Qwen Code | Its topics, and the hot tier |
| OpenCode, Codex, Claude Code, Gemini CLI | The hot tier, through the global instruction slot |
| Cursor | The hot tier, through a user rule |

The memory is **not** dumped into the context. The runtime injects the
profile and only the name and the description of each topic; the body of a
lesson is read when the tool decides it needs it.

Two channels do this work: the **skill**, which each tool discovers by its
own mechanism, and the **global instruction slot**, which guarantees the
minimum. See [the integrations reference](skills/smart-agents-memory/references/integrations.md).

## The rules of the memory

- **The corpus is the single source of truth.** The projections are
  regenerated, never merged. When the two disagree, the most recent change
  wins.
- **Always the why.** An entry without a reason is not reusable — nobody will
  know when it applies.
- **English by default.** The stored memory is written in English, including
  the text inside each entry: it keeps the notes native for the models, and
  it is the reading practice of the person who owns the store. The replies
  to that person stay in their own language.
- **Hot and cold.** `hot` enters every session — the profile and the
  permanent rules, and only those. `cold` is fetched on demand.
- **Pinned.** A pinned entry is curated by the person: imports,
  consolidation and extraction never overwrite it. In Qwen Code it is
  projected under the protected `pinned/` directory.
- **Repository and worktree.** A project decision belongs to the repository;
  a note specific to a branch lives in the linked worktree only when
  `--worktree auto` is used.
- **Team memory.** `--team` writes to the committed `.agents/memory/`; the
  team sees it through Git. A high-confidence credential blocks the write.
- **A tree, not a dump.** Entries carry a category and usage scenarios.
  `recall --tree` is the compact router; `--query`, `--category` and `--ref`
  fetch only what the task needs.
- **Capture is incremental.** `sync` reads only the transcript files that
  changed, seeds the first run and extracts new user turns from the formats
  the harnesses already write. `--backfill` is the explicit opt-in for old
  history. Candidates are stored as cold, unpinned memories after the
  SecretGuard and the why check; a failed write leaves the cursor untouched.

## Architecture

Ports and adapters, on both sides:

- The **entrypoints** — the CLI today, an MCP server next — are adapters
  over the same use cases.
- The **gateways** — the store — are adapters behind a port. The domain
  never learns which one is in use.

```
cmd/smart-agents-memory   entrypoint
        │
internal/app              use cases: search, brief, add, index, install…
        │
internal/vault            the port ──┬── obsidian   (default)
        │                             ├── disk
        │                             └── sqlite
internal/memory           the domain: entries, types, tiers, slugs
internal/native           the projection formats of each tool
```

The gateway test that matters is the round trip: the same corpus travels
through every backend and comes back unchanged.

See [the architecture document](ARCHITECTURE.md).

## Development

```bash
go test ./...
go vet ./...
```

The suite covers the domain, every gateway, the round trip between them, the
use cases and the command line. It needs no token and no network.

## Documentation

| Document | What is in it |
|---|---|
| [ARCHITECTURE.md](ARCHITECTURE.md) | The model, the ports, the reconciliation rule |
| [MEMORY_FORMAT.md](docs/MEMORY_FORMAT.md) | The format of an entry |
| [INTEGRATIONS.md](docs/INTEGRATIONS.md) | What each tool receives, and why |
| [DISTRIBUTION.md](docs/DISTRIBUTION.md) | The release channels, and how to publish one |
| [MIGRATION.md](docs/MIGRATION.md) | How to move a store from another layout |

## License

MIT. See [LICENSE](LICENSE).
