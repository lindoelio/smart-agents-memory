#!/bin/sh
# Installs Smart Agents Memory from a clone of the repository.
#
# The release channel is Homebrew and the GitHub releases. This script is
# for the machine that wants to build from source.
#
# Usage:
#   scripts/install.sh [--memory-home <path>]
set -eu

root="$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)"
bin_dir="${HOME}/.local/bin"
memory_home=""

while [ $# -gt 0 ]; do
    case "$1" in
        --memory-home)
            shift
            memory_home="${1:?--memory-home needs a path}"
            ;;
        -h|--help)
            sed -n '2,10p' "$0" | sed 's/^# \{0,1\}//'
            exit 0
            ;;
        *)
            echo "unknown option: $1" >&2
            exit 2
            ;;
    esac
    shift
done

echo "==> Building the binary"
cd "$root"
go build -trimpath -ldflags "-s -w" -o "$bin_dir/smart-agents-memory" ./cmd/smart-agents-memory

echo "==> Installed $("$bin_dir/smart-agents-memory" version) in $bin_dir"
echo

if [ -n "$memory_home" ]; then
    export SMART_AGENTS_MEMORY_HOME="$memory_home"
fi

echo "==> Linking the tools to the store"
"$bin_dir/smart-agents-memory" install

echo
echo "==> Done. Check the installation with:"
echo "    smart-agents-memory doctor"
