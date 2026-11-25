# Transcript fixtures

Synthetic transcripts that keep the shape of the real files produced by each
harness. They contain no real user text, identifiers or paths.

| Directory | Harness | Expected after normalization |
|---|---|---|
| `claude/` | Claude Code | user turn, assistant turn; `tool_result` dropped |
| `qwen/` | Qwen Code | user turn, assistant turn; `system` dropped |
| `cursor/` | Cursor | user turn with `<user_query>` stripped, assistant turn |
| `grok/` | Grok Build | `session.jsonl`: user + assistant, `system` and `synthetic_reason` dropped; `updates.jsonl`: `user_message_chunk` + `agent_message_chunk` only |
| `antigravity/` | Antigravity CLI | `USER_INPUT` → user with `<USER_REQUEST>` stripped, `PLANNER_RESPONSE` → assistant, `GENERIC` dropped |
| `codex/` | Codex | `response_item` message with role user/assistant; developer messages and the `# AGENTS.md instructions` block dropped |

The payload fixtures live in `internal/ingest/testdata/`.
