---
title: "The client lives in apps/client, not apps/web"
type: project
date: 2026-11-07
source: "smart-agents-memory-cli"
native: minimax-agent
tier: cold
native_type: reference
---

## The client lives in apps/client, not apps/web

The monorepo holds two frontends: a Flutter client in `apps/client`, and a
web client in `apps/web`. The two were confused for a long time, because the
directories suggested the opposite of what they are.

Why: the directory name is the only place that says it, so the name has to
stay precise. A rename here touches the build, the CI and the routes.
