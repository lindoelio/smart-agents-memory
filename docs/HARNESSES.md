# Harnesses

The contracts this product implements for each coding harness, and the
evidence behind them. This document is the output of the Phase 0 probe: it
records what each harness sends, where it keeps its transcript, and how the
product reaches it. Implementation status is tracked separately.

Probe date: 2026-11-25. Machine: macOS, user `lindoelio`.

## Tiers

- **Tier A — automatic.** Context, MCP tools and automatic capture.
- **Tier B — assisted.** Context and MCP tools; capture is manual or depends
  on a platform adapter that is not implemented yet.
- **Pending.** The harness is installed but the capture contract is not
  proven yet.

## Matrix

| Harness | Context | MCP | Capture today | Transcript | Tier |
|---|---|---|---|---|---|
| Claude Code | `~/.claude/CLAUDE.md` | manual | live Stop captured against a local endpoint | JSONL | A (after wiring) |
| Qwen Code | `~/.qwen/QWEN.md`, `~/.qwen/memories/` | manual | Stop payload captured live | JSONL | A (after wiring) |
| Cursor | `~/.cursor/rules/smart-agents-memory.mdc`, skill | manual | IDE payload in log; `cursor-agent` installed, live blocked on login | `agent-transcripts` JSONL | A (after wiring) |
| Grok Build | `~/.grok/AGENTS.md` (to add) | manual | Stop payload captured live | `updates.jsonl` (ACP) | A (after wiring) |
| Antigravity CLI (`agy`) | `~/.gemini/GEMINI.md` | manual | Stop payload captured live | `transcript_full.jsonl` | A (after wiring) |
| Codex | `~/.codex/AGENTS.md` | manual | non-ephemeral turn proved the rollout; watch root planned | `sessions/**/rollout-*.jsonl` | A (after watch root) |
| OpenCode | `~/.config/opencode/AGENTS.md`, skill | manual | plugin API confirmed | SQLite `session_message` / plugin | B (plugin planned) |
| MiniMax Code | native memory links | unknown | no hook | native projections | B |

"Manual" MCP means `smart-agents-memory install` does not write the harness
configuration yet; the server is available, the wiring is part of Phase 4.

## Live probe results

Each probe installed a temporary hook that wrote stdin to `/tmp`, ran one
short prompt, and restored the original configuration byte-for-byte.

### Qwen Code

Command: `qwen -p "Reply with the single word ok."`

```json
{
  "session_id": "11111111-1111-4111-8111-111111111111",
  "transcript_path": "/Users/example/.qwen/projects/-private-tmp-sam-phase0/chats/11111111-1111-4111-8111-111111111111.jsonl",
  "cwd": "/private/tmp/sam-phase0",
  "hook_event_name": "Stop",
  "timestamp": "2026-11-25T09:22:00.265Z",
  "permission_mode": "auto",
  "prompt_id": "11111111-1111-4111-8111-111111111111########0",
  "stop_hook_active": false,
  "last_assistant_message": "ok",
  "background_tasks": [],
  "crons": [],
  "context_usage": 0.018,
  "context_limit": 1048576,
  "input_tokens": 19117
}
```

The payload has **metadata, not the conversation**. The hook must read
`transcript_path`; piping the payload to `extract --stdin` sends only the
JSON. The configuration was restored byte-for-byte after the probe.

### Grok Build

Command: `grok -p "Reply with the single word ok." --no-memory --max-turns 1`

```json
{
  "hookEventName": "stop",
  "hook_event_name": "Stop",
  "sessionId": "22222222-2222-4222-8222-222222222222",
  "session_id": "22222222-2222-4222-8222-222222222222",
  "cwd": "/private/tmp/sam-phase0",
  "workspaceRoot": "/private/tmp/sam-phase0",
  "transcriptPath": "/Users/example/.grok/sessions/%2Fprivate%2Ftmp%2Fsam-phase0/22222222-2222-4222-8222-222222222222/updates.jsonl",
  "transcript_path": "/Users/example/.grok/sessions/%2Fprivate%2Ftmp%2Fsam-phase0/22222222-2222-4222-8222-222222222222/updates.jsonl",
  "permissionMode": "bypassPermissions",
  "permission_mode": "bypassPermissions",
  "reason": "shutdown",
  "stopHookActive": false,
  "stop_hook_active": false
}
```

**The transcript is `updates.jsonl`, not `chat_history.jsonl`.** It is an ACP
session-update stream:

```json
{"method":"session/update","params":{"update":{"sessionUpdate":"user_message_chunk","content":{"type":"text","text":"..."}}}}
{"method":"session/update","params":{"update":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"ok"}}}}
```

A transcript adapter for Grok must read `user_message_chunk` and
`agent_message_chunk`, and ignore `hook_execution`, `turn_completed` and
metadata. `chat_history.jsonl` exists beside it and is a usable fallback, but
it carries the full system prompt.

Grok also scans `~/.claude/settings.json` and `~/.cursor/hooks.json` by
default (`[compat.claude] hooks`, `[compat.cursor] hooks`). A Claude-style
Stop hook therefore fires in Grok as well; deduplication is mandatory.

### Antigravity CLI (`agy`)

Command: `agy -p "Reply with the single word ok." --print-timeout 90s`

```json
{
  "artifactDirectoryPath": "/Users/example/.gemini/antigravity-cli/brain/33333333-3333-4333-8333-333333333333",
  "continuationActive": false,
  "conversationId": "33333333-3333-4333-8333-333333333333",
  "cwd": "/private/tmp/sam-phase0",
  "error": "",
  "executionNum": 0,
  "fullyIdle": true,
  "hookEventName": "Stop",
  "lastUserInput": "Reply with the single word ok.",
  "modelName": "gemini-3.8-flash-high",
  "terminationReason": "NO_TOOL_CALL",
  "transcriptPath": "/Users/example/.gemini/antigravity-cli/brain/33333333-3333-4333-8333-333333333333/.system_generated/logs/transcript_full.jsonl",
  "workspacePaths": ["/private/tmp/sam-phase0"]
}
```

The payload is **camelCase and has no `hook_event_name`**; the transcript key
is `transcriptPath`, and `hookEventName` is `"Stop"`. The global hooks file is
`~/.gemini/config/hooks.json`; hooks in `~/.gemini/antigravity-cli/settings.json`
are not loaded. The schema is named hooks, not matcher groups:

```json
{"smart-agents-memory": {"Stop": [{"type": "command", "command": "..."}]}}
```

The transcript uses `USER_INPUT`, `PLANNER_RESPONSE`, `GENERIC` (tool output),
and other step types. `USER_INPUT` content wraps the prompt in
`<USER_REQUEST>...</USER_REQUEST>`; the adapter must strip that wrapper and
map `USER_INPUT` to `user`, `PLANNER_RESPONSE` to `assistant`.

### Cursor

The IDE has logged a real Stop payload (`cursor.hooks.*.log`), which records
the JSON sent to the hook:

```json
{
  "conversation_id": "44444444-4444-4444-8444-444444444444",
  "generation_id": "55555555-5555-4555-8555-555555555555",
  "model": "grok-4.7-high-fast",
  "model_id": "grok-4.7",
  "status": "completed",
  "loop_count": 0,
  "session_id": "44444444-4444-4444-8444-444444444444",
  "hook_event_name": "stop",
  "cursor_version": "3.30.14",
  "workspace_roots": ["/Users/example/Projects/Dizer/dizer-app"],
  "user_email": "example@example.com",
  "transcript_path": "/Users/example/.cursor/projects/.../agent-transcripts/<id>/<id>.jsonl"
}
```

The log also proves that Cursor loads `~/.claude/settings.json`:

```text
[Claude] Loaded Claude user hooks
Executing hook 1/1 from claude-user config
```

Cursor's own config is `~/.cursor/hooks.json`:

```json
{"version": 1, "hooks": {"stop": [{"command": "..."}]}}
```

The transcript is `agent-transcripts/<conversation>/<conversation>.jsonl` and
already normalizes to `Turn{Role, Text}` with the current parser. The `stop`
payload carries `transcript_path`, so the same reader works.

A fresh headless turn is still blocked: `cursor agent` installed
`cursor-agent 2026.11.22-e9042b2` at `~/.local/bin/cursor-agent`, but
`cursor-agent -p` returns:

```text
Error: Authentication required. Please run 'agent login' first, or set CURSOR_API_KEY environment variable.
```

The GUI is logged in, so one manual Agent turn with a temporary hook would
close this item; the CLI path needs `agent login` or `CURSOR_API_KEY`.

### Claude Code

Claude Code `2.1.332` was installed with
`npm install -g @anthropic-ai/claude-code`. A live Stop payload was captured
by pointing the CLI at a local Anthropic-compatible endpoint:

```text
ANTHROPIC_BASE_URL=http://127.0.0.1:18771
ANTHROPIC_API_KEY=probe-key
claude --settings /tmp/... --model claude-sonnet-4-5 --tools "" -p "Reply with the single word ok."
```

The local endpoint returned a streaming `message_start`/`content_block_delta`/
`message_stop` response. The CLI ran a real turn, loaded its hooks, and
executed the temporary Stop hook. The payload:

```json
{
  "session_id": "66666666-6666-4666-8666-666666666666",
  "transcript_path": "/Users/example/.claude/projects/-private-tmp-sam-phase0/66666666-6666-4666-8666-666666666666.jsonl",
  "cwd": "/private/tmp/sam-phase0",
  "prompt_id": "99999999-9999-4999-8999-999999999999",
  "permission_mode": "default",
  "hook_event_name": "Stop",
  "stop_hook_active": false,
  "last_assistant_message": "ok",
  "background_tasks": [],
  "session_crons": []
}
```

The payload shape is exactly the Qwen-compatible one, now verified live.
The hook file is `~/.claude/settings.json`, matcher-group format:

```json
{"hooks": {"Stop": [{"hooks": [{"type": "command", "command": "..."}]}]}}
```

The existing file already has `Notification`, `Stop` and `UserPromptSubmit`
hooks for the terminal; a writer must merge, never replace. A turn against the
real Anthropic API still needs `claude /login` or a real `ANTHROPIC_API_KEY`;
that is a user step, not a hook-contract unknown. The local endpoint test used
no real credentials.

### Codex

The `codex` binary is not on `PATH`, but the ChatGPT app ships a real CLI at
`/Applications/ChatGPT.app/Contents/Resources/codex-cli/CodexCLI.app/Contents/MacOS/codex`
(`codex-cli 0.167.0-alpha.4.1`). A live **non-ephemeral** turn confirmed the
`notify` payload:

```json
{
  "type": "agent-turn-complete",
  "thread-id": "77777777-7777-4777-8777-777777777777",
  "turn-id": "88888888-8888-4888-8888-888888888888",
  "cwd": "/tmp/sam-phase0",
  "client": "codex_exec",
  "input-messages": ["Reply with the single word ok."],
  "last-assistant-message": "ok"
}
```

The non-ephemeral turn used:

```sh
codex exec -C /tmp/sam-phase0 --skip-git-repo-check --json \
  -c 'notify=["/tmp/sam-phase0/codex-probe-persistent.sh"]' \
  "Reply with the single word ok."
```

It created:

```text
~/.codex/sessions/2026/11/25/rollout-2026-11-25T07-04-44-<thread-id>.jsonl
~/.codex/thread_history_1.sqlite
  thread_turns: 1 row
  thread_items: 2 rows (userMessage, agentMessage)
```

`thread_timeline_ledger` stayed **0** in both `codex.db` and `codex-dev.db`
before and after the non-ephemeral turn. It is not the transcript. The source
is the rollout JSONL, keyed by `thread-id` from the notify payload. Its
`response_item` messages have role `user`/`assistant`; developer messages and
the `# AGENTS.md instructions` user block must be filtered.

`~/.codex/config.toml` already assigns `notify` to Computer Use:

```toml
notify = [".../SkyComputerUseClient", "turn-ended"]
```

The notify route would need a dispatcher, but it is no longer necessary for
capture: `watch` can scan `~/.codex/sessions/**/rollout-*.jsonl` and leave the
Computer Use notify untouched. That is the recommended Tier A route.

The earlier `--ephemeral` probe left no rollout and no thread-history rows,
which is why the ledger check had to be repeated with a persistent turn.

### OpenCode

No shell hook. The supported integration is a server plugin:

- Discovery: `~/.config/opencode/plugins/*.ts` (or a package in `opencode.json`).
- Events: `ctx.event.subscribe({ types: ["session.idle"] })`.
- Context: `ctx.session.context({ sessionID })` returns the stored messages.
- The session store behind it is `~/.local/share/opencode/opencode.db`
  (`session_message.type`, `session_message.data.text`, `session_part`), which
  is a viable read-only fallback.

The plugin can therefore emit one neutral event per idle session, with the
last user/assistant turns, without reading the database directly.

### MiniMax Code

No hook or transcript contract is proven. The existing native memory links
(`~/.minimax/memory`, `~/.minimax/agents/main/memory`) keep the hot tier
available. Tier B.

## Hook payload keys

The neutral decoder accepts both spellings; no harness name is needed to read
the transcript path.

| Field | Keys seen |
|---|---|
| transcript | `transcript_path`, `transcriptPath` |
| session | `session_id`, `sessionId`, `conversation_id`, `conversationId` |
| event | `hook_event_name`, `hookEventName` |
| cwd | `cwd`, `workspaceRoot`, `workspace_roots[0]`, `workspacePaths[0]` |
| assistant text | `last_assistant_message` |
| user text | `lastUserInput` |

## Hook configuration locations

| Harness | Path | Shape |
|---|---|---|
| Claude Code | `~/.claude/settings.json` | `hooks.Stop[].hooks[]` (matcher group) |
| Qwen Code | `~/.qwen/settings.json` | `hooks.Stop[].hooks[]` (matcher group) |
| Cursor | `~/.cursor/hooks.json` | `hooks.stop[]` (flat), `version: 1` |
| Grok Build | `~/.grok/hooks/*.json` | `hooks.Stop[].hooks[]` (matcher group) |
| Antigravity CLI | `~/.gemini/config/hooks.json` | named hook, `Stop[]` flat handlers |
| OpenCode | `~/.config/opencode/plugins/*.ts` | plugin event subscription |

## Transcript locations

| Harness | Path | Format |
|---|---|---|
| Claude Code | `~/.claude/projects/**/*.jsonl` | JSONL, `type`, `message.content` |
| Qwen Code | `~/.qwen/projects/*/chats/*.jsonl` | JSONL, `type`, `message.parts` |
| Cursor | `~/.cursor/projects/*/agent-transcripts/*/*.jsonl` | JSONL, `role`, `message.content` |
| Grok Build | `~/.grok/sessions/*/*/updates.jsonl` | ACP `session/update` |
| Grok Build (fallback) | `~/.grok/sessions/*/*/chat_history.jsonl` | `type`, `content`; contains the system prompt |
| Antigravity CLI | `~/.gemini/antigravity-cli/brain/*/.system_generated/logs/transcript_full.jsonl` | JSONL, `USER_INPUT`/`PLANNER_RESPONSE`/`GENERIC` |
| Codex | `~/.codex/sessions/**/rollout-*.jsonl` | JSONL, `response_item` message with role user/assistant |
| OpenCode | `~/.local/share/opencode/opencode.db` | SQLite `session_message` + `session_part` |

## Open items

1. **Claude Code real-provider login.** The Stop contract is captured live
   through a local Anthropic-compatible endpoint. A real Anthropic turn still
   needs `claude /login` or an `ANTHROPIC_API_KEY`; the hook contract itself is
   closed.
2. **Cursor live probe.** The IDE payload is in the log and the transcript
   path is confirmed. A fresh turn needs either one GUI Agent turn or
   `cursor-agent` logged in (`agent login` / `CURSOR_API_KEY`); the CLI
   currently returns `Authentication required`.
3. **Codex adoption.** The non-ephemeral turn proved the rollout JSONL; the
   remaining work is the watch root and adapter, not another probe. Do not
   replace the Computer Use `notify` array; the watch route avoids it.
4. **Grok `updates.jsonl` adapter.** The transcript path is ACP, not
   `chat_history.jsonl`; the adapter must be written and tested against this
   fixture.
5. **Antigravity wrapper.** `USER_INPUT` and `PLANNER_RESPONSE` need mapping
   and tag stripping; a raw parser sees no `user` turn and silently advances
   the cursor.
6. **OpenCode plugin.** The event and context APIs are confirmed; the plugin
   and its test are Phase 5.

## Fixtures

- `internal/ingest/testdata/` — redacted payloads for Claude, Qwen, Cursor,
  Grok, Antigravity and Codex, plus a note on how each was captured.
- `internal/extract/testdata/` — synthetic transcripts in each native shape,
  including the Grok ACP stream, the Antigravity step types and the Codex
  rollout with its developer/AGENTS.md noise.

The fixtures are synthetic where a live payload contains user text. They keep
the real key names, nesting and event names; only identifiers, paths and text
are replaced.
