# Changelog

All notable changes to this project are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and the project uses
[semantic versioning](https://semver.org/spec/v2.0.0.html).

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

[0.1.1]: https://github.com/lindoelio/smart-agents-memory/releases/tag/v0.1.1
[0.1.0]: https://github.com/lindoelio/smart-agents-memory/releases/tag/v0.1.0
