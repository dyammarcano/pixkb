# Code Review — branch `build/pixkb` vs `ef7d653`

Scope: real bugs only (SQL injection, races, error-swallowing, resource leaks,
bitemporal correctness, nil derefs, context/pgx misuse). Style nits skipped.
Point-in-time record; not a living doc.

## CRITICAL

_None found._ No SQL injection: every dynamic clause uses `$N` placeholders and
only column-name constants are interpolated (`currentTxPred` takes a fixed
`"cf.tx"`/`"tx"` literal, never user input). No raw string concatenation of
values into queries.

## HIGH

- **internal/store/postgres/fact.go:31-36 — `RecordFact` never closes the prior
  `valid` range.** Each epoch closes only the prior *tx* window, then inserts a
  new row with `valid = [validFrom, ∞)`. Every historical version therefore
  keeps an open-ended `valid` range, so true valid-time history is unrecoverable:
  a fact corrected at epoch N still reads as "valid forever" in the bundle's
  valid-time dimension. `AsOfTime` happens to return the right *single* row only
  because it also filters on open-tx, masking the defect.
  Fix: before insert, also close the prior open-tx row's valid range
  (`SET valid = tstzrange(lower(valid), $validFrom)`), or document that `valid`
  is intentionally always `[from,∞)` and drop the valid-time travel claim.

- **internal/store/postgres/fact.go:98 (caller internal/epoch/runner.go:98) —
  `RecordFact(ctx, c, at, at)` uses one identical `createdAt` for both the close
  bound and the new `[at,∞)` open bound.** The prior row's tx becomes
  `tstzrange(lower, at)`; if two facts for the same id land in the same epoch (or
  a reindex replays at a single `createdAt`), the close bound equals the prior
  row's lower bound, producing an empty tx range that silently drops the row from
  every `currentTxPred` query and can also trip the `EXCLUDE (id WITH =, tx WITH
  &&)` constraint on identical timestamps. Fix: derive `txFrom` from a
  monotonically increasing per-write `now()` (or `clock_timestamp()` inside the
  same tx), not the shared epoch timestamp.

## MEDIUM

- **internal/store/postgres/asof.go:30-34 / search.go:136-140 — `AsOfEpoch`
  ignores the tx window.** `DISTINCT ON (cf.id) ... epoch <= N ORDER BY epoch
  DESC` returns the latest version at/before N even if that version was a *delete
  marker* or was superseded by a tx correction. A concept removed at epoch N
  still appears in the epoch-N snapshot and in `Diff`. The doc comment claims
  this is intentional ("regardless of whether its tx is open or closed"), but it
  makes transaction-time corrections invisible to epoch-based travel. Confirm
  this is desired; otherwise add a tx-as-of bound.

- **internal/epoch/runner.go:35-78 — `Run` is not atomic across Postgres + git +
  bundle.** `NextEpoch` commits the epoch row, then per-concept writes, indexes,
  log, and `Git.Commit` run as independent operations. A failure after
  `NextEpoch` (e.g. embed error at runner.go:88) leaves an epoch row with no
  facts/commit and the on-disk bundle partially written, with no rollback. Fix:
  wrap the index mutations in one pgx tx, or make `Reindex` the documented
  recovery path and note the non-atomicity.

## LOW

- **cmd/pixkb/ops.go:178 — `err != http.ErrServerClosed`** uses `!=` instead of
  `errors.Is`; a wrapped `ErrServerClosed` would be misreported as a real error.
  Fix: `errors.Is(err, http.ErrServerClosed)` (CLAUDE.md error-comparison rule).

- **cmd/pixkb/ops.go:46-65 — `watch` callback swallows every error** (gather/run
  failures only `Fprintln` and return nil). This is documented ("a bad drop must
  not kill the daemon"), but failures are invisible to any non-stdout consumer.
  Fix: emit via `slog` at warn level so the offline daemon leaves an audit trail.

- **internal/store/postgres/store.go:57-60 — `GuardDim` is defined but never
  wired into `Open`/ingest.** The doc on schema/0001 says `Store.Open` guards the
  dim; it does not. Mixing a 256-dim and 384-dim embedder against the same DB is
  currently unguarded until a caller invokes `GuardDim` explicitly. Fix: call it
  during ingest/open, or update the comment.

- **internal/ingest/pdf.go:59-69 — `extractPDFText` aborts the whole file on the
  first `GetPlainText` error.** One malformed page kills ingestion of an entire
  manual. Fix: log and skip the page (continue) rather than returning.

## Notes (verified clean)

- All `rows.Query` paths (`asof.go`, `search.go`, `vector.go`, `shas.go`) defer
  `rows.Close()` and check `rows.Err()` after iteration — no row/cursor leaks.
- All `Begin` paths (`fact.go`, `search.go ReplaceEdges`) defer `tx.Rollback`
  and commit explicitly — no connection leaks.
- `tarDir` (ops.go) closes every opened file and caps the copy with
  `io.LimitReader(fh, hdr.Size)` — no "write too long" and no fd leak.
- `onnx.go` embedder serializes the shared session with `mu`, destroys all
  per-call tensors via defer, and guards a closed session — no native handle
  leak or data race. `meanPool` bounds are consistent (`len(hidden)=seqLen*dim`,
  `len(mask)=seqLen`).
- `watch.go` accesses `pending`/`timer` only within its single select loop — no
  goroutine race; timer is stopped before re-arming.
- pgvector types registered per-conn via `AfterConnect`; `pool.Ping` failure
  closes the pool (store.go:38-41) — no leaked pool.
