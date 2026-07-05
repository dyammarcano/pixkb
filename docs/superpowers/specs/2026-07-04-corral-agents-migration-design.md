# Migrate pkg/agents onto github.com/inovacc/corral — Design

<!-- rev:001 -->

## Problem

`pixkb`'s agent-runtime core (`pkg/agents`: `Agency`/`Provider`/`Session`/
`SessionPool`/rate-limit `monitor`/provider `registry`, plus vendor packages
`agy`/`codex`/`claude`/`all`) is a hand-maintained copy of infrastructure that
now exists as a standalone, published Go module: `github.com/inovacc/corral`.
Diffing the two confirms `corral` is pixkb's own `pkg/agents`, generalized:
`agency.go`, `agent.go`, `providers.go`, `session.go`, and `monitor.go` are
byte-for-byte identical to pixkb's copies (only the package name changed,
`agents`→`corral`, and a few doc comments were genericized). `corral`'s vendor
packages (`agy`, `codex`, `claude`) carry the exact same MITM-captured usage
endpoints, User-Agent strings, and pinned `openai/codex` upstream SHAs pixkb's
own copies do. `pkg/agents/doc.go` even records that pixkb's version was
itself "ported 2026-06-23 from inovacc/lensr/pkg/aihost" — `corral` is the
next link in that same lineage.

Maintaining a private fork of infrastructure that already exists upstream is
pure duplication cost: bug fixes and the two new providers `corral` ships
(`grok`, `kimi`) never reach pixkb, and pixkb's own fixes never reach
`corral`. This migration removes the fork: pixkb depends on `corral` directly
and keeps only what's genuinely pixkb-specific (the BACEN-charter agent
roster, and the pixkb-branded host-plugin installer).

## Scope decisions (confirmed with the user)

- **Full replace.** `pkg/agents/` is deleted once nothing references it — no
  compatibility shim, no side-by-side gating period. This is an internal
  implementation swap (pixkb exposes no public Go API of its own around these
  types), not a user-facing breaking change requiring the project's normal
  deprecation window.
- **3 providers only.** Wire `claude`/`codex`/`agy` (pixkb's current fleet)
  onto `corral`'s implementations. Do **not** register `corral`'s `grok`/
  `kimi` providers — that's a separate, later task if wanted.
- **Go version bump accepted.** `corral`'s `go.mod` declares `go 1.26.3`;
  pixkb's currently declares `go 1.25.0`, and the local toolchain is
  `go1.26.2`. Bump pixkb's `go.mod` to `go 1.26.3` and rely on
  `GOTOOLCHAIN=auto` (Go's default) to fetch the exact toolchain on first
  build.

## What moves where

| Today (`pkg/agents/...`) | Tomorrow | Why |
|---|---|---|
| `agency.go`, `agent.go`, `provider.go`, `session.go`, `monitor.go`, `providers.go`, `doctor.go`, `cli.go`, `doc.go` | **deleted** — replaced by `github.com/inovacc/corral` | Byte-identical to corral's copies; pure fork removal. |
| `agy/`, `codex/`, `claude/`, `all/` (vendor packages + their tests) | **deleted** — replaced by `github.com/inovacc/corral/{agy,codex,claude,all}` | Same MITM-captured endpoints, same upstream SHA pins; verified file-by-file below. |
| `roster.go` (the BACEN-charter agent definitions: control/gather/scraper/normalization/quality/governance/research/diagram/hygiene/deviation/enrich/answerer/judge, plus `domainCharter`/`pixkbContract`/the 5 JSON schemas) | **`internal/roster/roster.go`** (new package `roster`) | This is pixkb's own IP — the normative BACEN Pix/SPB content — not generic agent-runtime plumbing. Rewritten to call `corral.Agent`/`corral.Kind`/`corral.Register` instead of pixkb's own types; the system-prompt content itself is unchanged. |
| `host/host.go`, `host/hosts.go` | **`internal/agenthost/`** (new package `agenthost`) | This is pixkb-branded plugin-install logic (`installDir = "pixkb"`, writes an `.mcp.json` registering `pixkb mcp serve`). `corral`'s own `host` package hardcodes `installDir = "corral"`, which would silently rename every existing install's target directory (`~/.claude/pixkb/` → `~/.claude/corral/`) — a real behavior change this migration does not want. `hosts.go` has zero `pkg/agents` dependency (stdlib only); `host.go` imports `pixkb/pkg/agents` for exactly two symbols — `agents.Agent` (the `AgentMarkdown(a agents.Agent)` param type) and `agents.All()` (in `sharedFiles`, to enumerate the roster into per-agent `.md` files) — both become `corral.Agent`/`corral.All()`. Otherwise a verbatim move (package rename only). |
| `embed.go` (`OpenAIEmbedder`, a dev-only, air-gap-violating opt-in embedder) | **`internal/embed/openai.go`** | Used by `cmd/pixkb/config.go`'s `newEmbedder` factory (the opt-in `--embedder openai` config path: `agents.NewOpenAIEmbedder(...)`), but has no dependency on `pkg/agents`' Agency/Provider types — it only implements `internal/embed.Embedder`. It was living in `pkg/agents` for no structural reason; `internal/embed` is its natural home. `cmd/pixkb/config.go` becomes an 11th consumer to update (`agents.NewOpenAIEmbedder`→`embed.NewOpenAIEmbedder`; confirmed already imported there for the hashing embedder — see the consumer list below). |
| `agency_e2e_test.go` | **`internal/roster/roster_e2e_test.go`** | Ported to exercise `internal/roster` + `corral.Agency` end-to-end (live coding-agent CLI, guarded by provider-on-PATH + `-short`, matching the existing convention). |

## Consumers to update (10 files, confirmed via `grep -rl "pixkb/pkg/agents"`)

Every one of these swaps `pixkb/pkg/agents` (and any subpackage import) for
`github.com/inovacc/corral` (and its subpackages), and updates type/function
qualifiers (`agents.Agency`→`corral.Agency`, `agents.ErrRateLimited`→
`corral.ErrRateLimited`, `agents.NewAgency`→`corral.NewAgency`,
`agents.All`/`ByName`/`Register`→`corral.All`/`ByName`/`Register`,
`agents.ProviderByName`/`ProviderUsage`→`corral.ProviderByName`/
`ProviderUsage`, `agents.Doctor`→`corral.Doctor`). None of these files change
behavior — the API surface is identical.

- `cmd/pixkb/agents.go` — the `agents list/doctor/run/install/hosts/usage/upstream` CLI. `install`/`hosts` keep importing `internal/agenthost` (renamed from `pkg/agents/host`) unchanged; `upstream` switches to `corral/codex`'s `StatusRefs`/`CheckDrift`/`FetchCurrentSHAs`/`UpstreamMirrors` (confirmed present, matching pixkb's own `codex/upstream.go` symbol-for-symbol).
- `cmd/pixkb/ask.go` — `rag.AgentGenerator{Agency: ag}` construction; `ag, err := agents.NewAgency(...)` → `corral.NewAgency(...)`.
- `cmd/pixkb/config.go` — `newEmbedder`'s opt-in `--embedder openai` branch calls `agents.NewOpenAIEmbedder(...)`; becomes `embed.NewOpenAIEmbedder(...)` once that function relocates to `internal/embed` (see table above) — a qualifier fix, not a new import (`internal/embed` is already imported here for the default hashing embedder).
- `cmd/pixkb/curate.go` — `curate --enrich`/`--apply` Agency wiring.
- `cmd/pixkb/eval.go` — `rag-diversity`'s `agents.NewAgency` construction (this call site was flagged as inconsistent with the RAG `BundleDir` wiring during `/steps:next` item 5 — worth a look while touching this file, but out of scope for this migration unless trivial).
- `cmd/pixkb/mcp.go` — `kb_ask` tool's `Agency` dependency wiring into `kbmcp.Deps`.
- `internal/curate/fixer.go` — `Fixer`/`Enricher` interfaces dispatching through `*agents.Agency`.
- `internal/curate/e2e_test.go` — live e2e test exercising the real fleet through curate.
- `internal/kbmcp/server.go` — `Deps.Agency *agents.Agency` field.
- `internal/rag/adapters.go` — `AgentGenerator{Agency *agents.Agency}`.

## `internal/roster` package shape

```go
package roster

import "github.com/inovacc/corral"

// domainCharter, pixkbContract, and the 5 JSON schemas (judgeSchema,
// conceptSchema, enrichSchema, answerSchema, qualitySchema) move here
// verbatim — same string content, same const names.

func register(a corral.Agent) {
    a.System += domainCharter + pixkbContract
    corral.Register(func() corral.Agent { return a })
}

func init() {
    register(corral.Agent{Name: "control", Kind: corral.KindControl, ...})
    // ... all 13 agents (control, gather, scraper, normalization, quality,
    // governance, research, diagram, hygiene, deviation, enrich, answerer,
    // judge) ported verbatim — same Name/Kind/Description/Tools/Schema/System.
}
```

Every pixkb entry point that needs the roster populated blank-imports
`internal/roster` (mirroring how `pkg/agents/all` is blank-imported today) —
`cmd/pixkb/agents.go`, `cmd/pixkb/ask.go`, `cmd/pixkb/curate.go`,
`cmd/pixkb/mcp.go` gain `_ "pixkb/internal/roster"` alongside their existing
`_ "github.com/inovacc/corral/all"` (replacing `_ "pixkb/pkg/agents/all"`).

## Vendor-package parity (confirmed, not assumed)

Diffed against `corral`'s published source before writing this spec:

- **`providers.go`, `session.go`, `monitor.go`** — byte-identical to pixkb's copies (package name only differs).
- **`cli.go`** (`CLIProvider`) — corral's version adds a `PromptFlag` field (for `grok`/`kimi`'s `--single`/`--prompt` style) that pixkb's copy lacks. Confirmed inert for `claude`/`codex`: `argv()` only uses `PromptFlag` when non-empty, and neither `claude`/`codex` (in either codebase) sets it — zero behavior change for the 3 kept providers.
- **`agy/agy.go`, `agy/run_windows.go`, `agy/usage.go`** — same ConPTY mechanism, same ANSI-stripping, same ConfPTY warm-session heuristic (idle-window settle), same ~/.gemini OAuth creds path, same `daily-cloudcode-pa.googleapis.com` quota host override, same pinned `antigravity/cli/1.0.11 windows/amd64` User-Agent. This is the highest-platform-risk vendor package (Windows ConPTY); verify with a live `pixkb agents run` smoke test on Windows before deleting pixkb's copy.
- **`codex/usage.go`, `codex/upstream.go`** — same `Window`/`Usage` shapes, same `~/.codex/sessions` rollout-scanning logic, same `StatusRefs` pins (identical repo/path/BlobSHA/date/purpose for every entry checked).
- **`claude/usage.go`** — same `~/.claude/.credentials.json` OAuth path, same `api.anthropic.com` base, same `oauth-2025-04-20` beta header, same pinned `claude-code/2.0.30` User-Agent, same macOS-keychain fallback.
- **`doctor.go`** — same structure (per-check verdicts, roll-up rule), differs only in the `embedder` check's detail wording (pixkb: "hashing (offline default); agents curate pixdb"; corral: generic "no metered embedder configured (offline)"). Low-stakes cosmetic difference — accept corral's generic wording rather than wrapping `Doctor()` just to restore pixkb-specific phrasing.

## Testing

- The existing pixkb suite (`internal/curate`, `internal/rag`, `internal/kbmcp`, `cmd/pixkb`) is the primary regression net — every test there exercises the `Agency`/`Provider` contract already and must pass unchanged post-rename, since the API is identical.
- `internal/roster`: a DB-free unit test asserting all 13 agents register with the expected `Name`/`Kind`, and that `domainCharter`+`pixkbContract` are appended to every agent's `System` (port the intent of any existing `pkg/agents` roster tests, if present — check `pkg/agents/core_test.go`/`cli_model_test.go` for whether a roster-shape test already exists before writing a new one).
- `internal/agenthost`: existing `pkg/agents/host/host_test.go` ports verbatim (package rename only) — its assertions (install-path shape, dry-run planning, Doctor checks) are unaffected by the corral swap since `agenthost` has zero dependency on `pkg/agents`/`corral` core types.
- e2e (`internal/roster/roster_e2e_test.go`, ported from `pkg/agents/agency_e2e_test.go`): live coding-agent CLI through `corral.NewAgency` + `internal/roster`'s registered agents, guarded by provider-on-PATH + `-short`, paired with the MCP `concept_upsert`→search round-trip exactly as today.
- New: a `go vet`/`go build` pass immediately after the `go.mod` bump + dependency add, before any file is deleted, to confirm the toolchain fetch and dependency resolution succeed in this environment before committing to the rest of the migration.

## Risks

- **Toolchain fetch** — `go 1.26.3` must be fetchable (`GOTOOLCHAIN=auto`) in every environment that builds pixkb (dev machine, CI, any air-gapped build image). Confirmed acceptable by the user for dev; CI/air-gap images are out of this spec's verification scope (flagged, not blocking).
- **`agy` Windows ConPTY behavior** — highest-risk vendor package due to platform-specific process/PTY handling; verify live before deleting pixkb's copy.
- **`cmd/pixkb/eval.go`'s pre-existing `HybridRetriever{Store, Emb}` construction missing `BundleDir`** (noted during `/steps:next` item 5) is unrelated to this migration and should not be conflated with it — leave as its own follow-up unless the file is being touched anyway and the fix is trivial.

## Out of scope

- Registering `corral`'s `grok`/`kimi` providers (deferred per the scope decision above).
- Any change to the BACEN-charter content itself (system prompts, schemas) — this is a structural move, not a content edit.
- CI/deployment-image toolchain provisioning for `go 1.26.3`.
