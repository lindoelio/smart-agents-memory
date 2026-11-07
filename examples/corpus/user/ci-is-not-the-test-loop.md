---
title: "CI confirms, it does not find the bug"
type: user
date: 2026-11-07
source: "smart-agents-memory-cli"
native: minimax-user
tier: hot
native_type: user
---

## CI confirms, it does not find the bug

Test and fix locally; only push green. A green pipeline is evidence that the
change did not break what was already covered — it is not evidence that the
change is correct.

Why: a pipeline that fails for a reason nobody can reproduce costs more time
than the bug it was meant to catch.
