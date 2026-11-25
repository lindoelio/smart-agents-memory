# Ingestion payload fixtures

Captured from real hooks during the Phase 0 probe (2026-11-25), then
redacted: session identifiers, paths, email and user text are replaced. The
key names, nesting and event values are preserved.

| File | Harness | Provenance |
|---|---|---|
| `stop-qwen.json` | Qwen Code | live `qwen -p` Stop hook |
| `stop-grok.json` | Grok Build | live `grok -p` Stop hook |
| `stop-antigravity.json` | Antigravity CLI | live `agy -p` Stop hook |
| `stop-cursor.json` | Cursor | Cursor 3.30.14 hook log, Stop event; fresh turn blocked on auth |
| `stop-claude.json` | Claude Code | live `claude -p` against a local Anthropic-compatible endpoint; no real credentials |
| `notify-codex.json` | Codex | live non-ephemeral `codex exec` turn via `notify`; Computer Use notify untouched |

`stop-grok.json` points at `updates.jsonl`, the ACP stream. Its
`chat_history.jsonl` alternative is covered by the transcript fixtures.

Codex has no `transcript_path`; its `notify` payload carries
`input-messages` and `last-assistant-message` inline. A non-ephemeral turn also
writes `~/.codex/sessions/**/rollout-<thread-id>.jsonl`, which is the
recommended capture source: `watch` can read it without touching the Computer
Use `notify` command.
