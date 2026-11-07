# Contributing

Thanks for taking the time.

## The shape of the code

The project is ports and adapters, on both sides.

```
cmd/smart-agents-memory   entrypoint: parses the arguments, prints the result
internal/cli               the command line adapter
internal/app               the use cases: search, brief, add, index, install…
internal/vault             the port of the store, and its registry
internal/vault/*           the gateways: obsidian, disk, sqlite, notion
internal/memory            the domain: entries, types, tiers, slugs
internal/native            the projection formats of each tool
internal/reconcile         the rule that keeps the corpus and the projections in step
internal/projection        the local native store
internal/config            where the store is resolved
internal/link              the symlinks of the installation
internal/skill             the skill embedded in the binary
```

Two rules hold the shape together:

1. **The domain knows no gateway.** `internal/memory` and `internal/native`
   never import `internal/vault`. The store is reached through the port.
2. **The port knows no gateway.** `internal/vault` never imports an adapter.
   An adapter registers itself from its `init`, and the command imports it
   for the side effect.

A new gateway is one package, one `Register` call, and a line in the
command. A new entrypoint is one package that calls `internal/app`.

## Ground rules

- **English.** Code, identifiers, messages, documentation and the content of
  the memory are all in English.
- **No placeholders.** Nothing ships half-done. A gateway that cannot be
  verified is documented as unverified.
- **The tests are the proof.** Every change comes with the test that shows
  it works, and the suite passes before the pull request.
- **The corpus is the source of truth.** A change in the reconciliation rule
  changes what every tool reads; it needs a test of its own.

## Running the checks

```bash
go test ./...
go vet ./...
gofmt -l .
```

The suite needs no token and no network: the Notion gateway is tested
against a fake API.

## A change in the memory format

The format of an entry is a contract with the tools and with the people who
read the notes. When you change it:

1. update `internal/memory/frontmatter.go`;
2. update `skills/smart-agents-memory/references/corpus-format.md`;
3. update the migration in `internal/app/index.go`, so a store written by
   the previous version can still be read;
4. add the round trip test.

## Commits and pull requests

One concern per commit. The message says what changed and why. A pull
request explains the problem before the solution, and lists the tests that
cover it.

## Reporting a bug

Open an issue with what you ran, what you expected, and what happened. If
the store is involved, `smart-agents-memory doctor` is the first thing to
paste: it reports the links, the projections and the index in one go.
