# Distribution

## The channels

| Channel | What it delivers |
|---|---|
| GitHub Releases | The binary, for every platform |
| Homebrew | The formula that installs the binary |
| The agent skill registry | The skill, for `npx skills add` |

The store — the memory itself — is **not** distributed with the tool. It is
private, and it belongs to the person who owns it.

## The platforms

| Platform | Architecture | Asset |
|---|---|---|
| macOS | arm64 | `smart-agents-memory-darwin-arm64` |
| macOS | amd64 | `smart-agents-memory-darwin-amd64` |
| Linux | arm64 | `smart-agents-memory-linux-arm64` |
| Linux | amd64 | `smart-agents-memory-linux-amd64` |
| Windows | amd64 | `smart-agents-memory-windows-amd64.exe` |

Every release carries a `SHA256SUMS` file.

The build is pure Go: the SQLite driver has no cgo, so the cross-compile
needs no C toolchain and no platform-specific setup.

## Installing

```bash
# Homebrew
brew install lindoelio/smart-agents-memory/smart-agents-memory
```

The command works on a machine that never tapped the repository: Homebrew
clones the public tap, downloads the binary from the public release, and
verifies its checksum. To tap it by hand and install from the tap:

```bash
brew tap lindoelio/smart-agents-memory
brew install smart-agents-memory
```

From a release:

```bash
curl -fsSLO https://github.com/lindoelio/smart-agents-memory/releases/latest/download/smart-agents-memory-darwin-arm64
chmod +x smart-agents-memory-darwin-arm64
sudo install -m 755 smart-agents-memory-darwin-arm64 /usr/local/bin/smart-agents-memory
```

Then wire the tools up:

```bash
smart-agents-memory install
```

The command creates the store when there is none, at `~/Smart Agents
Memory`, and links the tools to it. To use another location:

```bash
smart-agents-memory --memory-home ~/Documents/MyVault/Smart\ Agents\ Memory install
```

## Upgrading

```bash
brew upgrade smart-agents-memory
```

The formula of the tap points at the release of the version it was
rewritten for, and the release pipeline rewrites it on every tag. The
checksum in the formula is the checksum of the published asset, so Homebrew
refuses to install anything that was tampered with.

A store written by an older version is brought forward by the `migrate`
command, and the changelog says what changed.

## The skill

```bash
npx skills add lindoelio/smart-agents-memory
```

The skill is also embedded in the binary, so `smart-agents-memory install`
materializes it and links it into the eight skill directories. A user who
installs only the binary never needs the repository.

## The release pipeline

`publish.yml` runs on a tag, and publishes nothing before the checks pass.

1. **Verify the tag.** The tag must match the version declared in
   `internal/version`. A mismatch stops the pipeline: nothing is published.
2. **Verify the code.** `go vet`, the test suite, the build, and a smoke
   test of the built binary — `--version` and `doctor`.
3. **Cross-compile.** One job per platform, from a clean runner.
4. **Publish the release.** The assets and the checksums, on the tag.
5. **Update the formula.** The formula of the `homebrew-smart-agents-memory`
   tap is rewritten with the new version, the new URLs and the new
   checksums, and pushed.

The pipeline never publishes by hand. Push the commit, wait for the CI on
that exact commit, tag it, push the tag.

## The Homebrew tap

The tap is a separate repository: `lindoelio/homebrew-smart-agents-memory`.

```
homebrew-smart-agents-memory/
├── Formula/
│   └── smart-agents-memory.rb
├── LICENSE
└── README.md
```

The formula pins the URL and the checksum of every platform, installs the
binary as `smart-agents-memory`, and runs `--version` as its test.

The tap needs a token with write access to that repository. It is a
repository secret named `HOMEBREW_TAP_TOKEN`, and it is used by the
`homebrew` job of the release pipeline.

## Publishing by hand

Usually nothing has to be done by hand. When it does:

```bash
git tag -a v0.2.0 -m "v0.2.0"
git push origin v0.2.0
```

The pipeline takes it from there.

## Before the first release

- The version in `internal/version` is the one you want.
- The repository is public, and the description, the topics and the
  homepage are set.
- The tap repository exists, with a formula and a README.
- `HOMEBREW_TAP_TOKEN` is configured in the repository secrets.
- The README tells the truth about where the memory lives.
