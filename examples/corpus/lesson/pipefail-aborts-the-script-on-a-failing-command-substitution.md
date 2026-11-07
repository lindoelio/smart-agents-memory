---
title: "pipefail aborts the script on a failing command substitution"
type: lesson
date: 2026-11-07
source: "smart-agents-memory-cli"
native: minimax-agent
tier: cold
native_type: reference
---

## pipefail aborts the script on a failing command substitution

Under `set -euo pipefail`, a pipeline that fails inside `$(...)` aborts the
whole script, even when the caller only wants an empty value. Wrap the
tolerant read in `{ cmd || true; }`, or handle the status explicitly.

Why: a "best effort" read has to be explicitly tolerant. A metadata service
that was briefly unavailable took down the entire authentication script.
