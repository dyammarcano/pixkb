# Design: HQL filter in hybrid search — `search --where "<hql>"` (HQL v2)

**Date:** 2026-07-17
**Status:** Draft (authored autonomously under `docs/AUTONOMY.md`; forks settled below)
**Backlog item:** HQL v2 follow-up (i) — "fold HQL into `search`'s hybrid RRF (HQL narrows, RRF ranks)."
**Builds on:** the shipped HQL v1 (`internal/hql`, `Query.ToSQL`, `pixkb query`) and the research seam analysis in `.superpowers/research/hql-port-input.md` §2.

## Goal

Let hybrid search accept an **HQL predicate that narrows the candidate set before
RRF ranking**, so a user can combine precise structured filtering with relevance
ranking in one call:

```
pixkb search "recolhimento split" --where 'type = LegalArticle AND domain = tax AND livro IN (i, ii)'
```

Today you can filter precisely (`pixkb query`, unranked) OR rank (`pixkb search`,
only coarse `--type`/`--tag` filters) — not both. This composes them: the HQL
`WHERE` is AND-ed into both search arms (FTS and vector), RRF still ranks the
survivors. The ranking math (`hybridCore`, ADR 0002) is **untouched**.

## The core fork (decided + logged): safe placeholder composition

The research flagged the one real hazard: HQL's `ToSQL` numbers its placeholders
`$1..$K`, but `FTS`/`Vector` already have `$1..$M` in flight, so splicing HQL's
`WHERE` in naively collides placeholder numbers — and fixing it by string-
substituting `$1→$M+1` risks corrupting `$1` inside `$10` (an injection/corruption
bug).

**Decision (Settled Fork 1): thread a `startArg` offset through the SQL builder —
never renumber strings.** Add `func (q Query) ToSQLAt(ctx EvalContext, startArg int)
(where string, args []any, order string, err error)`; the existing `ToSQL` becomes
`ToSQLAt(ctx, 0)`. `sqlBuilder.ph` numbers from `startArg+len(args)+1`. The caller
passes the count of args already bound, so HQL's fragment is born correctly
numbered — no post-hoc renumbering, no string surgery, zero new injection surface.
Every value remains a `$N` placeholder exactly as in v1.

**Decision (Settled Fork 2): the store does NOT import `hql`.** `postgres.Filter`
gains one optional field: `HQLWhere func(startArg int) (where string, args []any,
err error)` — a closure. `cmd/pixkb`/`kbmcp` build it from `hql.Parse` +
`ToSQLAt`; `FTS`/`Vector` just invoke it. This keeps the `hql`→`postgres` dependency
out of the store package (postgres stays hql-agnostic; the closure is the seam).

**Decision (Settled Fork 3): only the HQL `WHERE` composes; its `ORDER BY`/`LIMIT`
are ignored in search mode.** RRF determines order; `--limit` caps. An HQL `ORDER
BY`/`LIMIT` inside `--where` is silently ignored (documented) — mixing a structured
sort into a relevance ranking is incoherent. (A user who wants HQL ordering uses
`pixkb query`.)

## Non-goals (v2)

- No change to RRF fusion / `hybridCore` / type-weight / title-boost (ADR 0002).
- No new HQL grammar or fields — reuses v1's `internal/hql` verbatim + `ToSQLAt`.
- No bitemporal HQL field (still deferred); `--where` composes with the existing
  `--as-of-*` flags via the existing `asOfConceptPredicate` (both AND-ed).
- `Match` backend still out.

## Architecture

1. **`internal/hql/sql.go`:** extract the current `ToSQL` body into
   `ToSQLAt(ctx, startArg)`; `ToSQL(ctx)` = `ToSQLAt(ctx, 0)`. `sqlBuilder` gains a
   `base int` so `ph` returns `$(base+len(args))`. Pure refactor + one new entry
   point; all v1 golden tests stay green (they call `ToSQL` = offset 0).
2. **`internal/store/postgres` (`search.go`, `vector.go`):** `Filter` gains
   `HQLWhere func(startArg int) (string, []any, error)`. In `FTS` and `Vector`,
   after the base `where`/`args` are assembled (including type/tag/exclude/as-of),
   if `f.HQLWhere != nil` call `f.HQLWhere(len(args))`, append `AND (<where>)` to
   the SQL and the returned args to `args`. Nil closure → zero behavior change.
3. **`cmd/pixkb/commands.go` (`newSearchCmd`):** new `--where <hql>` flag. When set,
   `hql.Parse` it once (parse error → fail before the store), then set
   `f.HQLWhere = func(start int) (string, []any, error) { w, a, _, err := q.ToSQLAt(hql.EvalContext{Now: time.Now()}, start); return w, a, err }`.
4. **`internal/kbmcp/server.go` (`search` tool):** add a `where` input field, same
   wiring, so MCP agents get filtered ranking too.

## Data flow

`pixkb search "q" --where 'type = LegalArticle AND domain = tax'` →
`hql.Parse` → build `HQLWhere` closure → `query.Hybrid(store, emb, "q", filter)` →
`FTS` binds `$1..$M` (query text, type, tags, as-of), then `HQLWhere(M)` returns
`type = $M+1 AND tags @> ARRAY[$M+2]::text[]` + args, AND-ed in; same for `Vector`
→ both arms return only rows matching the HQL predicate → `hybridCore` fuses/ranks
→ ranked, filtered hits.

## Error handling

- Parse error in `--where` → returned before opening the store (like `pixkb query`).
- Compile/lowering error (unknown field, bad operator) → the closure returns the
  error; `FTS`/`Vector` propagate it (search fails with a clear message). Since the
  closure runs inside FTS/Vector, add a lightweight pre-check: `cmd` calls
  `q.ToSQLAt(ctx, 0)` once at flag-parse time to surface lowering errors early, then
  reuses the parsed query in the closure. (Belt-and-suspenders: fail fast, and the
  closure can't silently swallow.)
- `HQLWhere` returning an error must abort the query, not run an unfiltered search
  (fail closed) — asserted in a test.

## Testing

- **`internal/hql`:** `ToSQLAt` offset tests — `ToSQLAt(ctx, 5)` numbers the first
  placeholder `$6`; `ToSQL` == `ToSQLAt(ctx,0)` unchanged (all v1 goldens pass).
- **`postgres` (local DB):** `Filter.HQLWhere` composition — seed a `LegalArticle`
  (domain:tax) + an `ApiEndpoint` (domain:tax) + a `ManualSection` (domain:pix);
  a hybrid search with `HQLWhere` = `type = LegalArticle` returns only the article
  even when the raw query text would match all three; verify placeholder numbering
  is correct (no pgx "wrong number of parameters" error — the exact failure a
  renumbering bug would cause); verify a nil `HQLWhere` is byte-identical to today.
- **`cmd`:** `search --where "bad ="` fails before the store; `search --where 'type
  = X'` builds the closure; `--where` with `--as-of-epoch` both apply (AND).
- **`kbmcp`:** the `search` tool's `where` field narrows results (DB-gated test).
- **Regression:** the full existing search/eval suite stays green (nil closure path).

## Open questions for the plan

1. Whether `sqlBuilder` needs `base` as a field or `ToSQLAt` can pre-seed the args
   slice — pick whichever keeps the v1 goldens untouched (the field is cleaner).
2. Exact `--where` flag name (`--where` vs `--filter` vs `--hql`) — lean `--where`
   (matches the SQL mental model and the `query` DSL).
3. Whether to also expose `--where` on `--mode multi` (each subquery's Filter
   carries the same closure — should compose fine; confirm in the plan).
