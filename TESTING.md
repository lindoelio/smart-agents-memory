# Testing

## The strategy

The suite is the proof of every change: a change ships with the test that
shows it works, and the suite passes before the pull request.

The strategy is layered, and each layer has a different job:

1. **The gateway round trip is the architectural test.** The same corpus is
   written to `obsidian`, `disk` and `sqlite` and read back unchanged:
   `TestBackendsCarryTheSameCorpus` and
   `TestTheCorpusTravelsBetweenBackends` in `internal/vault/vault_test.go`.
   The same file covers the behavior each file gateway owns: recursive
   discovery in Obsidian, a note without a type ignored by disk, the
   append-only log, search, create without overwrite and the modification
   time. `internal/vault/sqlite/sqlite_test.go` covers what only the
   database can do: rename, remove, project notes and literal search.
2. **The projection formats have their own round trip.** A rendered native
   file must parse back into the same entry:
   `TestTopicRoundTripKeepsSpacesAndDropsTheMarker`,
   `TestAgentAndUserRoundTrip`, `TestQwenRoundTrip` and
   `TestRenderHotIsStable` in `internal/native/native_test.go`. These formats
   are read by the tools, so a change here changes the product.
3. **The use cases are tested as a whole.** `internal/app/app_test.go`
   drives add, append, deduplication, the English warning, index, the hot
   tier, install, doctor, brief, migrate and the SQLite installation over a
   temporary store and a temporary home.
4. **The command line is tested through its own entrypoint.**
   `internal/cli/cli_test.go` calls `cli.Run` and checks the exit code and
   the output, including the pointer a legacy flag receives, `--version`
   without a store, and the error for a removed backend.
5. **Isolated logic has unit tests.** `internal/memory/memory_test.go`
   covers the entry format, the slugs, the normalization, the type mapping
   and the English check; `internal/skill/skill_test.go` covers
   materialization; `internal/config/config_test.go` covers the default
   store and a rejected backend; `internal/link/link_test.go` covers the
   backup of a file a tool overwrote.

The end-to-end path is the smoke job of CI: it builds the release binary and
runs `version`, `index`, `search`, `install` and `doctor` against a
temporary store.

## Running the suite

```bash
go test ./...
go vet ./...
gofmt -l .
```

The suite needs no token and no external network: every gateway is local,
and the tests use temporary directories. There is no fake service and no
credential to configure.

The prerequisites are Go 1.27.3 — the version in `go.mod` and in CI — and
nothing else. The SQLite driver is pure Go (`modernc.org/sqlite`), so no C
toolchain is required.

## Where the tests live

| Package | File | What it proves |
|---|---|---|
| `internal/memory` | `memory_test.go` | Parse and format round trip, unknown keys, slugs, normalization, types, tiers, the English check |
| `internal/vault` | `vault_test.go` | Every file gateway carries the same corpus; the corpus travels between backends; discovery, search, log and modification time |
| `internal/vault/sqlite` | `sqlite_test.go` | Rename, remove, project notes and literal search in the database |
| `internal/native` | `native_test.go` | The topic, agent, user and Qwen formats round trip; the hot block is stable |
| `internal/app` | `app_test.go` | Add, append, deduplication, index, the hot tier, install, doctor, brief, migrate, SQLite install |
| `internal/cli` | `cli_test.go` | Help, version, search, path, exit codes, legacy flags, a rejected backend |
| `internal/config` | `config_test.go` | The default store path, and the error of a backend that left the product |
| `internal/link` | `link_test.go` | A second overwrite keeps the first backup |
| `internal/skill` | `skill_test.go` | The skill lands in the layout the tools expect, with its references |

`internal/projection`, `internal/reconcile` and `internal/version` have no
test file of their own: the first two are exercised through the use cases,
and the third is a constant. When a change lands in one of them, add the
test at the level that proves the behavior.

## Writing a test

- Use the standard `testing` package. There is no assertion library: a
  failure is `t.Fatalf` or `t.Errorf` with the value and what was expected.
- A test name is a sentence: `TestInstallRepairsALinkAFileOverwrote`.
- The test owns its state. `t.TempDir()` for the store; `t.Setenv("HOME",
  ...)` before anything reads the home directory. No test writes outside a
  temporary directory.
- A matrix is a subtest:
  `for _, kind := range []string{"obsidian", "disk", "sqlite"} { t.Run(kind, ...) }`.
- A fake belongs in the test file, and only when the real dependency cannot
  run in the suite. Do not reach for a live service.
- Black-box tests use the `_test` package (`app_test`, `cli_test`,
  `vault_test`, `skill_test`); a test that needs unexported helpers stays in
  the package (`memory`, `native`, `config`, `link`, `sqlite`).
- Test the public surface — `cli.Run`, `app.App`, the `vault.Vault`
  interface — and assert the persisted state, not only the return value.

## What a change must cover

| Change | Test |
|---|---|
| A gateway | The round trip, plus create, update, search and the modification time |
| The reconciliation rule | A use-case test that proves which side wins when the corpus and a projection disagree |
| A native projection format | The render/parse round trip in `internal/native/native_test.go` |
| The entry format | The parse/format round trip and the unknown-key preservation |
| A command or a flag | `cli_test.go`: the exit code and the output |
| The installation | The links, the backup of an overwritten file, and `doctor` |
| The migration | The dry run, the apply, and the idempotency |

## Continuous integration

`.github/workflows/ci.yml` runs `gofmt -l .`, `go vet ./...`,
`go test ./...`, `go build ./...`, the release build with the version
check, the skill manifest check, and `shellcheck` on `scripts/*.sh`, on
Ubuntu and macOS. The `smoke` job builds the release binary on Ubuntu and
runs the commands above against a temporary store.

`.github/workflows/publish.yml` repeats the formatting, vet and test checks
before a release: a red suite stops the publication.

See [AGENTS.md](AGENTS.md) for the contracts the suite protects, and
[CONVENTIONS.md](CONVENTIONS.md) for the contribution workflow.
