#!/bin/sh
# Rewrites the Homebrew formula of the tap and pushes it.
#
# The tap is a separate repository: a Homebrew tap has to be named
# homebrew-<something>, and the release pipeline owns the formula, so nobody
# edits it by hand.
#
# Environment:
#   HOMEBREW_TAP_REPOSITORY   the tap, as owner/name
#   HOMEBREW_TAP_TOKEN        a token with write access to the tap
set -eu

VERSION="${1:?usage: update-homebrew.sh <version> <assets-dir>}"
ASSETS="${2:?usage: update-homebrew.sh <version> <assets-dir>}"

TAP_REPOSITORY="${HOMEBREW_TAP_REPOSITORY:-lindoelio/homebrew-smart-agents-memory}"
TAP_TOKEN="${HOMEBREW_TAP_TOKEN:?HOMEBREW_TAP_TOKEN is required}"

REPO="lindoelio/smart-agents-memory"
WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

sha_of() {
    if command -v sha256sum >/dev/null 2>&1; then
        sha256sum "$1" | awk '{print $1}'
    else
        shasum -a 256 "$1" | awk '{print $1}'
    fi
}

asset_sha() {
    if [ ! -f "$ASSETS/$1" ]; then
        echo "missing asset: $1" >&2
        exit 1
    fi
    sha_of "$ASSETS/$1"
}

FORMULA="$WORK/smart-agents-memory.rb"
cat > "$FORMULA" <<EOF
class SmartAgentsMemory < Formula
  desc "Shared memory for AI agents: one corpus, many harnesses."
  homepage "https://github.com/$REPO"
  license "MIT"
  version "$VERSION"

  on_macos do
    on_arm do
      url "https://github.com/$REPO/releases/download/v$VERSION/smart-agents-memory-darwin-arm64"
      sha256 "$(asset_sha smart-agents-memory-darwin-arm64)"
    end

    on_intel do
      url "https://github.com/$REPO/releases/download/v$VERSION/smart-agents-memory-darwin-amd64"
      sha256 "$(asset_sha smart-agents-memory-darwin-amd64)"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/$REPO/releases/download/v$VERSION/smart-agents-memory-linux-arm64"
      sha256 "$(asset_sha smart-agents-memory-linux-arm64)"
    end

    on_intel do
      url "https://github.com/$REPO/releases/download/v$VERSION/smart-agents-memory-linux-amd64"
      sha256 "$(asset_sha smart-agents-memory-linux-amd64)"
    end
  end

  on_windows do
    url "https://github.com/$REPO/releases/download/v$VERSION/smart-agents-memory-windows-amd64.exe"
    sha256 "$(asset_sha smart-agents-memory-windows-amd64.exe)"
  end

  def install
    bin.install Dir["smart-agents-memory-*"].first => "smart-agents-memory"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/smart-agents-memory --version")
  end
end
EOF

echo "==> Updating the formula of $TAP_REPOSITORY"

git clone --depth 1 "https://x-access-token:$HOMEBREW_TAP_TOKEN@github.com/$TAP_REPOSITORY.git" "$WORK/tap"
cp "$FORMULA" "$WORK/tap/Formula/smart-agents-memory.rb"

cd "$WORK/tap"
git config user.name "github-actions[bot]"
git config user.email "41898282+github-actions[bot]@users.noreply.github.com"
git add Formula/smart-agents-memory.rb
if git diff --cached --quiet; then
    echo "The formula is already up to date."
else
    git commit -m "smart-agents-memory $VERSION"
    git push origin HEAD
fi

echo "==> The formula of $TAP_REPOSITORY now points to v$VERSION"
