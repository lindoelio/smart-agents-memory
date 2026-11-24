# Conventions

Thanks for taking the time. These are the conventions of this repository:
its shape, its vocabulary, its code style and the workflow of a contribution.
They apply to the code, the documentation and the content of the memory.

## The shape of the code

The project is ports and adapters, on both sides.

```
cmd/smart-agents-memory   entrypoint: parses the arguments, prints the result
internal/cli               the command line adapter
internal/app               the use cases: search, brief, add, index, install…
internal/vault             the port of the store, and its registry
internal/vault/*           the gateways: obsidian, disk, sqlite
internal/vault/filevault   the shared base of obsidian and disk
internal/memory            the domain: entries, types, tiers, slugs
internal/tree              the memory tree snapshot, router and subtree
internal/gitscope          the repository and linked-worktree resolver
internal/secretguard       the high-confidence credential scan
internal/teammemory        the Git-backed team store and Qwen projection
internal/llm               the optional JSON completion port
internal/llm/openai        the OpenAI-compatible adapter
internal/native            the projection formats of each tool
internal/reconcile         the rule that keeps the corpus and the projections in step
internal/projection        the local native store
internal/config            where the store is resolved
internal/link              the symlinks of the installation
internal/skill             the skill embedded in the binary
```

Two rules hold the shape together:

1. **The domain knows no gateway.** `internal/memory`, `internal/native`,
   `internal/gitscope`, `internal/tree`, `internal/secretguard` and
   `internal/teammemory` never import `internal/vault`. The store is reached
   through the port.
2. **The port knows no gateway.** `internal/vault` never imports an adapter.
   An adapter registers itself from its `init`, and the command imports it
   for the side effect.

A new gateway is one package, one `Register` call, and a line in the
command. A gateway that stores notes in a directory shares `filevault` with
`obsidian` and `disk` instead of reimplementing the note handling. A new
entrypoint is one package that calls `internal/app`.

## Code style

### Naming

The vocabulary is the domain, and it is stable across the code, the flags,
the documentation and the stored memory:

| Thing | Convention | Examples |
|---|---|---|
| Packages | one short lowercase noun | `app`, `vault`, `memory`, `native` |
| Exported types | domain nouns | `Entry`, `Vault`, `Reconciler`, `Projection` |
| Entry types | the four fixed scopes | `user`, `lesson`, `project`, `reference` |
| Tiers | two fixed values | `hot`, `cold` |
| Categories | Qwen's twenty semantic buckets | `testing_standard`, `important_decision` |
| Scope | repository slug, then worktree id | `project`, `worktree` |
| Gateways | lowercase registered names | `obsidian`, `disk`, `sqlite` |
| Native targets | the tool, or the tool and the store | `minimax-user`, `minimax-agent`, `qwen` |
| CLI flags | long, kebab-case | `--memory-home`, `--allow-other-language` |
| Environment variables | the product prefix | `SMART_AGENTS_MEMORY_HOME`, `SMART_AGENTS_MEMORY_VAULT` |
| Configuration keys | snake_case | `memory_home`, `sqlite_path`, `projection_root` |
| Corpus note | the slug of the title, accents kept | `reconciliação-do-corpus.md` |
| Native topic | ASCII, at most 64 characters | `reconciliacao-do-corpus-mtime-decide.md` |

A project phase is never a name. There is no `mvp`, `pilot` or `beta` in a
path, a symbol, a flag, a comment or a document: the name says what the
thing is, not when it was built.

The MiniMax runtime only recognizes a topic file whose name matches
`^[a-z0-9][a-z0-9-]{0,63}$`. `memory.ASCIISlug` produces that name; a corpus
note keeps the accents, because its name is only an address.

### Go

- **Formatted by `gofmt`.** A file that `gofmt -l .` names is a failed
  check.
- **`go vet` is part of the gate.** There is no separate linter.
- **Packages say what they are for.** The primary file of a package opens
  with a package comment; exported declarations carry a doc comment.
- **Errors are contextual and lowercase**, and a wrapped cause uses `%w`:
  `fmt.Errorf("cannot import %q: %w", title, err)`.
- **Comments say why.** The code says what it does; a comment explains the
  intent, a workaround or a constraint that is not obvious from the code.
- **The standard library first.** The direct dependencies are deliberate:
  `gopkg.in/yaml.v3` for the frontmatter, `modernc.org/sqlite` (pure Go, so
  the binary cross-compiles without a C toolchain) and `golang.org/x/text`
  for the Unicode slugs.
- **Small interfaces.** A capability only some gateways have is a separate
  optional interface (`Layout`, `ProjectNotes`, `Renamer`, `Remover`,
  `IndexLocator`, `IndexWriter`, `Diagnostics`), and the use case checks for
  it.
- **Paths use `filepath`**, so the same code builds for macOS, Linux and
  Windows.
- **The CLI returns `0` on success, `1` on a runtime failure and `2` on a
  usage error.**
- **A test name is a sentence:** `TestTheCorpusTravelsBetweenBackends`.

## Contributing

### Ground rules

- **English.** Code, identifiers, messages, documentation and the content of
  the memory are all in English.
- **No placeholders.** Nothing ships half-done. A gateway that cannot be
  verified is documented as unverified, or it is not shipped at all.
- **The tests are the proof.** Every change comes with the test that shows
  it works, and the suite passes before the pull request.
- **The corpus is the source of truth.** A change in the reconciliation rule
  changes what every tool reads; it needs a test of its own.

### Running the checks

```bash
go test ./...
go vet ./...
gofmt -l .
```

The suite needs no token and no network: every gateway is local, and the
tests use temporary directories.

CI runs the same three checks on Ubuntu and macOS, then `go build ./...`,
the release build with the version check, the skill manifest check, and
`shellcheck` on `scripts/*.sh`. The smoke job runs the binary against a
temporary store. A red check stops a release.

### A change in the memory format

The format of an entry is a contract with the tools and with the people who
read the notes. When you change it:

1. update `internal/memory/frontmatter.go`;
2. update `skills/smart-agents-memory/references/corpus-format.md`;
3. update the migration in `internal/app/index.go`, so a store written by
   the previous version can still be read;
4. add the round trip test.

### Commits and pull requests

One concern per commit. The message says what changed and why. A pull
request explains the problem before the solution, and lists the tests that
cover it.

The history reads as a sentence, in the imperative: `Fix where the skill
lands`, `Answer --version without a store`. The default branch is `main`;
CI runs on every pull request and on every push to `main`.

### Documentation

- `README.md` is the English entry point; `README.pt-BR.md` mirrors it.
  A user-facing command, flag or path changes in both.
- `CHANGELOG.md` follows
  [Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and
  [semantic versioning](https://semver.org/spec/v2.0.0.html). The release
  notes are the section for the version.
- `skills/smart-agents-memory/` ships inside the binary. Its references are
  the documentation every tool reads; changing them changes the product.
- The guideline set lives at the root: [AGENTS.md](AGENTS.md),
  [CONVENTIONS.md](CONVENTIONS.md), [TESTING.md](TESTING.md),
  [ARCHITECTURE.md](ARCHITECTURE.md) and [SECURITY.md](SECURITY.md). The
  reference documents live in `docs/`:
  [INTEGRATIONS.md](docs/INTEGRATIONS.md),
  [MEMORY_FORMAT.md](docs/MEMORY_FORMAT.md),
  [MIGRATION.md](docs/MIGRATION.md) and
  [DISTRIBUTION.md](docs/DISTRIBUTION.md). Do not duplicate one document in
  the other.

### Releases

A release is a tag `v<version>`; the tag must match `internal/version`.
`.github/workflows/publish.yml` verifies the checks, cross-compiles for
macOS, Linux and Windows, publishes the release and updates the Homebrew
formula. The checklist before a tag is in [AGENTS.md](AGENTS.md).

### Reporting a bug

Open an issue with what you ran, what you expected, and what happened. If
the store is involved, `smart-agents-memory doctor` is the first thing to
paste: it reports the links, the projections and the index in one go.
