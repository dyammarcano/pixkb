# Plan 006: Typed rate-limit handling at the corral generation seam

> **Executor instructions**: Follow step by step; run every verify command. On any STOP
> condition, stop and report. Update this plan's row in `plans/README.md` when done.
>
> **Drift check (run first)**: `git diff --stat b2d1fe6..HEAD -- internal/rag/adapters.go cmd/pixkb/ask.go cmd/pixkb/mcp.go`

## Status

- **Priority**: P2
- **Effort**: S
- **Risk**: LOW
- **Depends on**: none
- **Category**: bug (resilience)
- **Planned at**: commit `b2d1fe6`, 2026-07-18

## Why this matters

`AgentGenerator.Generate` (the seam between pixkb's RAG layer and the corral agent fleet)
returns the agency error **raw**. For a subscription-agent fleet, the expected failure is a
rate limit — corral signals it with the sentinel `corral.ErrRateLimited` ("run was withheld
because usage reached the threshold"). Today a caller cannot distinguish that from a real
failure, so surfaces show an opaque string and cannot say "try again later." This plan makes
the rate-limit condition a typed, recognizable error and lets surfaces present it clearly. It
also short-circuits proactively when the provider is already exhausted, to avoid spending a
doomed attempt.

NOTE (scope discipline): corral's `LimitStatus` exposes **no reset time**, and
`ErrRateLimited` means the run was withheld — so an automatic backoff-retry loop is NOT
included (it would spin without a known reset). Typed propagation + a friendly message is the
honest maturity improvement; timed retry is deferred until corral exposes a reset window.

## Current state

- `internal/rag/adapters.go:99-110` — the seam:
  ```go
  type AgentGenerator struct{ Agency *corral.Agency }
  func (a AgentGenerator) Generate(ctx context.Context, prompt string) (string, error) {
      res, err := a.Agency.Run(ctx, "answerer", prompt)
      if err != nil {
          return "", err   // raw — caller can't tell a rate limit from a crash
      }
      return res.Text, nil
  }
  ```
- corral API (verified via `go doc github.com/inovacc/corral`):
  - `var ErrRateLimited = errors.New("agents: rate-limit threshold reached")` — `errors.Is`-friendly.
  - `func (a *Agency) Run(ctx, agentName, input string) (RunResult, error)`
  - `func (a *Agency) LimitStatus() (status *LimitStatus, supported bool, err error)`
  - `func (s *LimitStatus) Exhausted() bool`
- Surfaces that call the generator: `cmd/pixkb/ask.go:64` (`rag.Ask(... AgentGenerator{Agency: ag} ...)`),
  `cmd/pixkb/mcp.go:76` (the `kb_ask` MCP path), `cmd/pixkb/eval.go:367`.

Conventions: sentinel errors declared at package scope with `errors.New`; wrap with
`fmt.Errorf("...: %w", err)`; the RAG generator is behind the `rag.Generator` interface, so
unit tests inject a fake `Generator` — a fake can return the new sentinel to test surfaces.

## Commands you will need

| Purpose | Command | Expected |
|---------|---------|----------|
| Build | `go build ./...` | exit 0 |
| Test rag | `go test ./internal/rag/ -count=1` | ok |
| Test cmd | `go test ./cmd/pixkb/ -count=1` | ok |
| Lint | `golangci-lint run ./internal/rag/ ./cmd/pixkb/` | `0 issues.` |

## Scope

**In scope:**
- `internal/rag/adapters.go` (add `rag.ErrRateLimited`, map corral's sentinel, proactive check)
- `internal/rag/adapters_test.go` or a new `internal/rag/generator_test.go` (tests via a fake)
- `cmd/pixkb/ask.go` and `cmd/pixkb/mcp.go` (present a clear message on `rag.ErrRateLimited`)

**Out of scope:** any change to corral; automatic timed retry/backoff (deferred, see note);
the answerer prompt; `eval.go` (its failures are batch-tolerated — leave as-is).

## Git workflow

- Branch: `advisor/006-corral-generate-resilience` (or the batch branch the operator names)
- Commit conventional: `fix(rag): typed rate-limit error at the corral generation seam`. No AI attribution.
- Do NOT push.

## Steps

### Step 1: Declare `rag.ErrRateLimited` and map corral's sentinel

In `adapters.go`, add a package sentinel `var ErrRateLimited = errors.New("rag: agent fleet
rate-limited")`. In `Generate`, after `Run`, if `errors.Is(err, corral.ErrRateLimited)` return
`fmt.Errorf("%w", ErrRateLimited)` (or wrap with context). Optionally, before `Run`, call
`a.Agency.LimitStatus()`; if `supported && status.Exhausted()`, return `ErrRateLimited` without
spending the attempt (ignore the `LimitStatus` error — a status probe failure must not block a
normal answer; fall through to `Run`).

**Verify**: `go build ./...` → exit 0

### Step 2: Friendly surface messages

In `ask.go` (CLI) and `mcp.go` (`kb_ask`), when `errors.Is(err, rag.ErrRateLimited)`, present a
clear message ("the agent fleet is rate-limited; try again later") instead of the raw error.
For the CLI, print to stderr and exit non-zero; for MCP, return the typed message in the tool
error. Keep it minimal — one branch each.

**Verify**: `go build ./...` → exit 0

### Step 3: Tests

Add tests (see Test plan) and run them.

**Verify**: `go test ./internal/rag/ ./cmd/pixkb/ -count=1` → ok

## Test plan

- **Generate maps corral's sentinel** (`internal/rag`): this needs a real `*corral.Agency`,
  which a unit test cannot easily construct. Instead test the *mapping helper*: refactor the
  `errors.Is(err, corral.ErrRateLimited)` → `rag.ErrRateLimited` mapping into a small unexported
  function `mapGenErr(error) error` and unit-test it with a fabricated
  `fmt.Errorf("%w", corral.ErrRateLimited)` input, asserting `errors.Is(out, ErrRateLimited)`;
  and that an unrelated error passes through unchanged.
- **Surface message** (`cmd/pixkb`): the surfaces call `rag.Ask` with a `Generator`; inject a
  fake `Generator` that returns `rag.ErrRateLimited` and assert the CLI/MCP path surfaces the
  friendly message (or the typed error). Model after existing `cmd/pixkb/ask_test.go`.

**Verification**: `go test ./internal/rag/ ./cmd/pixkb/ -count=1` → all pass with new tests.

## Done criteria

- [ ] `go build ./...` exits 0
- [ ] `go test ./internal/rag/ ./cmd/pixkb/ -count=1` passes with new tests
- [ ] `golangci-lint run ./internal/rag/ ./cmd/pixkb/` → `0 issues.`
- [ ] `errors.Is(err, rag.ErrRateLimited)` is true when corral withholds a run
- [ ] CLI + MCP present a clear rate-limit message, not a raw error string
- [ ] No automatic retry loop was added (deferred per the note)
- [ ] `plans/README.md` status row updated

## STOP conditions

- Excerpts don't match live code (drift), or `corral.ErrRateLimited`/`Agency.LimitStatus` are
  not present at the versions above (`go doc github.com/inovacc/corral` to confirm).
- Mapping cannot be isolated for a unit test without constructing a real Agency — implement
  Steps 1-2 + the surface test, and report the mapping-test gap rather than building an Agency mock.

## Maintenance notes

- If corral later exposes a rate-limit reset time, a bounded backoff-retry can be layered on
  top of this typed error — that is the deferred follow-up.
- Reviewer: confirm the `LimitStatus` probe error is swallowed (never blocks a normal answer)
  and that the sentinel is matched with `errors.Is`, not string compare.
