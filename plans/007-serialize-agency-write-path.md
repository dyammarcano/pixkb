# Plan 007: Serialize the epoch/bundle/git write path against concurrent callers

> **Executor instructions**: Follow step by step; run every verify command. On any STOP
> condition, stop and report. Update this plan's row in `plans/README.md` when done.
>
> **Drift check (run first)**: `git diff --stat b2d1fe6..HEAD -- internal/epoch/runner.go internal/kbmcp/server.go`

## Status

- **Priority**: P2
- **Effort**: S–M
- **Risk**: LOW
- **Depends on**: none
- **Category**: bug (concurrency)
- **Planned at**: commit `b2d1fe6`, 2026-07-18

## Why this matters

`epoch.Runner`'s write entry points — `Run`, `UpsertBatch`, `Reindex` — allocate an epoch,
write the bundle, append `log.md`, and git-commit, with **no serialization**. The MCP server
holds one `Runner` for its lifetime and dispatches tool handlers concurrently, so two
`concept_upsert` calls (or one racing an ingest) can: (a) both compute
`coalesce(max(n)+1,0)` and one fails the epoch PRIMARY KEY (the store's own doc,
`internal/store/postgres/epoch.go:14-19`, states "the caller is responsible for serialising
epoch creation"); (b) interleave `log.md` appends; (c) corrupt the shared go-git worktree
mid-stage (`AddGlob(".")` + `Commit`). This is the crown-jewel write path; the store
explicitly delegates serialization to the caller, and the caller provides none.

## Current state

- `internal/epoch/runner.go:16-21` — `Runner` struct (no mutex):
  ```go
  type Runner struct {
      Bundle string
      Store  *postgres.Store
      Emb    embed.Embedder
      Git    Committer
  }
  ```
- Three write entry points, each doing `NextEpoch → applyConcept loop → WriteIndexes →
  AppendLog → Git.Commit → SetEpochCommit → PruneEmbeddings` (Reindex omits commit/log):
  `Run` (`runner.go:43`), `UpsertBatch` (`runner.go:109`), `Reindex` (`runner.go:216`).
- `internal/kbmcp/server.go:384` — `concept_upsert` handler calls `d.Runner.UpsertBatch`
  directly; `:429` — `reindex` calls `d.Runner.Reindex`. MCP handlers run concurrently.
- The store's warning: `internal/store/postgres/epoch.go:14-19`.

Conventions: standard library `sync`; keep the mutex unexported; errors wrapped with `%w`.

## Commands you will need

| Purpose | Command | Expected |
|---------|---------|----------|
| Build | `go build ./...` | exit 0 |
| Vet (race-aware) | `go vet ./internal/epoch/` | exit 0 |
| Test with race detector | `go test -race ./internal/epoch/ -count=1` | ok (integration subtests skip without `PIXKB_TEST_DSN`) |
| Lint | `golangci-lint run ./internal/epoch/ ./internal/kbmcp/` | `0 issues.` |

## Scope

**In scope:**
- `internal/epoch/runner.go` (add an unexported `sync.Mutex` to `Runner`; lock the three write
  entry points)
- `internal/epoch/runner_test.go` (a concurrency test under `-race`)

**Out of scope:**
- `internal/kbmcp/server.go` — no change needed if the lock lives in `Runner` (preferred, so
  every caller is protected, not just MCP). Do NOT scatter locking into handlers.
- The documented cross-system non-atomicity of `Run` (`runner.go:36-42`) — unchanged; this plan
  serializes concurrent callers, it does NOT add cross-system transactions.
- `applyConcept` and the store methods.

## Git workflow

- Branch: `advisor/007-serialize-agency-write-path` (or the batch branch the operator names)
- Commit conventional: `fix(epoch): serialize the Runner write path (epoch/bundle/git)`. No AI attribution.
- Do NOT push.

## Steps

### Step 1: Add the mutex

Add an unexported field to `Runner`: `mu sync.Mutex` (import `sync`). Because `Runner` is
constructed as a struct literal in several places (search `epoch.Runner{`), a zero-value mutex
is correct — no constructor change needed.

**Verify**: `go build ./...` → exit 0

### Step 2: Guard the three write entry points

At the very top of `Run`, `UpsertBatch`, and `Reindex`, add `r.mu.Lock(); defer r.mu.Unlock()`.
This serializes epoch allocation + bundle write + git commit across concurrent callers while
preserving each method's existing logic. Keep the lock coarse (whole method) — these are not
hot-path high-QPS calls, and correctness beats granularity here.

**Verify**: `go build ./...` → exit 0; `go vet ./internal/epoch/` → exit 0

### Step 3: Concurrency test

Add a `-race` test that runs two `UpsertBatch` calls concurrently against a fake/in-memory
store (or the DB-gated integration store if that is the only `Store`) and asserts no race and
no error. If `Runner` requires a real `*postgres.Store` (not an interface), gate the test on
`PIXKB_TEST_DSN` like the existing integration tests and assert both concurrent upserts get
distinct epochs. If a fake store is feasible, prefer the DB-free version.

**Verify**: `go test -race ./internal/epoch/ -count=1` → ok

## Test plan

- **No race under concurrent writes** (`runner_test.go`): launch N goroutines each calling
  `UpsertBatch` with one concept; wait; assert every call returned nil error and the epochs are
  distinct (no PK collision). Run under `-race`. Model after
  `internal/epoch/ingest_integration_test.go` for the DB-gated variant; skip cleanly when
  `PIXKB_TEST_DSN` is unset.

**Verification**: `go test -race ./internal/epoch/ -count=1` → all pass (integration subtests
skip without a DSN).

## Done criteria

- [ ] `go build ./...` exits 0; `go vet ./internal/epoch/` clean
- [ ] `Runner` has an unexported mutex; `Run`/`UpsertBatch`/`Reindex` each lock it
- [ ] `go test -race ./internal/epoch/ -count=1` passes (with the new concurrency test)
- [ ] `golangci-lint run ./internal/epoch/ ./internal/kbmcp/` → `0 issues.`
- [ ] No locking logic was added to `internal/kbmcp/server.go`
- [ ] `plans/README.md` status row updated

## STOP conditions

- Excerpts don't match live code (drift).
- `Runner` is copied by value anywhere (a `sync.Mutex` must not be copied) — search for
  `epoch.Runner{` and any function taking `Runner` (not `*Runner`); if a value copy exists,
  STOP and report (the fix is to pass `*Runner`, which may be out of this plan's scope).
- The only available `Store` forces a live DB for any test and no DSN is configured — implement
  Steps 1-2 (verifiable by `go vet`/`-race` build) and report the test gap.

## Maintenance notes

- If a second process (not just goroutines) can write the same bundle/DB concurrently, this
  in-process mutex is insufficient — a Postgres advisory lock around `NextEpoch` would be the
  next layer. Note it for whoever adds multi-process writers.
- Reviewer: confirm `Runner` is always used as `*Runner` (never copied), and the lock wraps the
  whole method including `NextEpoch`.
