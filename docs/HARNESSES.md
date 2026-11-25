# Harnesses

How Smart Agents Memory captures durable knowledge from each coding harness.
The capture is **sync-first**: the product reads the artifacts the harness
already writes. There is no hook, plugin, dispatcher or configuration writer
in the critical path.

Probe date: 2026-11-25. Source code: `internal/sources`.

## Tiers

- **Tier A — automatic.** Context, MCP tools and automatic capture via `sync`.
- **Tier B — assisted.** Context and MCP tools; capture is manual.

## Matrix

| Harness | Context | Capture source | Format | Transcript | Tier |
|---|---|---|---|---|---|
| Claude Code | `~/.claude/CLAUDE.md` | `~/.claude/projects/**/*.jsonl` | `messages-jsonl` | JSONL | A |
| Qwen Code | `~/.qwen/QWEN.md`, memories | `~/.qwen/projects/*/chats/*.jsonl` | `messages-jsonl` | JSONL | A |
| Cursor | `.cursor/rules`, skill | `~/.cursor/projects/*/agent-transcripts/*/*.jsonl` | `messages-jsonl` | JSONL | A |
| Grok Build | `~/.grok/AGENTS.md` | `~/.grok/sessions/*/*/updates.jsonl` (fallback `chat_history.jsonl`) | `acp-jsonl` | ACP | A |
| Antigravity CLI | `~/.gemini/GEMINI.md` | `~/.gemini/antigravity-cli/brain/*/.../transcript_full.jsonl` (fallback `transcript.jsonl`) | `step-jsonl` | JSONL | A |
| Codex | `~/.codex/AGENTS.md` | `~/.codex/sessions/**/rollout-*.jsonl` | `response-item-jsonl` | JSONL | A |
| OpenCode | `~/.config/opencode/AGENTS.md`, skill | `~/.local/share/opencode/opencode.db` | `session-message-sqlite` | SQLite | A |
| MiniMax Code | native memory links | none proven | — | native | B |

## Formats

### `messages-jsonl`

Claude, Qwen and Cursor. Role comes from `role`, `type` or `sender`; text comes
from `content`, `message`, `parts` or `text`. `message` may be an object with
`content` or `parts`.

The adapter drops system/developer/tool turns, tool results and synthetic user
turns. It removes `<timestamp>`, `<user_query>`, `<system-reminder>`,
`<USER_REQUEST>`, `<ADDITIONAL_METADATA>` and `<USER_SETTINGS_CHANGE>`
wrappers. A user message that is the Codex `# AGENTS.md instructions` block is
also dropped.

### `acp-jsonl`

Grok `updates.jsonl`. A line is `method: "session/update"` with
`params.update.sessionUpdate`:

- `user_message_chunk` → user;
- `agent_message_chunk` → assistant;
- `hook_execution`, `turn_completed` and metadata → dropped.

The file is ACP. When a session has no `updates.jsonl`, discovery uses its
`chat_history.jsonl` as a per-session fallback and parses it as
`messages-jsonl`; it never processes both for the same session. The fallback
contains the full system prompt, which the parser drops.

### `step-jsonl`

Antigravity `transcript_full.jsonl`; when it is absent, discovery uses the
session's `transcript.jsonl` instead. Both use the step format:

- `USER_INPUT` → user, with `<USER_REQUEST>` wrappers stripped;
- `PLANNER_RESPONSE` → assistant;
- `GENERIC` (tool output) and the rest → dropped.

### `response-item-jsonl`

Codex `rollout-*.jsonl`:

- `type: "response_item"` with `payload.type: "message"`;
- `payload.role` user/assistant and `payload.content[].text`;
- developer/system/tool/event records and the `AGENTS.md` user block are
  dropped.

### `session-message-sqlite`

OpenCode `opencode.db` read-only:

```sql
SELECT session_id, seq, type, data
FROM session_message
WHERE type IN ('user', 'assistant') AND seq > ?
ORDER BY session_id, seq
```

The `data` column is JSON; `data.text` is the message. The source opens with
`mode=ro` and never writes to the database. A missing schema is a source
error, not a silent skip.

## State and first run

`.sync-state.json` stores a byte offset per file and a sequence per SQLite
session. The first run seeds the state at the end of every file and extracts
nothing. `sync --backfill` is the explicit way to process history. A partial
final line is left for the next run; a shrunken file restarts at zero.

A legacy `.extract-cursor.json` from the old `watch` is read on the first
run. A file it already processed is seeded at its end and counted as
`migrated`; the old history is not sent to the model again. The seed is
tracked per source, so a first run limited by `--source` does not make the
other sources backfill accidentally.

The state also stores a short-lived hash of delivered turns. This keeps two
sources that point at the same session from producing duplicate memories.
`doctor` shows `seed: done|pending` and `backfill: done|not run` per source.

Before the extraction prompt, high-confidence credentials from the turns and
the manifest are replaced with `[redacted]`; the report counts the
replacements but never logs the value.

## Artifacts verified in the probe

| Harness | Evidence |
|---|---|
| Qwen | live Stop payload; transcript parsed |
| Grok | live Stop payload pointing at `updates.jsonl`; ACP parsed |
| Antigravity | live Stop payload pointing at `transcript_full.jsonl`; steps parsed |
| Codex | non-ephemeral `codex exec` wrote `sessions/**/rollout-*.jsonl`; parsed |
| Cursor | hook log payload + real transcript parsed |
| Claude | live Stop payload captured against a local Anthropic-compatible endpoint; parsed |
| OpenCode | `session_message` schema inspected; read-only reader tested |

## What sync does not do

- It does not install hooks, plugins or dispatchers.
- It does not change `settings.json`, `hooks.json` or `config.toml`.
- It does not authenticate to a harness.
- It does not copy transcripts into the corpus; it stores only extracted
  memory entries.
- It does not backfill history without `--backfill`.
- It does not require starting the harness with a particular environment.

## Checks

```bash
smart-agents-memory sync --dry-run
smart-agents-memory sync --backfill --since 7d --dry-run
smart-agents-memory sync --source cursor --dry-run
smart-agents-memory sync --backfill --since 1d --max-bytes 262144
smart-agents-memory doctor
```

`doctor` lists every source as `ok`, `missing` or `disabled`, with the last
sync time and any source error.
