# Standalone Agent-Runtime Module — Design

**Status:** Proposed (awaiting approval) · 2026-07-03
**Goal:** Extract the reusable, product-agnostic agent-runtime from `pixkb/pkg/agents` into its own Go module at `D:\weaver-sync\development\personal\projects\agents`, scaffolded via `/scaffold:go`, so it can be consumed by pixkb (and others) as a dependency — then enhance it and add docs + branding.

> Best-judgment defaults were chosen while the user was away; the one still-open question (module identity/naming sequence) is flagged in §2. Nothing is scaffolded or ported until this spec is approved (brainstorming hard-gate).

## 1. What we are (and are NOT) extracting

`pkg/agents` (~3,006 LOC, 42 files, module `pixkb`, BSD-3-Clause) splits cleanly along a reuse boundary:

| Reusable RUNTIME → the new module | pixkb-SPECIFIC → stays in pixkb |
|-----------------------------------|--------------------------------|
| `provider.go` (Provider interface), `agent.go` (Agent type), `cli.go` (CLIProvider), `providers.go` (provider registry), `session.go` (SessionPool), `agency.go` (Agency), `registry.go` (roster *mechanism*: Register/All/ByName), `monitor.go` (UsageReporter/checkLimit), `doctor.go`, `embed.go` (OpenAIEmbedder) | `roster.go` — the concrete agents (judge / scraper / normalization / quality / governance / control) **and** the injected "pixkb operating contract" |
| Vendor providers: `agy/` (Antigravity, ConPTY), `codex/` (headless exec), `claude/` (headless `claude -p`), `all/` (blank-import barrel) | the `pixkb/internal/embed` coupling (imported only by `embed.go`) |
| `host/` — multi-host plugin installer (Claude/Codex/Antigravity) | agents' reach into the KB via pixkb MCP verbs |

**Key insight:** "port all agents logic" ≠ copy everything. The whole point of "separate that component" is a **reusable runtime**. Blindly lifting `roster.go` + the pixkb operating contract + `internal/embed` would drag pixkb-specific concerns into a supposedly-generic module and make it un-reusable. So:
- The new module ships the **framework** + the three vendor providers + the host installer.
- pixkb's own agents (`roster.go`) become a **consumer**: pixkb imports the new module and registers its roster on top (`agents.Register(agents.Agent{...})`), keeping its operating-contract injection in its `register` helper.
- `embed.go`'s `internal/embed` dependency is **decoupled** — the embedder takes its model/assets via config, not a hard import.

## 2. Module identity (OPEN — best-judgment default)

- **Provisional path:** `github.com/inovacc/agents` (owner `inovacc`, matching the scaffold generator `github.com/inovacc/mantle` and sibling repos). Physical dir: `D:\weaver-sync\development\personal\projects\agents`.
- **Naming sequence (recommended, matches the command order):** scaffold + port under the provisional name now, then run `/branding:names` at the end and rename to the winner (a cheap module-path find/replace). Alternatives: brand-first (port waits) or keep `agents` permanently.
- **License:** BSD-3-Clause, **Copyright (c) 2026 inovacc** (the personal owner). NOT "Javali Holding" — that is the pixkb *source* repo's holder; the extracted standalone module is owned personally under `inovacc`, so its `LICENSE` header carries `inovacc`, not Javali Holding.

## 3. Target structure (scaffolded by `/scaffold:go`, then filled)

```
agents/                         module github.com/inovacc/agents
  go.mod (go 1.25, + github.com/UserExistsError/conpty)
  LICENSE (BSD-3)  README.md  MAINTAINERS.md
  provider.go agent.go cli.go providers.go session.go agency.go
  registry.go monitor.go doctor.go embed.go doc.go   <- core (genericised)
  agy/ codex/ claude/ all/                            <- vendor providers
  host/                                               <- multi-host installer
  internal/embed/ (or fold)                           <- decoupled default embedder assets
  <archetype from /scaffold:go: Taskfile, .golangci.yml, .goreleaser, CI>
```

The only external dependency is `github.com/UserExistsError/conpty` (agy's ConPTY driver). Doc-comments that reference "pixkb" are genericised to "the host application."

## 4. Port procedure (after scaffold, on approval)

1. `/scaffold:go` a library-archetype Go module at the target path (module `github.com/inovacc/agents`, BSD-3).
2. Copy the reusable files (§1 left column) into it; rewrite package import paths `pixkb/pkg/agents/...` → `github.com/inovacc/agents/...`.
3. Genericise: strip pixkb-specific doc/comments; remove the operating-contract injection from `registry.go`'s `register` helper (that logic returns to pixkb's consumer-side roster). Decouple `embed.go` from `pixkb/internal/embed` (accept an embed source via config; default to offline hashing as today).
4. Drop `roster.go` (pixkb-specific) from the module; provide a tiny `example_test.go` showing how a consumer registers its own agents.
5. Build + `go test ./...` green; `golangci-lint` clean; `go vet`.
6. (Follow-up, tracked separately, NOT in this module's PR) pixkb refactor: replace its in-tree `pkg/agents` with a dependency on `github.com/inovacc/agents` + a local `roster.go` that registers pixkb's agents. This keeps pixkb working and proves the extraction.

## 5. Enhancements ("enhance" + "use unravel to help")

Concrete, evidence-backed improvements (the runtime's known gaps, surfaced by prior analysis of this same codebase):
- **SessionPool robustness** (`session.go`) — add exponential backoff + an attempt cap to the reopen-on-death path (today it drops-and-reopens with no cap → can hot-loop under sustained failure).
- **Rate-limit backpressure** (`monitor.go`) — `checkLimit` is advisory-only with no synchronization; add a small queue/backpressure primitive so concurrent `RunAgent` calls honor `ErrRateLimited` coherently (relevant to warm-pool concurrency).
- **Usage persistence seam** — `LimitStatus`/usage snapshots are ephemeral; add a pluggable `UsageSink` interface so a host can persist them (pixkb → its enrich tables; others → their own).
- **agy usage/status decode — made evidence-based (this is where unravel helps):** the agy provider's `retrieveUserQuotaSummary` field mapping was *recovered from the binary, not a live response* (`agy/doc.go`). Use unravel's RE of `agy.exe` (already in unravel's KB this session — `kb_id 8c7b51c29445303a`; `unravel garble strings` / `unravel knowledge`) to verify/expand the `QuotaSummaryGroup`/`Bucket`/`UsageMetadata` field names against the binary's proto descriptors, and harden the tolerant decoder accordingly.

**How unravel assists the port itself:** coupling/dep mapping (done via `unravel`-style analysis), and — optionally — the unravel MCP tools/subagents (now connected) for a review pass over the ported tree. Go→Go extraction is not a transpile job, so `pkg/transpile` is not used.

## 6. Deliverable sequence

1. **Approve this spec** → `writing-plans` produces the implementation plan.
2. `/scaffold:go` the module (provisional `github.com/inovacc/agents`).
3. Port + genericise (§4) → green build/tests.
4. Enhance (§5).
5. `/docs:update` → README/ARCHITECTURE/MAINTAINERS for the new module.
6. `/branding:names` → name + tagline + identity; rename the module to the winner if chosen.

## 7. Non-goals

- Not porting pixkb's roster/agents or its MCP-verb/KB coupling (those stay in pixkb).
- Not refactoring pixkb to consume the module in this pass (a tracked follow-up; the module must stand alone first).
- Not reconciling lensr's divergent `pkg/agents` copy here (bacen/pixkb is canonical per the user).
