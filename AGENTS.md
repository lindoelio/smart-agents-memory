# AGENTS.md

The conventions of this repository, for the agents that work on it.

## What this project is

A CLI, an agent skill, and the wiring that connects AI tools to a shared
memory. The corpus is the single source of truth; the native stores are
projections.

## The shape of the code

Ports and adapters, on both sides.

- **Entrypoints** (`cmd/`, `internal/cli`) are adapters. They parse
  arguments and print results. All the behavior lives in the use cases.
- **Use cases** (`internal/app`) know the domain and the port. They never
  know the gateway.
- **The port** (`internal/vault`) is an interface, a registry, and a
  configuration. It never imports an adapter.
- **The gateways** (`internal/vault/*`) register themselves from `init`.
- **The domain** (`internal/memory`, `internal/native`) knows nothing about
  where the memory lives.

Adding a gateway: one package, one `Register` call, one line in
`cmd/smart-agents-memory/main.go`. Adding an entrypoint: one package over
`internal/app`.

## Conventions

- **English.** Identifiers, messages, comments, documentation and the
  content of the memory. The replies to the maintainer are in Brazilian
  Portuguese; the product is not.
- **Nouns for the domain.** `Entry`, `Vault`, `Tier`, `Projection`,
  `Reconciler`. No `mvp`, `pilot` or `beta` anywhere: a phase never enters
  the code.
- **No placeholders.** A gateway that cannot be verified is documented as
  unverified, not hidden.
- **The tests are the proof.** A change ships with the test that shows it
  works.
- **Comments say why.** The code says what it does; the comment says why it
  does it that way.

## The rules of the memory

These hold in the code and in the content of the store:

1. The corpus is the source of truth. Projections are regenerated, never
   merged. When they disagree, the most recent change wins.
2. The hot tier enters every session. The cold tier is fetched on demand.
3. An entry without a why is not reusable.
4. The stored memory is written in English.

## Checks

```bash
gofmt -l .
go vet ./...
go test ./...
```

The suite needs no token and no network.

## The contracts that must not break

- The format of an entry (`internal/memory/frontmatter.go`) and its
  documentation (`skills/smart-agents-memory/references/corpus-format.md`).
- The projection formats (`internal/native`), because the runtime of each
  tool reads them.
- The names of the topic files: the MiniMax runtime only recognizes
  `^[a-z0-9][a-z0-9-]{0,63}$`.
- The round trip between the gateways: the same corpus must come back
  unchanged from every backend.

## Before a release

- The version in `internal/version` matches the tag.
- `go test ./...`, `go vet ./...` and `gofmt -l .` are clean.
- The changelog describes the change, and the migration command knows how to
  move a store written by the previous version.
- The release pipeline updates the formula of the Homebrew tap.
