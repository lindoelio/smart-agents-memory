# SPEC.md audit — implementation status and findings

Date: 2026-11-25
Commit: `f672edd` (working tree: only `SPEC.md` untracked)
Version: `0.3.0` (released as `0.4.0`)
Scope: the six phases of `SPEC.md`, the invariants in §1, and the documentation
deliverables in §4.

## Verdict

Every phase is implemented and has an end-to-end path that works. The
verification gates exist and pass. The audit found **one high-severity
behavioral bug**, five medium-severity issues, and a set of low/informational
gaps. Nothing requires a redesign; the fixes are localized.

| Area | Status |
|---|---|
| Phase 1 — Pinned protection | Pass, with F3 (team path) and F15 (literal `List`) |
| Phase 2 — Repo and worktree scope | Pass |
| Phase 3 — Team memory and SecretGuard | Pass, with F3 and F17 |
| Phase 4 — Taxonomy, tree, recall, MCP | **F1 breaks query ranking**; tree/MCP pass |
| Phase 5 — LLM port and Dream | Pass, with F5, F7, F8, F12 |
| Phase 6 — Extract and hooks | Pass, with F9, F16 |
| §4 — Documentation | Two small gaps: F13, F14 |

## Baseline evidence

```
$ go test ./...        # all packages ok
$ go vet ./...         # no output
$ gofmt -l .           # no output
$ ./scripts/coverage.sh
  complete: satisfied (measurement and analysis are complete)
  decision:  satisfied (2448/2720; minimum 80.00%)
  function:  satisfied (442/444; minimum 90.00%)
  statement: satisfied (4847/5079; minimum 90.00%)
```

The suite is offline and deterministic. Everything below was also verified by
driving the built binary (`go build -o /tmp/sam ./cmd/smart-agents-memory`)
against a sandboxed `HOME` and temporary stores, so no real store was touched.

## What was verified working (end to end)

- **Phase 1.** `add --pinned --native qwen` writes `pinned: true` and
  `native/qwen/pinned/<slug>.md`, linked from `native/qwen/MEMORY.md`; a second
  `add` without `--force` fails with `entry "..." is pinned; pass --force to
  modify it`; `add --force` records `(append, forced on pinned)`; `pin`/`unpin`
  move the Qwen file between the root and `pinned/`; a newer tampered native
  file cannot overwrite the corpus; a legacy SQLite database gains `pinned`,
  `project`, `worktree`, `category`, `usage_scenarios` and
  `entries_project` on open.
- **Phase 2.** `add --scope project` auto-detects the repo from a nested
  directory; `--worktree auto` inside a linked worktree stores
  `project: repo` + `worktree: wt`; `brief` shows `## Project:` and
  `## Worktree:` with the right entries and notes; `brief --global` disables
  detection; after `git worktree remove`, `doctor` reports the stale entry and
  note, and `prune-worktrees --apply` removes the unpinned ones and keeps the
  repo-wide entry.
- **Phase 3.** `add --team` writes `.agents/memory/corpus/<type>/<slug>.md` and
  refreshes `MEMORY.md`; `--scope user --team` is refused; a fake AWS key is
  refused in both the private and the team store; `--native qwen` also projects
  into `.qwen/team-memory/`; `brief` renders `## Team Memory (repo)` and
  `search` prefixes hits with `[team] `; a symlinked `.agents` is refused and a
  symlinked note is not read.
- **Phase 4.** `recall --tree`, `--category`, `--query` and `--ref` work; the
  MCP server answers `initialize`, `tools/list` and all five `tools/call`
  handlers over real stdio; the tree tests enforce the 6000-char budget, the
  12-keyword cap with `HiddenKeywordCount`, and a deterministic revision.
- **Phase 5.** The deterministic pass merges duplicate titles/bodies, unions
  keywords/scenarios and backfills category/description; a stale lock is broken
  and a live lock refuses; with a fake OpenAI-compatible server, a valid merge
  is applied while operations that touch a pinned entry, name a missing source,
  carry a credential, or use a long non-English body are all rejected before
  any write.
- **Phase 6.** `extract --stdin` (one-shot) and `--transcript` with a session
  cursor both work; the cursor advances atomically, is clamped after a
  compaction, and a transcript with no new user turn returns
  `skipped: no_new_user_turns`; extracted entries are cold and unpinned; a
  pinned title is refused and a secret is blocked; `--auto-dream` fires at the
  threshold; `install --hooks` writes an executable `post-turn.sh` that exits 0
  even with a dead endpoint; `sync`/`watch --once` capture new turns.
- **Docs.** `pinned`, `project`, `worktree`, `category`, `usage_scenarios`,
  `internal/secretguard`, `internal/tree`, `internal/llm`, `internal/mcp`, the
  team boundary, the LLM-key-by-env rule and the MCP/hook integrations are
  documented. Two gaps remain (F13, F14).

## Findings

### F1 — High — `recall --query` returns entries that do not match the query

**Spec.** §4.4 mode 3: rank by a score built from the normalized terms, then
"boosting active `Project`/`Worktree` matches".

**Evidence.** `internal/app/recall.go:276-284`:

```go
if activeProject != "" && entry.Project == activeProject {
    score += 2
}
if activeWorktree != "" && entry.Worktree == activeWorktree {
    score += 3
}
if score > 0 {
    ranked = append(ranked, scored{item: item, score: score})
}
```

The boost is added **before** the `score > 0` filter, so every entry scoped to
the active repo (including every team entry, which carries `Project`) passes the
filter even when no term matches.

**Reproduction.** Inside a git repo with two project-scoped entries and two team
entries:

```
$ /tmp/sam recall --query "zzzznothing" --limit 10
## Recall

### [project:durable-project-decision] Durable project decision
...
### [project:repo-decision] Repo decision
...
### [team:corpus/lesson/team-pinned-rule] Team pinned rule
...
### [team:corpus/lesson/team-rule] Team rule
```

It also explains an earlier observation: `--query "Portuguese"` returned
"Adopt the tree router", which shares no term with the query.

**Impact.** Structured recall is supposed to be selective. Inside a repository,
any query dumps every repo-scoped and team memory, which defeats the token
budget and can mislead the model with irrelevant context. `--limit` does not
help because these entries outrank nothing.

**Fix direction.** Compute a `base` score from the terms first and only add the
project/worktree boost when `base > 0`; or keep the boost but filter on
`base > 0`. Add a regression test: a query with no matching term must return
nothing even when project/worktree entries exist.

---

### F2 — Medium — `App.Add` accepts an empty title and writes an invisible note

**Spec.** §4.5 makes `memory_add` a first-class MCP tool; §1.1 makes the corpus
the source of truth. An entry with no title cannot be part of it.

**Evidence.** `internal/app/app.go:369-390` validates the body but not the
title. Only the CLI guards it (`internal/cli/cli.go:437`), so the MCP entrypoint
and any programmatic caller bypass the guard.

**Reproduction.**

```
$ printf '%s\n' '{"jsonrpc":"2.0","id":1,"method":"tools/call","params":
  {"name":"memory_add","arguments":{"scope":"lesson","title":"",
  "body":"No title. Why: test."}}}' | smart-agents-memory mcp
{"jsonrpc":"2.0","id":1,"result":{"content":[
  {"text":"recorded: corpus/lesson/entry.md","type":"text"}]}}

$ smart-agents-memory search "No title"
nothing found for "No title"
```

The file contains `title: ""`; `memory.Parse` rejects it, so `Entries`, `brief`,
`search`, `index` and `recall` never see it again. `doctor` does not report it
either, because `filevault.Problems` only checks that a note can be opened.

**Impact.** Silent data loss and orphan notes (`entry.md`, `entry-2.md`, …).
A model that emits an empty title — a plausible failure — believes the memory
was stored.

**Fix direction.** Reject `strings.TrimSpace(opts.Title) == ""` in `App.Add`,
and add the same guard in `filevault.Create`/`sqlite.Create` as a backstop.
Optionally have `Problems` report notes whose frontmatter has no title.

---

### F3 — Medium — Team memory bypasses the pinned guard

**Spec.** §1.1/§1.4: a pinned entry is never modified by an import or by an
automated path, and a manual `add` on a pinned title requires `--force`.

**Evidence.** `internal/app/team.go:48` calls `teammemory.Add` without checking
the existing entry. `internal/teammemory/teammemory.go:130-170` appends to the
existing entry and only keeps `Pinned` sticky; it never refuses.

**Reproduction.**

```
$ smart-agents-memory add --scope lesson --team --pinned \
    --title "Team pinned rule" --body "Team pinned rule. Why: curated."
recorded (team): .agents/memory/corpus/lesson/team-pinned-rule.md

$ smart-agents-memory add --scope lesson --team \
    --title "Team pinned rule" --body "An unforced append."
appended (team): .agents/memory/corpus/lesson/team-pinned-rule.md
```

The same sequence against the private vault fails with
`entry "Private pinned rule" is pinned; pass --force to modify it`.

**Impact.** The protection invariant does not hold in one of the two stores.
A teammate can change a curated rule without `--force`.

**Fix direction.** In `App.addTeam`, read the team entry by normalized title and
return the same error when `Pinned && !opts.Force`. Add the mirror of the
private-vault test.

---

### F4 — Medium — The Qwen projection can emit invalid YAML

**Spec.** §1.4/§4.2: the Qwen projection is the file Qwen Code reads.
`native.RenderQwen`/`ParseQwen` must round-trip.

**Evidence.** `internal/native/native.go:168-176` writes `name`, `description`
and `keywords` unquoted:

```go
b.WriteString("name: " + e.Title + "\n")
b.WriteString("description: " + desc + "\n")
...
b.WriteString("  - " + k + "\n")
```

**Reproduction.**

```
$ smart-agents-memory add --scope lesson --native qwen \
    --title "Rule: colons are fine" --description "#1 priority" --tags "a: b"
$ cat native/qwen/rule-colons-are-fine.md
---
name: Rule: colons are fine
description: #1 priority
...
keywords:
  - map[a:b]
---
```

A strict YAML parser rejects `name: Rule: colons are fine`
(`mapping values are not allowed in this context`), reads `description` as
`null`, and reads the keyword as a map. Our own `ParseQwen` silently falls back
to a line parser, so the internal round-trip still works — which is why the
tests do not catch it. Qwen Code may not have that fallback.

**Impact.** A projection that the consuming tool cannot parse. Titles and
descriptions come from flags and from extracted memories, so this is easy to
hit.

**Fix direction.** Quote every scalar, as `usage_scenarios` already does with
`yamlScalar`. Add a test that parses the rendered file with `yaml.Unmarshal`
rather than only with `ParseQwen`.

---

### F5 — Medium — The documented default LLM endpoint is never attempted

**Spec.** §5.2: `LLMProvider` defaults to `openai`; the adapter defaults
`BaseURL` to `http://127.0.0.1:8080/v1` (local gateway) or OpenAI when the key
variable is set.

**Evidence.** `internal/cli/cli.go:136-141`:

```go
if cfg.LLMBaseURL != "" || cfg.LLMModel != "" || os.Getenv(cfg.LLMKeyEnv) != "" {
    a.LLM = llmopenai.New(...)
}
```

A fresh `install` writes `{"llm_provider":"openai","llm_key_env":"..."}` with no
URL and no model, so `a.LLM` stays `nil`.

**Reproduction.** With a fake OpenAI-compatible server on the documented
default port `127.0.0.1:8080` and only `SMART_AGENTS_MEMORY_LLM_PROVIDER=openai`
set:

```
$ smart-agents-memory dream
dream: merged=0 updated=0 deleted=0 backfilled=0   # no request reached the server
$ smart-agents-memory extract --stdin </dev/null
error: extraction needs an LLM client
```

This contradicts the README ("The local `smart-llm-gateway` is the default when
no key is set") and the configuration file, which advertises the provider.

**Impact.** The semantic Dream stage and extraction look configured but are
off. Setting `llm_model` or `llm_base_url` works around it, so the bug is easy
to miss in development.

**Fix direction.** Build the client when `cfg.LLMProvider` is set (it always is,
because it defaults to `openai`), and let the adapter apply its default URL.
Add an explicit way to disable the LLM (`llm_enabled: false` or a `--no-llm`
flag) if opt-out matters. Otherwise, stop writing `llm_provider` in `install`
and document that a URL or model is required.

---

### F6 — Low — `log` is not newest-first within a day

**Spec/contract.** `vault.Vault.LogLines` and `App.Log` document "newest first".
§1.4/§5.3 rely on the write log being readable.

**Evidence.** `internal/vault/filevault/filevault.go:282-320` sorts the day
files in reverse but then appends the lines of each file in file order (oldest
first) and stops at `limit`.

**Reproduction.**

```
$ for i in 1 2 3; do smart-agents-memory add ... --title "Log line $i" ...; done
$ smart-agents-memory log --limit 2
- 10:53 | add | lesson | Log line 1 | ...
- 10:53 | add | lesson | Log line 2 | ...
```

Expected: `Log line 3`, `Log line 2`.

**Impact.** `log --limit N` hides the most recent activity. The existing test
`TestLogPrintsTheWriteLogNewestFirst` only asserts that a line is present, so it
does not catch the order.

**Fix direction.** Reverse the lines of each day file, or collect all lines and
sort them newest-first before applying the limit.

---

### F7 — Low — `dream --dry-run` mutates the state file

**Evidence.** `internal/app/dream.go:174-180` writes `LastStatus = "noop"` and
saves the state even in a dry run.

**Reproduction.**

```
before: {"last_dream_at":"...","dirty_mutations":4,"last_status":"updated"}
$ smart-agents-memory dream --dry-run --deterministic-only
after:  {"last_dream_at":"...","dirty_mutations":4,"last_status":"noop"}
```

**Impact.** A dry run changes observable state and erases the previous status.
Any consumer of `last_status` sees a lie.

**Fix direction.** Do not save the state on a dry run, or keep a separate
`last_dry_run` field.

---

### F8 — Low — `DreamState.LastStatus` never records `running`, `failed` or `idle`

**Spec.** §5.3 lists `"idle", "running", "updated", "noop", "failed"`.
**Evidence.** `internal/app/dream.go` only ever writes `"noop"` and `"updated"`.
On error the function returns before saving, so the previous status survives.

**Impact.** A crashed or failed consolidation is invisible; the state file
cannot be used to diagnose a run.

**Fix direction.** Write `"running"` after the lock is acquired, set
`"failed"` in a deferred error path, and use `"idle"` when nothing is running
and no status is known.

---

### F9 — Low — The English gate is bypassed for texts under 8 words

**Spec.** §5.3 (reject `!memory.IsEnglish(NewBody)`) and §6.3 (candidates must
be English).
**Evidence.** `internal/memory/lang.go:40`: `if len(words) < 8 { return true }`.
The helper was written for a warning, and is now used as a fail-closed gate.

**Reproduction.**

```
IsEnglish("Não faça isso porque quebra o build.") => true   # 7 words
```

A Dream operation or an Extract candidate with a short Portuguese body is
accepted. A longer Portuguese body (≥ 8 words with Portuguese markers) is
correctly rejected — verified end to end.

**Impact.** Short non-English entries can enter the corpus through the
automated paths, which is exactly what the gate is meant to prevent.

**Fix direction.** Use a stricter check at the gate (for example, require at
least one English stopword or a minimum share of English words) and keep the
lenient version for the CLI warning. Add table tests for short Portuguese
bodies.

---

### F10 — Low — Ranked recall includes hot entries

**Spec.** §4.4 mode 3: "ranks **cold** + project + team entries".
**Evidence.** `internal/app/recall.go:244-289` scores every item from
`recallItems`, with no tier filter.

**Reproduction.** Add a hot entry whose title matches the query; it appears in
`recall --query`.

**Impact.** Redundant context: hot entries are already injected by `brief` and
the projections.

**Fix direction.** Skip `e.EffectiveTier() == memory.TierHot` in `rankRecall`.

---

### F11 — Low — Recall repeats the description as the first body line

**Evidence.** `internal/app/recall.go:299-315` prints `Description` and then the
whole body. `dreamBackfill` derives the description from the first body line
(`internal/app/dream.go:264-279`), so the two always coincide after a Dream.

**Reproduction.** `recall --query "layered layout"` prints:

```
### [project:repo-decision] Repo decision

Use the layered layout. Why: it keeps the UI independent.

Use the layered layout. Why: it keeps the UI independent.
```

**Impact.** Noise and wasted `--max-chars` budget.

**Fix direction.** Omit the description when it equals the first sentence or
line of the body.

---

### F12 — Low — `DirtyMutations` counts index runs, not imported entries

**Spec.** §5.3: increment on every `App.Add` and `Reconciler.Import` that adds
or updates an entry.
**Evidence.** `internal/app/index.go:79` calls `a.bumpDirty()` once when
`imported+updated > 0`, not `imported+updated` times.

**Impact.** `--min-dirty` and the auto-dream threshold trigger earlier than the
configured number of memories.

**Fix direction.** Increment by `imported+updated` (a `bumpDirtyBy(n)` helper),
and keep `App.Add` at one.

---

### F13 — Low (documentation) — The READMEs do not document `extract` or `watch`

**Spec.** §4: "`README.md` and `README.pt-BR.md`: document `pin`/`unpin`, ...,
`dream`, and `extract`/`watch`."

**Evidence.**

```
$ grep -n "smart-agents-memory extract" README.md README.pt-BR.md
(none)
$ grep -n "smart-agents-memory watch" README.md README.pt-BR.md
(none)
```

`README.md` uses "extract" only as a verb ("extracts nothing"). `watch` appears
only as `watch_dirs`. Both commands are documented in
`skills/smart-agents-memory/references/cli-reference.md:105` and `:127`.

**Impact.** A reader of the README does not learn that `extract` exists, while
the spec requires it.

**Fix direction.** Add `extract` and `watch` to the "Use it" block, with a note
that `watch` is a deprecated alias of `sync`. Mirror it in `README.pt-BR.md`.

---

### F14 — Low (documentation) — `ARCHITECTURE.md` does not name `usage_scenarios`

**Spec.** §4: "`ARCHITECTURE.md` and `docs/MEMORY_FORMAT.md`: document
`pinned`, `project`, `worktree`, `category`, `usage_scenarios`, ...".

**Evidence.** `ARCHITECTURE.md:201` and `:215` say "usage scenarios" in prose;
the key `usage_scenarios` appears only in `docs/MEMORY_FORMAT.md`.
`grep -c usage_scenarios ARCHITECTURE.md` → 0.

**Impact.** Minor: the concept is there, the wire name is not.

**Fix direction.** Use the key name once in the sentence about the leaf and the
Dream merge.

---

### F15 — Info — `projection.Store.List` does not include `pinned/*.md` for Qwen

**Spec.** §1.4 asks `List(target, sub)` itself to return top-level `.md` and
`pinned/*.md` for `native.TargetQwen`.

**Evidence.** `internal/projection/projection.go:83-104` lists only the
requested directory and skips subdirectories. The reconciler compensates by
calling `List(qwen, "")` and `List(qwen, "pinned")` separately
(`internal/reconcile/reconcile.go:487-494`), and `Export` removes stale files
with a second `removeAbsent` (`internal/reconcile/reconcile.go:218-227`).

**Impact.** None today: every current caller compensates. A future caller that
trusts `List` would miss pinned files.

**Fix direction.** Either implement the spec in `List` (documenting the
behavior for Qwen) or record the compensation in a comment so the contract is
explicit.

---

### F16 — Info — `watch` is a deprecated alias, and `App.WatchOnce` is dead code

**Spec.** §6.4: `watch [--once] [--interval 5m] [--auto-dream]` scans the
configured directories and runs an incremental `Extract`.

**Evidence.** `internal/cli/cli.go:671-713` implements `watch` by delegating to
`sync` (with a deprecation warning). `internal/app/watch.go:30` defines
`WatchOnce`, which no non-test code calls (`grep -rn "WatchOnce" internal/`
returns only the definition and the tests).

**Impact.** The capability exists and is better tested under `sync`; the dead
path is a maintenance trap and the command name is documented differently from
the spec.

**Fix direction.** Choose one: delete `WatchOnce` and document `watch` as a
deprecated alias, or wire `watch` back to `WatchOnce` if the strict/plain-text
scan is still wanted. Either way, align §6.4's story in the docs.

---

### F17 — Info — Symlinked team notes are skipped silently

**Spec.** §3.3: verify with `os.Lstat` that every file is a real file and
"refuse symlinks pointing outside `<WorktreeRoot>`".

**Evidence.** `internal/teammemory/teammemory.go:263-265` skips a symlinked
entry during `readEntries`; `safeWrite` refuses a write through one. The write
path is safe, but the read path gives no diagnostic.

**Impact.** A symlinked team note is invisible with no warning; `doctor` does
not mention it.

**Fix direction.** Keep skipping, but surface the path as a warning (or as a
`doctor` problem) so a human knows the note was ignored.

---

### F18 — Info — Test gaps

- No CLI test for auto-detected `brief` inside a **linked worktree** (spec §2.5
  asks for both the main checkout and the linked worktree). It was verified
  manually and works; only the main checkout has a test
  (`TestBriefAutoDetectsTheRepository`).
- No test for `projection.Store.List(TargetQwen, "")` including `pinned/*.md`.
- `TestLogPrintsTheWriteLogNewestFirst` does not assert the order, which is why
  F6 is invisible.
- No test for `App.Add` with an empty title (F2).
- No test for the pinned guard in the team store (F3).
- No test that `recall --query` returns nothing when no term matches (F1).
- No test for the English gate on short non-English text (F9).

---

### F19 — Info — `secretguard` private-key regex differs from the spec

**Spec.** Rule 19 uses `.{64,16384}?`; `internal/secretguard/secretguard.go:48`
uses `[\s\S-]{64,}?`. The hyphen is a literal at the end of the class, so
`[\s\S-]` is "any character" — a superset of the spec with no upper bound. It
cannot miss a secret the spec would catch; the only difference is the 16384
cap. Leave it, or align it for documentation fidelity.

### F20 — Info — `ExtractOptions` has no `Project`

**Spec.** §6.3 rule 4 says auto-populate `Project` "when `opts.Project == ""`".
The implementation has no `Project` field (`internal/app/extract.go:26-49`) and
always resolves from `opts.CWD`. There is no CLI flag for it either, so there is
no user-facing gap today; add the field if another entrypoint must force a
project.

### F21 — Info — `--worktree auto` needs `--scope project` or an explicit `--project`

**Spec.** §2.4 attaches auto worktree resolution to `--scope project`.
`add --scope lesson --worktree auto` returns
`--worktree auto needs a project; pass --project or --scope project`. This is
defensible (a lesson has no inherent repo), but it should be a documented
decision rather than an emergent error.

### F22 — Info — Dream dry-run overstates `backfilled`

A dry run counts the entries that a real run would delete first. Observed:
`backfilled=9` in the dry run, `backfilled=8` in the real run. Cosmetic; make
the dry run simulate the post-dedupe set, or document the approximation.

## Suggested fix order

1. **F1** (recall boost) — one condition, one regression test; highest impact.
2. **F2** (empty title) and **F3** (team pin) — two guards, mirror tests.
3. **F4** (Qwen YAML) — quote scalars, add a `yaml.Unmarshal` round-trip test.
4. **F5** (LLM default) — decide the product behavior, then wire or document.
5. **F6**, **F7**, **F9** — small, isolated, each with a test.
6. **F13**, **F14** — documentation.
7. The info items in one cleanup pass (F15–F17, F19–F22), and the test gaps in
   F18 with the corresponding fixes.

## Method

Baseline: `go test ./...`, `go vet ./...`, `gofmt -l .`,
`./scripts/coverage.sh`. Behavior: the binary built from this commit, a
throwaway `HOME`, temporary stores for `obsidian`, `disk` and `sqlite`, real
`git` checkouts and linked worktrees, a fake OpenAI-compatible HTTP server for
Dream/Extract/`recall --llm`, and a JSON-RPC pipe for MCP. Two independent
spec-vs-code audits were run in parallel and their findings were re-checked
against the code and the binary before being included here.

## Resolution — 2026-11-25

Every finding was addressed, except F19, where the wider regex was kept on
purpose. The decisions and the implementation plan live in
`~/.opencode/plan/spec-audit-fix-plan.md`.

| Finding | Resolution | Main evidence |
|---|---|---|
| F1 | The relevance base is computed first; the project/worktree boost only applies when it is non-zero. | `internal/app/recall.go`; `TestRecallQueryDoesNotReturnUnrelatedScopedEntries`, `TestRecallQueryBoostsTheActiveScope` |
| F2 | `App.Add` and both `Create` gateways reject an empty title; the doctor reports a titleless entry. | `internal/app/app.go`, `internal/vault/filevault/filevault.go`, `internal/vault/sqlite/sqlite.go`; `TestAddRejectsAnEmptyTitle`, `TestMCPMemoryAddRejectsAnEmptyTitle`, `TestProblemsReportsATitlelessNote` |
| F3 | `add --team` follows the private-vault pin rule. | `internal/app/team.go`; `TestAddTeamRefusesAPinnedEntryWithoutForce`, `TestAddTeamForceAppendsToAPinnedEntry` |
| F4 | Every Qwen scalar and every corpus keyword is YAML-safe. | `internal/native/native.go`, `internal/memory/frontmatter.go`; `TestRenderQwenIsValidYAML`, `TestKeywordsWithYAMLPunctuationRoundTrip` |
| F5 | New `llm_enabled` toggle (default off); when on, the adapter uses the documented default URL. An ambient API-key variable alone no longer enables the client. | `internal/config/config.go`, `internal/cli/cli.go`, `internal/app/install.go`; `TestLLMToggleDefaultsOff`, `TestBuildEnablesTheLLMFromTheToggle`, `TestInstallWritesTheLLMToggle`, `TestDreamWithoutAnEndpointStaysDeterministic` |
| F6 | The write log is read newest-first within each day. | `internal/vault/filevault/filevault.go`; `TestLogLinesAreNewestFirst`, strengthened `TestLogPrintsTheWriteLogNewestFirst` |
| F7 | A dry run no longer writes the state file. | `internal/app/dream.go`; `TestDreamDryRunDoesNotTouchTheStateFile`, `TestDreamDryRunLeavesNoStateFile` |
| F8 | `idle` is the default; a run records `running`, then `updated`/`noop`, and `failed` on error. | `internal/app/dream.go`; `TestDreamRecordsRunningThenUpdated`, `TestDreamRecordsFailedStatus`, `TestDreamRecordsFailedStatusAfterALateFailure` |
| F9 | `IsEnglishStrict` gates Dream and Extract; the lenient check still only warns. | `internal/memory/lang.go`, `internal/app/dream.go`, `internal/app/extract.go`; `TestIsEnglishStrict`, `TestExtractRejectsEveryCandidateGate/short_non_English` |
| F10 | Hot entries are excluded from ranked recall. | `internal/app/recall.go`; `TestRecallQueryExcludesHotEntries` |
| F11 | A description equal to the body's first line is printed once. | `internal/app/recall.go`; `TestRenderRecallItemsDoesNotRepeatTheBody` |
| F12 | The importer adds `imported+updated` to the dirty counter. | `internal/app/index.go`, `internal/app/dream.go`; `TestDreamImportCountsEveryEntry` |
| F13 | Both READMEs document `extract` and `watch`. | `README.md`, `README.pt-BR.md` |
| F14 | `ARCHITECTURE.md` names `usage_scenarios`. | `ARCHITECTURE.md` |
| F15 | `projection.Store.List` includes `pinned/*.md` for Qwen. | `internal/projection/projection.go`, `internal/reconcile/reconcile.go`; `TestListIncludesPinnedQwenTopics`, `TestExportRemovesAStalePinnedFile` |
| F16 | `App.WatchOnce` and its helpers were deleted; `watch` stays a deprecated alias of `sync`. | `internal/app/watch.go` removed |
| F17 | The doctor reports symlinked team notes. | `internal/app/doctor.go`; `TestDoctorReportsASymlinkedTeamNote` |
| F18 | The missing tests were added (linked-worktree brief, log order, empty title, team pin, no-match recall, short non-English). | see the tests above |
| F19 | The wider private-key regex was kept, with a comment explaining the deliberate difference. | `internal/secretguard/secretguard.go` |
| F20 | `ExtractOptions.Project` is honored before the CWD. | `internal/app/extract.go`; `TestExtractHonorsTheExplicitProject` |
| F21 | `--worktree auto` works with any scope; the main checkout stays repository-wide. | `internal/app/worktree.go`; `TestAddWorktreeAutoScopesALessonToTheLinkedWorktree`, `TestBriefAutoDetectsTheLinkedWorktree` |
| F22 | A dry run reports the same counts as a real run (survivor set). | `internal/app/dream.go`; `TestDreamDryRunReportsTheSameCounts` |

Follow-up in 0.4.1: the F2 check originally reported a project note
(`projects/<name>.md`, which has `type: project` and no title by convention)
as a titleless entry. The check now skips the `projects/` folder, and the
MCP test that installs the links sets a temporary `HOME` so a test never
touches the real tool configuration.

One extra bug was found by the end-to-end pass while fixing F4: `memory.Format`
wrote keywords unquoted, so a keyword such as `a: b` came back as `map[a:b]`
from the corpus. It is fixed and covered by
`TestKeywordsWithYAMLPunctuationRoundTrip`.

### Final verification

```
gofmt -l .                 # clean
go vet ./...               # clean
go test ./...              # all packages ok
./scripts/coverage.sh      # decision 2473/2740, function 446/448,
                           # statement 4872/5105 — all gates satisfied
```

The independent end-to-end pass repeated F1–F4, F6, F7, F9, F17 and F21 on a
fresh sandbox and all of them passed.
