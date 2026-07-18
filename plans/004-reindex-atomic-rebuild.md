# Plan 004: `Reindex` cannot leave the index empty on failure

> **Executor instructions**: Follow step by step; run every verify command. On any STOP
> condition, stop and report. Update this plan's row in `plans/README.md` when done. This
> plan touches the store's write/transaction path — read it fully before starting.
>
> **Drift check (run first)**: `git diff --stat b4e7632..HEAD -- internal/epoch/runner.go internal/store/postgres/maintenance.go`

## Status

- **Priority**: P2
- **Effort**: M
- **Risk**: MED (touches TRUNCATE + rebuild transaction semantics)
- **Depends on**: none (but a fake-store test from a future TEST plan would strengthen it)
- **Category**: bug (availability)
- **Planned at**: commit `b4e7632`, 2026-07-18

## Why this matters

`Reindex` is documented as the canonical recovery path after any interrupted `Run`, but it
`TRUNCATE`s the whole index up front and then reads the bundle and replays concepts
one-by-one, none of it transactional. If the bundle read fails, the embedder errors
mid-replay, or the process is killed, the KB is left empty or partially populated with no
rollback — searches return nothing until a second full `Reindex` succeeds. The recovery
tool is itself the biggest availability hazard. Note the ordering: it truncates *before* it
even reads the bundle, so a bad bundle wipes a healthy index.

## Current state

- `internal/epoch/runner.go:216-238` — `Reindex`:
  ```go
  func (r *Runner) Reindex(ctx context.Context) error {
      if err := r.Store.Truncate(ctx); err != nil { ... }      // wipes first
      concepts, err := okf.ReadBundle(r.Bundle)                // read AFTER wipe
      if err != nil { ... }
      n, createdAt, err := r.Store.NextEpoch(ctx, "reindex", "", len(concepts), 0, 0)
      ...
      for _, c := range concepts { c.Epoch = n; r.applyConcept(ctx, c, createdAt) }
      r.Store.PruneEmbeddings(ctx)
      return nil
  }
  ```
- `internal/store/postgres/maintenance.go:11-17` — `Truncate` = `TRUNCATE concept,
  embedding, epoch, edge, concept_fact … CASCADE`.
- `applyConcept` (`runner.go:147-168`) does per-concept UpsertConcept/embed/UpsertEmbedding/
  ReplaceEdges/RecordFact against `r.Store` (a `*postgres.Store` wrapping a pgx pool).

Repo conventions: errors wrapped `fmt.Errorf("...: %w", err)`; the bundle is the source of
truth and the index is explicitly rebuildable/non-atomic across systems (`runner.go:36-42`)
— this plan does NOT change that contract, only makes the *destructive* step safe.

## Commands you will need

| Purpose | Command | Expected |
|---------|---------|----------|
| Build | `go build ./...` | exit 0 |
| Vet | `go vet ./internal/epoch/ ./internal/store/postgres/` | exit 0 |
| Integration test (needs throwaway DB) | `task testdb:up` then `go test ./internal/epoch/ -run Reindex -count=1 -p 1` | ok (or skipped if no DSN) |
| Lint | `golangci-lint run ./internal/epoch/ ./internal/store/postgres/` | `0 issues.` |

The integration test needs `PIXKB_TEST_DSN` pointing at a **throwaway** DB (never prod).
`task testdb:up` starts a local one at `localhost:5433`; set
`PIXKB_TEST_DSN=postgres://pixkb:pixkb@localhost:5433/pixkb?sslmode=disable`.

## Scope

**In scope:**
- `internal/epoch/runner.go` (`Reindex` ordering + failure safety)
- `internal/store/postgres/maintenance.go` (only if a "truncate within the rebuild tx" or a
  swap helper is the chosen approach)
- `internal/epoch/runner_test.go` and/or `internal/epoch/ingest_integration_test.go` (add
  the failure-safety test)

**Out of scope:** `Run`/`UpsertBatch`; the cross-system non-atomicity of the normal ingest
path; the git-commit sequence.

## Git workflow

- Branch: `advisor/004-reindex-atomic-rebuild`
- Commit(s) conventional: `fix(epoch): make Reindex failure-safe (read bundle before truncate)`. No AI attribution.
- Do NOT push.

## Steps

### Step 1 (minimum viable safety): read the bundle BEFORE truncating

Reorder `Reindex` so `okf.ReadBundle` runs first; only `Truncate` after a successful read.
This alone removes the most likely failure (a bad/missing bundle wiping a healthy index).

**Verify**: `go build ./...` → exit 0

### Step 2 (preferred, if feasible): rebuild-then-swap or single-transaction

If the store exposes (or can cheaply expose) a way to rebuild into staging tables and swap,
or to run truncate+repopulate inside one transaction, prefer that so a mid-replay failure
leaves the prior index intact. If a full staging-swap is too large for this plan's effort
budget, STOP after Step 1 and record Step 2 as a follow-up in `plans/README.md` — do not
half-build a shadow-table mechanism. A single wrapping transaction is only acceptable if
`applyConcept` can run on a tx handle without deadlocking against `Truncate`; verify against
the throwaway DB before committing.

**Verify**: integration test (below) green against the throwaway DB.

### Step 3: Failure-safety test

Add a test that injects a failure and asserts the prior index survives (see Test plan).

**Verify**: `go test ./internal/epoch/ -run Reindex -count=1 -p 1` → ok

## Test plan

- **Bad bundle does not wipe the index** (unit or integration): point `Reindex` at a bundle
  dir that `ReadBundle` will reject; assert `Reindex` errors AND (integration) a prior
  `search`/`CountConcepts` still returns the pre-existing rows. If a fake store is available
  (see whether `Runner` can take an interface), a unit test that asserts `Truncate` is NOT
  called when `ReadBundle` fails is the cheapest version — prefer it.
- Model after `internal/epoch/ingest_integration_test.go` for the DB-gated variant.

**Verification**: `go test ./internal/epoch/ -count=1 -p 1` → all pass (integration
sub-tests skip cleanly when `PIXKB_TEST_DSN` is unset).

## Done criteria

- [ ] `go build ./...` exits 0; `go vet` clean
- [ ] `Reindex` reads the bundle before any `Truncate`
- [ ] A bad-bundle test proves the index is not wiped on read failure
- [ ] `golangci-lint run ./internal/epoch/ ./internal/store/postgres/` → `0 issues.`
- [ ] If Step 2 was deferred, `plans/README.md` records it as a follow-up
- [ ] `plans/README.md` status row updated

## STOP conditions

- Excerpts don't match live code (drift).
- Step 2's single-transaction approach deadlocks or requires reworking `applyConcept`'s
  signature — fall back to Step 1 + defer Step 2, report.
- No throwaway DB is available and the chosen approach needs one to verify — implement Step 1
  (verifiable without a DB via a fake-store or a bad-bundle unit test) and report the
  integration gap.

## Maintenance notes

- Never reintroduce a `Truncate` that precedes the bundle read.
- Reviewer: confirm no destructive call happens before the bundle is known-good, and that the
  normal-ingest non-atomicity contract (`runner.go:36-42`) is unchanged.
