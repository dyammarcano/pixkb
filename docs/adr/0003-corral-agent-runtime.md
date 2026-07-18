# ADR 0003 — Migrate the agent runtime to github.com/inovacc/corral

- **Status:** Accepted
- **Date:** 2026-07-05
- **Context item:** the `pkg/agents` → corral migration (specs/plans 2026-07-04)

## Context

pixkb carried a hand-maintained `pkg/agents` package: an agent-runtime core
(Agency / Provider / Session / monitor) plus vendored coding-agent CLI adapters
(claude / codex / agy). That same runtime was independently generalized and
published upstream as `github.com/inovacc/corral`. A byte-level diff confirmed
corral is pixkb's own `pkg/agents`, generalized — so pixkb was maintaining a
fork of a now-public module.

## Options

1. **Keep the local `pkg/agents` fork.** No external dependency, full control.
   But it means maintaining a runtime that already exists upstream, and every
   corral improvement has to be hand-ported.
2. **Replace `pkg/agents` with corral entirely**, keeping only the pixkb-specific
   content (the BACEN-charter agent roster, the pixkb-branded host installer) as
   pixkb-owned packages.

## Decision

**Option 2 — depend on corral, delete `pkg/agents`.** Relocate the
pixkb-specific pieces: the agent roster → `internal/roster` (registered against
corral's global registry), the host-plugin installer → `internal/agenthost`
(kept pixkb-owned specifically so the install dir stays `pixkb`, not corral's
hardcoded `corral`), and the OpenAI embedder → `internal/embed`. Mechanically
repoint every consumer's `agents.*` qualifiers to `corral.*`.

## Consequences

- **Positive:** no more forked runtime; corral is the single source of the
  Agency/Provider/Session machinery; pixkb keeps only its own domain content.
- **Negative:** a new external dependency pinned at `v0.1.1` (pre-1.0, no
  SemVer-stability guarantee), and corral transitively drags a large
  release-tooling dependency graph into the air-gap binary (BACKLOG /
  MATURITY Dependencies [D] — an upstream build-tag split is the fix).
- **Neutral:** generation still routes solely through a subscription agent
  (no metered API), preserving the air-gap posture.

## Follow-ups

- Wrap the generation seam with typed rate-limit handling (done — ADR-adjacent,
  `rag.ErrRateLimited`).
- Push for an upstream corral split so library consumers don't compile
  goreleaser / cloud SDKs (Dependencies route item).
