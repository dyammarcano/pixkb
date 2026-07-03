# `pkg/agents` — the pixkb agent host
<!-- rev:001 -->

> Documentation convention ported 2026-06-23 from
> [inovacc/lensr/pkg/aihost](https://github.com/inovacc/lensr) (BSD-3): minimal
> interface + optional capabilities by type assertion, lazy-factory registry,
> `doc.go` provenance lines, and this README/MAINTAINERS pair.

## What it does (one paragraph)

`pkg/agents` runs a roster of declarative agents (gather, scraper,
normalization, quality, governance, research, judge, control) through a
pluggable **Provider** backend, keeping backends warm via a `SessionPool`. Each
backend is a subscription coding agent in its **own vendor package** so the
products stay cleanly separated. The agents reach the KB only through pixkb's
MCP verbs; `pkg/agents/host` packages the roster + an `.mcp.json` into an
installable plugin tree per host.

## Vendor packages (one product each)

| Package | Product | Mechanism |
|---|---|---|
| `agy/` | **Google Antigravity** | ConPTY pseudo-console (no headless mode); warm Session |
| `codex/` | **OpenAI Codex** | headless `codex exec` (native `--output-schema`) + rate-limit usage tracking + upstream drift pinning |
| `claude/` | **Anthropic Claude Code** | headless `claude -p` |
| `all/` | — | blank-imports the three so they register |
| `host/` | — | multi-host plugin installer (Claude/Codex/Antigravity) |

## Core types (in this package)

- **`Provider`** (`provider.go`) — `Name()` + `Run(ctx, RunRequest) (RunResult, error)`. Optional `SessionOpener` (warm sessions) by type assertion.
- **`Agent`** (`agent.go`) — declarative: Name, Kind, Description, Model, Tools, System prompt, Schema.
- **Provider registry** (`providers.go`) — `RegisterProvider(factory, names...)` (called from each vendor `init()`); `ProviderByName(name)`.
- **Agent roster** (`registry.go` + `roster.go`) — `Register/All/ByName`; every agent carries the pixkb operating contract.
- **`SessionPool`** (`session.go`) — one warm session per agent, reused across turns, reopened on death.
- **`Agency`** (`agency.go`) — pairs a Provider with the roster; warm when the provider supports it.
- **`CLIProvider`** (`cli.go`) — shared headless-CLI mechanism the codex/claude presets build on.
- **`OpenAIEmbedder`** (`embed.go`) — optional metered embedder (NOT the default; hashing is).
- **`Doctor`** (`doctor.go`) — CLI/embedder/roster health checks.

## How to ADD a provider (the common task)

1. Create `pkg/agents/<vendor>/<vendor>.go`, `package <vendor>`.
2. Return a `*agents.CLIProvider` preset (or a custom type implementing `agents.Provider`).
3. Register in `init()`: `agents.RegisterProvider(func() agents.Provider { return New() }, "<name>", "<alias>")`.
4. Add a blank import to `pkg/agents/all/all.go`.

## How to ADD an agent

Add a `register(Agent{...})` call in `roster.go` (the `register` helper appends
the pixkb operating contract). Give it a Kind, a tight System prompt, and a
Schema when it must emit structured output.

## Invariants / gotchas

- **Core never imports the vendor packages** — they self-register. Import
  `pkg/agents/all` wherever `ProviderByName` is used, or it errors "provider not
  registered".
- **Vendor ≠ host name.** `agy` is Antigravity, not "the agency".
- The OpenAI embedder is opt-in; embeddings default to offline hashing.
- Codex usage/upstream tracking lives in `codex/` (provider-specific), not core.

## File map

```
pkg/agents/
  doc.go provider.go agent.go cli.go providers.go session.go agency.go
  registry.go roster.go doctor.go embed.go   <- package agents (core)
  agy/    codex/    claude/    all/    host/  <- vendor + barrel + installer
```
