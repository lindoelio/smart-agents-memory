#!/bin/sh
# Prints the declared version of the product.
#
# The version has a single source: internal/version/version.go. The release
# pipeline compares it with the git tag, and refuses to publish when the two
# disagree.
set -eu

root="$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)"

sed -n 's/^const Version = "\(.*\)"$/\1/p' "$root/internal/version/version.go"
