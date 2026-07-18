# ADR 0005 — Bitemporal epoch engine with as-of queries

- **Status:** Accepted
- **Date:** 2026-06-22
- **Context item:** ROADMAP Phase 5 (Epoch Engine); the base pixkb design

## Context

The KB evolves: sources are re-ingested, concepts change, agents write enriched
facts back. The system needs (a) a canonical, portable history and (b) the
ability to answer "what did the KB say as of epoch N / time T". The OKF markdown
bundle is the source of truth and is git-versioned per epoch, but the derived
Postgres index also has to support point-in-time reads without keeping every
snapshot as a separate table.

## Options

1. **Snapshot per epoch** — copy the whole concept set into an epoch-tagged
   table on every cut. Simple reads, but storage grows linearly and diffs are
   expensive.
2. **Bitemporal fact table** — one `concept_fact` row per (concept version),
   carrying a *valid-time* range (when the fact was true in the domain) and a
   *transaction-time* range (when the row was recorded), both as `tstzrange`.
   Point-in-time reads become range predicates; the current view is the open-tx,
   open-valid row.

## Decision

**Option 2 — a bitemporal `concept_fact` table.** Each `RecordFact` closes the
prior open row (capping its `tx` at the DB clock and its `valid` at the new
version's `validFrom`, so historical versions get true valid-time history) and
inserts the new row with `valid = [validFrom, ∞)`, `tx = [clock_timestamp(), ∞)`.
Both `tx` bounds are driven by `clock_timestamp()` (which advances per statement)
rather than a shared epoch timestamp, so the closed range is always non-empty and
cannot overlap the new one — avoiding both an empty-range row-drop and a gist
`EXCLUDE (id WITH =, tx WITH &&)` violation when two facts for one id land in the
same epoch. As-of reads filter on the ranges; epoch metadata lives in an `epoch`
table, and the bundle git commit per epoch is the durable, portable history.

## Consequences

- **Positive:** point-in-time (`as-of epoch`/`as-of time`) reads without
  per-epoch snapshot tables; the bundle+git remains the rebuildable source of
  truth (`reindex` replays it); storage grows with *changes*, not with epochs.
- **Negative:** bitemporal correctness is subtle — the clock-driven bounds and
  range-close logic are load-bearing (an earlier review flagged, and the current
  code fixes, exactly these hazards; see docs/REVIEW.md resolution). Epoch
  allocation must be serialized (done — `Runner` mutex).
- **Neutral:** `RecordFact` keeps a `txFrom` parameter for API compatibility even
  though tx-time is DB-clock driven.

## Follow-ups

- The cross-system write (Postgres + bundle + git) is intentionally non-atomic;
  recovery is `reindex` (now read-before-truncate failure-safe).
