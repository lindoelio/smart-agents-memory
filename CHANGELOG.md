# Changelog

All notable changes to this project are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and the project uses
[semantic versioning](https://semver.org/spec/v2.0.0.html).

## [0.4.1] — 2026-11-25

### Fixed

- The doctor no longer reports a project note (`projects/<name>.md`) as a
  titleless entry. A project note is addressed by its file name and
  deliberately carries no title; only a real corpus entry without a title is
  a problem.

## [0.4.0] — 2026-11-25

### Added

- **Explicit LLM toggle.** `llm_enabled` in the configuration turns the
  optional OpenAI-compatible client on; it is off by default and `install`
  writes it off. When it is on and no base URL is set, the adapter uses the
  local `smart-llm-gateway`. An API-key variable exported for another tool
  never enables the client by itself.
- **`ExtractOptions.Project`.** A caller can force the project of an
  extracted candidate instead of resolving it from the working directory.

### Changed

- `recall --query` ranks by relevance first and only then boosts the active
  project and worktree; an entry that shares no term with the query is not a
  result. Hot entries stay out of the ranking, and a description that repeats
  the body's first line is printed once.
- `--worktree auto` works with any scope. In the main checkout the entry
  stays repository-wide; in a linked worktree it carries the worktree id.
- The write log is read newest-first within each day.
- The Qwen projection quotes `name`, `description`, `category` and keywords,
  and the corpus quotes a keyword that YAML would read as a map or a comment.
  A plain keyword stays plain.
- `dream --dry-run` no longer writes the state file and reports the same
  counts as a real run. The state records `idle`, `running`, `updated`,
  `noop` and `failed`.
- The English gate now rejects a short non-English body in `dream` and
  `extract`; the lenient check still only warns on the command line.
- The native import adds every imported entry to the dirty counter, so
  `--min-dirty` and the auto-dream threshold agree with the number of
  memories.

### Fixed

- `App.Add` and both store gateways reject an empty title. The doctor reports
  a titleless entry instead of leaving it invisible.
- `add --team` follows the pinned rule: a curated team entry refuses an
  append without `--force`.
- `projection.Store.List` includes `pinned/*.md` for the Qwen target.
- The doctor reports a symlinked team note that the reader skips.

### Removed

- `App.WatchOnce` and its helpers. `watch` remains as a deprecated alias of
  `sync`.

### Security

- The LLM client is opt-in: a command talks to an endpoint only when
  `llm_enabled`, a base URL or a model asks it to. A key exported for another
  tool cannot turn a local command into a network call.

## [0.3.0] — 2026-11-25

### Added

- **Pinned entries.** `add --pinned`, `pin` and `unpin` protect a curated
  memory from automated imports and consolidation. Qwen Code receives the
  entry under its protected `pinned/` directory, and a newer native edit
  cannot overwrite the corpus. An existing SQLite store gains the column on
  open; a missing value reads as false.
- **Repository and worktree scope.** `add --scope project` detects the Git
  repository, `--worktree auto` scopes a note to the linked worktree, and
  `brief` prints the project and worktree sections automatically. The scope
  is stored in `project` and `worktree`; SQLite migrates the columns and the
  project index on open.
- **`prune-worktrees`.** Reports memories scoped to worktrees that no longer
  exist; `--apply` removes the unpinned, cold entries and their notes,
  reexports the projections and never touches pinned or hot entries. A note
  whose worktree still has a protected entry is kept.
- **Team memory.** `add --team` writes to the Git-backed `.agents/memory/`,
  with an optional `.qwen/team-memory/` projection. `brief` adds
  `## Team Memory (<repo-slug>)` and `search` marks team hits with
  `[team] `; both work with only a team store, without a private vault.
  The directory refuses a symlink that escapes the worktree.
- **SecretGuard.** Every private and team write, and every native import,
  scans for high-confidence credentials. A match rejects or skips the write
  and names only the rule, never the matched value.
- **Taxonomy, tree and recall.** Entries gain `category` and
  `usage_scenarios`; `recall --tree`, `--category`, `--ref` and `--query`
  serve the deterministic memory tree, with the active project/worktree
  boosted in ranking.
- **MCP server.** `smart-agents-memory mcp` exposes `memory_tree`,
  `memory_recall`, `memory_search`, `memory_brief` and `memory_add` over
  JSON-RPC stdio.
- **Optional LLM and Dream.** `internal/llm` is a pluggable JSON completion
  port with an OpenAI-compatible adapter. `recall --llm` validates selected
  refs, and `dream` runs deterministic deduplication/backfill before an
  optional fail-closed LLM merge/update pass. Pinned and hot entries are
  never modified; the API key is referenced by environment variable, never
  stored.
- **Coverage gates.** `scripts/coverage.sh` enforces 90% statements, lines and
  functions and 80% branch/decision across the module. CI runs the same gate
  on every push and pull request.
- **Sync capture.** `internal/sources` reads the artifacts the harnesses
  already write (Claude, Qwen, Cursor, Grok, Antigravity, Codex and OpenCode)
  and `sync` extracts only the new turns. The first run seeds the state;
  `--backfill` is explicit. `install --scheduler` writes a launchd or systemd
  user timer for `sync --quiet`. No hook, plugin or harness configuration is
  required.
- **Post-turn extraction.** `internal/extract` parses JSONL, JSON and plain
  text, keeps a per-session compression-safe cursor, and stores only cold,
  unpinned candidates after the SecretGuard and the why check. `extract`
  remains available for a manual transcript; `install --hooks` is an
  experimental manual hook, not the capture path.

### Changed

- `watch` is a deprecated alias of `sync`; `watch --once` runs one sync.
- High-confidence credentials in a transcript or manifest are redacted
  before the extraction prompt; only the replacement count is reported.
- Grok and Antigravity have per-session fallback transcripts
  (`chat_history.jsonl` and `transcript.jsonl`) when the primary file is
  absent. A legacy `.extract-cursor.json` is migrated on the first sync.
- The default capture no longer writes to harness settings, installs plugins,
  or authenticates to a harness.
- `sync_roots`, `sync_disabled`, `sync_max_bytes` and `sync_interval` are new
  configuration keys; `watch_dirs` adds a custom `messages-jsonl` source.

### Fixed

- **The post-turn hook extracts every turn.** A stdin payload without
  `--session-id` is a one-shot: it no longer writes the cursor, so the second
  consecutive payload is not reported as `no_new_user_turns`.
- **A failed write no longer counts as a skip.** `extract` aborts, exits
  non-zero and leaves the cursor untouched; the next run retries the
  candidate, and the corpus still has no duplicate.
- **`watch` ignores what is not a transcript.** A JSON configuration file, a
  JSON array without turns and a Markdown note are no longer sent to the
  model as one user message; a file without a conversation creates no
  cursor.
- **`extract --dry-run` reports `accepted`, not `added`.** The count matches
  what was written: nothing.
- **The installed hook is actionable.** The script carries the absolute path
  of the binary and the snippets are real `{"command":...}` blocks; a README
  documents the wiring the command does not perform.

## [0.2.0] — 2026-11-11

The corpus is the source the projections follow, and a gateway that could
not be verified is no longer part of the product.

### Removed

- **The Notion gateway.** It was tested against a stand-in that accepted a
  call the Notion API does not define. `vault: notion` now fails and names
  this version. Move the corpus to `obsidian`, `disk` or `sqlite` first.
- **The empty MiniMax store link.** `~/.minimax/memories` was created and
  nothing was written there. The format of that store is not known.

### Changed

- **A projection is regenerated.** `index` rewrites every native file from
  the corpus and deletes a file no entry backs. A file whose bytes already
  match is left alone, so the next run stays quiet. An append updates the
  topic.
- **One hot tier.** Entries with `--tier hot` are what every harness reads,
  inside the markers of `MEMORY.md`. The frontmatter stays. A block with no
  headings is imported once as `Profile`.
- **`brief` prints the hot tier and the index**, without the bodies of cold
  entries.
- **SQLite publishes an index, renames, removes and keeps project notes.**
  `install` then `doctor` is clean, and it does not create a fake `corpus/`
  directory.
- **A second install keeps the previous backup.** The new file gets a
  numeric suffix. Nothing the user had is deleted.

### Added

- **`--tier`, `--description` and `--reason`.** `hot` or `cold`, the line a
  runtime injects, and the reason stored beside the body.
- **`doctor` reports a note it could not read.** A note without frontmatter
  is still not an entry.

## [0.1.1] — 2026-11-11

The first release worked, but the first run of a new machine did not.

### Added

- **`--version` answers without a store.** It is asked for before anything
  else: no configuration, no store, no network.

### Changed

- **`install` creates the store.** A machine that just installed the binary
  is wired up in one command: the store is created with its corpus, its
  project notes, its log, its hot tier, the native projections and the
  index. The default is `~/Smart Agents Memory`.
- **The commands that need a store say so.** They name the path they looked
  at, and they point at the command that creates it.

### Fixed

- **The formula of the Homebrew tap had a block for Windows.** The formula
  language of Homebrew knows only macOS and Linux, and the block was a
  syntax error that broke the install. The Windows binary is still published
  in the release.

## [0.1.0] — 2026-11-08

The first release. The project was previously known as `memoria`, a Python
CLI named `memoria` with a skill called `memoria-compartilhada`.

### Added

- **A Go CLI.** One binary, no runtime dependency, cross-compiled for macOS,
  Linux and Windows. The same commands as before: `search`, `brief`, `add`,
  `log`, `index`, `install`, `doctor`, `path`, plus `migrate` and `version`.
- **A pluggable store.** The corpus lives behind a port, with four gateways:
  `obsidian` (the default), `disk`, `sqlite` and `notion`. The backend is
  configuration, not a fork.
- **The Obsidian gateway.** Notes are discovered the way Obsidian discovers
  them, and the index is published as an Obsidian-native note with
  wikilinks.
- **An agent skill, embedded in the binary.** `brew install` plus
  `smart-agents-memory install` is enough — no clone of the repository is
  needed.
- **A `migrate` command.** It brings a store from the previous layout and
  vocabulary: the types become `user`, `lesson`, `project` and `reference`,
  the sources follow the product name, the entries that stayed in the
  previous language are renamed, the duplicates are merged and the orphan
  projections are removed. It reports before it acts, and it is idempotent.
- **Release and Homebrew channels.** A tag publishes the binaries and
  updates the formula of the `homebrew-smart-agents-memory` tap.

### Changed

- **The vocabulary is English.** The entry types, the flags, the messages,
  the documentation and the stored entries. The corpus was already written in
  English; now the product is too.
- **The store is resolved, not assumed.** `--memory-home`, then
  `SMART_AGENTS_MEMORY_HOME`, then the configuration file, then discovery.
  The tool and the store no longer have to live in the same folder.
- **The projections moved next to the corpus.** `native/` sits inside the
  store, the way it always did, and the global instruction slots point at
  the hot tier of the store.

### Removed

- The Portuguese command line flags. `--escopo`, `--titulo`, `--corpo`,
  `--projeto` and `--fonte` were replaced by `--scope`, `--title`,
  `--body`, `--project` and `--source`. An unknown flag now gets a pointer
  to the current one.
- The Python implementation, and the shell launcher that found the store in
  the iCloud vault.

[0.2.0]: https://github.com/lindoelio/smart-agents-memory/releases/tag/v0.2.0
[0.1.1]: https://github.com/lindoelio/smart-agents-memory/releases/tag/v0.1.1
[0.1.0]: https://github.com/lindoelio/smart-agents-memory/releases/tag/v0.1.0
