# Design: HQL structured-query DSL (v1) — `pixkb query "<hql>"`

**Date:** 2026-07-17
**Status:** Draft (authored autonomously under `docs/AUTONOMY.md`; forks settled below)
**Backlog item:** P2 "Structured query DSL for rich search (HQL-style), ported from herald."
**Research input:** `.superpowers/research/hql-port-input.md` (herald `internal/hql`
file-by-file inventory + pixkb field/column mapping).

## Goal

Give pixkb a structured boolean/field query language over the concept store, so a
user can express precise queries instead of only the flat flag filters
(`--type`/`--tag`/`--include-type`/`--exclude-id`/`--as-of-*`). Ported from
herald's proven `internal/hql` engine. v1 target — this exact query runs:

```
pixkb query 'type = LegalArticle AND domain = tax AND lei = lc-214-2025 AND (text ~ "split" OR title ~ "recolhimento") ORDER BY id ASC LIMIT 20'
```

…returning the matching concepts as a structured, filtered, ordered list. This
directly exercises the Phase A/B data (the `type`/`domain`/`lei` axes) that has no
precise-query surface today.

## Settled forks (decided autonomously; rationale logged in `docs/AUTONOMY.md`)

1. **v1 is a STANDALONE filter path, not folded into hybrid RRF.** HQL compiles to
   a parameterized `WHERE`/`ORDER BY`/`LIMIT` run directly against the `concept`
   table via a new `Store.QueryConcepts`, returning concepts with **no FTS/vector
   ranking**. *Why:* it delivers the backlog's core intent (precise boolean/field
   filtering + ordering) as the smallest shippable unit, and it sidesteps the
   research's biggest risk — splicing an HQL `WHERE` into `FTS`/`Vector`'s
   already-numbered `$N` arg lists (a placeholder-renumbering injection hazard).
   Folding HQL into `search`'s RRF ("HQL narrows, RRF ranks") is a **v2
   follow-up**, logged to backlog.
2. **`Match` (in-memory Go predicate) is OUT of v1.** pixkb has no live-stream /
   watch-daemon consumer for it (herald's reason for `Match`). Only the `ToSQL`
   backend is ported. If a future watch/curate-rule feature needs it, add `eval.go`
   then. This roughly halves the port.
3. **MCP `query` verb is a v1 fast-follow, not v1 itself.** Once
   `Store.QueryConcepts` + the `hql` package exist, the MCP verb is thin; keep v1
   to the CLI `pixkb query` surface to stay focused. Logged to backlog.
4. **`epoch` and `updated` map to REAL `concept` columns, not `concept_fact`.**
   `epoch` → `concept.last_epoch` (int compare), `updated` → `concept.updated_at`
   (timestamp compare). *Why:* v1 queries current state; reusing the plain columns
   avoids reinventing (or entangling with) `asOfConceptPredicate`'s bitemporal
   DISTINCT-ON logic. Time-travel stays on the existing `--as-of-*` flags; a
   bitemporal HQL field is deferred with the RRF-composition v2.
5. **`~`/`!~` = ILIKE substring (metachar-escaped), deliberately distinct from
   FTS.** Documented in the command help: `text ~ "put-pix"` finds a literal
   substring FTS would tokenize away. It is a filter, not a ranking mechanism.
6. **No `type` enum validation** — match pixkb's existing permissive `--type`
   behavior (a typo returns zero rows). Consistency over a new guard the flag path
   lacks.

## Non-goals (v1)

- Any change to FTS/Vector/`hybridCore` or the RRF ranking path.
- The `Match` backend / `me()`/`currentUser()` function (herald auth concept).
- Bitemporal as-of HQL fields (`asof`, point-in-time `time`).
- The MCP `query` verb (fast-follow).
- New DB columns/migrations — every field maps to an existing `concept` column or
  a `tags[]` containment.

## Architecture

New self-contained package `internal/hql` (ported from herald, ~890 LOC there, less
here without `Match`) + one new `postgres.Store` method + one new cobra command.

```
internal/hql/
  doc.go       — package doc + grammar summary (pixkb fields; no me()/mention/media)
  lexer.go     — PORT near-verbatim (herald-agnostic scanner)
  ast.go       — PORT near-verbatim (pure AST data)
  parser.go    — PORT near-verbatim (recursive-descent, full v1 grammar + ORDER BY/LIMIT)
  functions.go — PORT minus me()/currentUser(): keep now()/today()/startOfDay()/
                 endOfDay(), duration literals, escapeLike; EvalContext keeps Now only
  schema.go    — REWRITE: pixkb field map (below) + a new kTagPrefix fieldKind
  sql.go       — REWRITE structure-preserving: sqlBuilder/ph() + escapeLike ILIKE +
                 IN/NOTIN + IS EMPTY, PLUS tagPrefixCmp → tags @> ARRAY[$N]::text[]
```

Plus:
- `internal/store/postgres/query.go` — `QueryConcepts(ctx, where string, args []any, order string, limit int) ([]okf.Concept, error)`: `SELECT <cols> FROM concept [WHERE <where>] [ORDER BY <order>] [LIMIT n]`, scanning full `okf.Concept` rows (reuse the existing concept-row scan helper if one exists; else a local scanner). `where`/`order` come from `hql`, `args` are `$N`-parameterized.
- `cmd/pixkb/query.go` — `newQueryCmd()`: parses the HQL string via `hql.Parse`, lowers via `Query.ToSQL`, calls `Store.QueryConcepts`, renders via the existing `internal/output` (`--format text|json|md|yaml`, default text). Registered in `attachCommands`.

### Field map (`schema.go`)

| DSL field | Source | Kind | Ops |
|---|---|---|---|
| `text` / `body` | `concept.body` | text | `~ !~ = != IN NOTIN IS[NOT]EMPTY` |
| `title` | `concept.title` | text | text ops |
| `description` | `concept.description` | text | text ops |
| `intent_terms` | `concept.intent_terms` | text | text ops |
| `type` | `concept.type` | id | `= != IN NOTIN` |
| `id` | `concept.id` | id | `= != IN NOTIN ~ !~` |
| `language` | `concept.language` | id | `= != IN NOTIN` |
| `source_uri` | `concept.source_uri` | text | `~ !~ = != IS[NOT]EMPTY` |
| `tag` | `concept.tags` containment | **tagPrefix** (raw value, no prefix) | `= != IN NOTIN IS[NOT]EMPTY` |
| `domain` | `tags @> ARRAY['domain:'\|\|v]` | **tagPrefix** (prefix `domain:`) | `= != IN NOTIN` |
| `lei`/`livro`/`titulo`/`capitulo`/`secao` | `tags @> ARRAY['<field>:'\|\|v]` | **tagPrefix** | `= != IN NOTIN IS[NOT]EMPTY` |
| `epoch` | `concept.last_epoch` | int (date-family compare) | `= != > >= < <=` |
| `updated` | `concept.updated_at` | date | `= != > >= < <=` (RFC3339 / `YYYY-MM-DD` / rel-duration via functions.go) |

`tagPrefix` kind — the one genuinely new piece vs herald. For field with prefix `p`
(empty for bare `tag`): `= v` → `tags @> ARRAY[$N]::text[]` with `$N = p||v`;
`!= v` → `NOT (tags @> ARRAY[$N])`; `IN (a,b)` → OR of containments; `IS EMPTY` →
`NOT EXISTS (SELECT 1 FROM unnest(tags) t WHERE t LIKE $N)` with `$N = p||'%'` (for
bare `tag`, `IS EMPTY` = `tags IS NULL OR tags = '{}'`). Every value stays a `$N`
placeholder.

## Data flow

1. `pixkb query "<hql>"` → `hql.Parse(input)` → `Query{Where, OrderBy, Limit}` (or a
   parse error with position, surfaced to the user).
2. `Query.ToSQL(ctx)` → `(where, args, order, err)`, all placeholders `$1..$N`
   (this is the ONLY WHERE-builder for the query, so herald's `ph()`-from-`$1` ports
   unchanged — no renumbering needed, which is exactly why v1 is standalone).
3. `Store.QueryConcepts(ctx, where, args, order, limit)` runs the parameterized SQL
   against `concept`, returns `[]okf.Concept`.
4. `internal/output.Render` prints per `--format`.

## Error handling

- **Parse errors** (bad syntax, unknown operator) → returned from `hql.Parse` with a
  clear message + offset; the command prints it and exits non-zero. Table-tested.
- **Unknown field** → `schema.lookupField` miss → a clear `unknown field "foo"` error
  at lowering time, not a SQL error.
- **Operator/kind mismatch** (e.g. `updated ~ "x"`, `type > 5`) → rejected at
  lowering with a message naming the field, kind, and allowed ops (herald's per-kind
  validation, ported).
- **Empty query string** → error ("empty query").
- **SQL is always parameterized** — no value is ever interpolated; `~`/`!~` escape
  `\ % _` via `escapeLike`. This is a hard invariant (charter: no injection surface).

## Testing

- **`internal/hql` unit tests (ported + extended, no DB):**
  - lexer: tokens, quoted strings with doubled-quote escape, operators, word-char
    IDs (`lc-214-2025`, `domain:tax`, `put-pix-e2eid`).
  - parser: precedence (`NOT>AND>OR`), parens, `IN`/`NOT IN`, `IS [NOT] EMPTY`,
    `ORDER BY ... ASC|DESC`, `LIMIT n`, and parse-error cases.
  - `ToSQL`: golden `(where, args, order)` for representative queries incl. every
    field kind — crucially the **tagPrefix** shapes (`domain = tax` →
    `tags @> ARRAY[$1]` with arg `domain:tax`; `lei IN (a,b)` → OR-of-containments;
    `livro IS EMPTY`), ILIKE escaping (`text ~ "50%_x"`), and int/date compares.
  - `FuzzParse` ported (must not panic on arbitrary input).
- **`postgres.QueryConcepts` integration (local throwaway DB, the session pattern):**
  seed a few concepts incl. a `LegalArticle` tagged `domain:tax`+`lei:lc-214-2025`
  and an `ApiEndpoint` tagged `domain:tax`; assert the target query returns exactly
  the expected ids in the expected order; assert a `domain = pix` query excludes the
  tax rows; assert `LIMIT` and `ORDER BY ... DESC` honored.
- **Command test:** `newQueryCmd` wired, `--format json` round-trips, a parse error
  exits non-zero (httptest-style / cobra `Execute` with a buffer).
- **No prod mutation** in the plan; prod is read-only for `query` anyway.

## Open questions for the plan

1. Whether `concept`-row scanning already has a reusable helper (`related.go`/
   `asof.go` scan concepts) to avoid a duplicate column list — the plan checks and
   reuses if so.
2. Exact `EvalContext` shape retained from herald once `me()` is dropped (keep
   `Now time.Time` for `now()`/`today()`/duration literals used by `updated`).
3. Whether `ORDER BY` field validation reuses the same `schema.lookupField` (yes —
   only known fields orderable; `text`/tagPrefix fields orderable by their column,
   tagPrefix fields are NOT orderable → reject with a clear message).
