# HQL structured-query DSL (v1) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax.

**Goal:** Ship `pixkb query "<hql>"` — a parameterized boolean/field filter over the concept store, ported from herald's `internal/hql`.

**Architecture:** New `internal/hql` package (lexer/ast/parser/functions ported near-verbatim from herald; schema.go/sql.go rewritten for pixkb) + `postgres.Store.QueryConcepts` + a cobra `query` command. Standalone filter path, no RRF (per spec Settled Fork 1).

**Tech Stack:** Go (module `pixkb`), pgx/pgvector, cobra, testify, `internal/output`.

## Global Constraints

- **Reference source:** herald's engine at `D:\weaver-sync\development\personal\projects\herald\internal\hql` (module `github.com/dyammarcano/herald`, package `hql`). Ported files change package imports to pixkb's (`pixkb/...`) and DROP herald-specific deps (`github.com/dyammarcano/herald/internal/types`, `me()`/`currentUser()`, the `Match`/`eval.go` backend).
- **Parameterization is a hard invariant:** every value is a `$N` placeholder; nothing interpolated; `~`/`!~` escape LIKE metachars (`\ % _`) via `escapeLike`. No SQL-injection surface.
- **v1 scope (spec Non-goals):** NO change to FTS/Vector/`hybridCore`/RRF; NO `Match` backend; NO MCP verb; NO bitemporal as-of HQL field; NO new DB migration.
- **LF only; stage explicit paths (never `git add -A`/`.`/dir); no AI attribution; conventional commits; scripts to `.scripts/`.**
- **Field map, tagPrefix semantics, and error handling are defined in the spec** `docs/superpowers/specs/2026-07-17-hql-query-dsl-design.md` — treat it as binding.

---

### Task 1: Port the lexer + AST + parser core

**Files:**
- Create: `internal/hql/doc.go`, `internal/hql/lexer.go`, `internal/hql/ast.go`, `internal/hql/parser.go`
- Test: `internal/hql/parser_test.go` (port herald's lexer/parser test cases)

**Interfaces:**
- Produces: `func Parse(input string) (Query, error)`; `Query{Where Expr; OrderBy []OrderField; Limit int}`; `OrderField{Field string; Desc bool}`; `Expr` interface with `And`/`Or`/`Not`/`Comparison` nodes; `Value{Kind ValueKind; Raw string; Args []Value}`; `ValueKind` (`ValString`/`ValWord`/`ValFunc`); `Op*` string constants. These are consumed by Tasks 3-4.

- [ ] **Step 1: Read herald's source.** Read `…\herald\internal\hql\{doc.go,lexer.go,ast.go,parser.go}` and their tests in `hql_test.go`/`hql_ext_test.go` (the lexer/parser cases + `FuzzParse`). These four files have ZERO herald-specific imports (research §A) — they port near-verbatim.

- [ ] **Step 2: Create the four files under `internal/hql/`**, package `hql`. Change only: the package doc in `doc.go` to describe pixkb fields (drop herald's `me()`/`mention`/`media`/`msgtype` grammar mentions; keep the grammar block from the spec). Keep lexer/ast/parser byte-identical except any herald `types` import (there is none in these four — confirm and remove if present).

- [ ] **Step 3: Port the lexer + parser tests** into `internal/hql/parser_test.go`: keep every lexer tokenization case and parser case that doesn't reference a herald-only field name; where a test uses a herald field (`from`, `mention`, `chat_type`…), substitute a pixkb field (`type`, `domain`, `tag`, `id`) — the parser is field-agnostic (it doesn't validate field names; that's schema.go/Task 3), so substitution is mechanical. Include the `FuzzParse` fuzz target (rename to `FuzzParse`).

- [ ] **Step 4: Run.** `go test ./internal/hql/ -run 'Parse|Lex' -v` → PASS. `go test ./internal/hql/ -run FuzzParse -fuzz FuzzParse -fuzztime 10s` → no panics (then stop the fuzz).

- [ ] **Step 5: Commit.**
```bash
git add internal/hql/doc.go internal/hql/lexer.go internal/hql/ast.go internal/hql/parser.go internal/hql/parser_test.go
git commit -m "feat(hql): port herald lexer/ast/parser core"
```

---

### Task 2: Port the function/value resolver

**Files:**
- Create: `internal/hql/functions.go`
- Test: `internal/hql/functions_test.go`

**Interfaces:**
- Consumes: `Value` (Task 1).
- Produces: `type EvalContext struct { Now time.Time }`; `escapeLike(string) string`; `resolveScalar`/`resolveDate` value resolution; `funcArity`/`resolveFunc`; `ErrUnsupported`. Consumed by Task 4 (`sql.go`).

- [ ] **Step 1: Read** `…\herald\internal\hql\functions.go` + its tests.

- [ ] **Step 2: Port with these deletions/changes:** drop `me()`/`currentUser()` and the `Self` field of `EvalContext` (pixkb has no signed-in user — spec Settled Fork 2 / field map note). Drop the `ThreadsTable` field if present (herald join-table config, not used by pixkb's standalone path). Keep: `now()`/`today()`/`startOfDay()`/`endOfDay()` derived from `EvalContext.Now`; the relative-duration regex (`^([+-]?\d+)([smhdw])$`); absolute date parse (RFC3339 / `YYYY-MM-DD`); arity-checked `resolveFunc`; `escapeLike` (metachar `\ % _`) verbatim. `EvalContext` becomes `struct { Now time.Time }`.

- [ ] **Step 3: Port the function tests**, deleting `me()`/`currentUser()` cases, keeping now/today/date/duration/escapeLike cases. Inject a fixed `Now` (e.g. `time.Date(2026,1,1,...)`) for determinism.

- [ ] **Step 4: Run.** `go test ./internal/hql/ -run 'Func|Escape|Resolve|Date' -v` → PASS.

- [ ] **Step 5: Commit.**
```bash
git add internal/hql/functions.go internal/hql/functions_test.go
git commit -m "feat(hql): port function/value resolver (drop herald me())"
```

---

### Task 3: Rewrite schema.go for pixkb fields (+ tagPrefix kind)

**Files:**
- Create: `internal/hql/schema.go`
- Test: `internal/hql/schema_test.go`

**Interfaces:**
- Produces: `fieldKind` enum incl. a NEW `kTagPrefix`; `type field struct { column string; prefix string; kind fieldKind }`; `fields map[string]field`; `func lookupField(name string) (field, bool)` (case-insensitive). Consumed by Task 4.

- [ ] **Step 1: Read** herald's `schema.go` for the shape (`fieldKind` enum, `field` struct, `fields` map, `lookupField`). Note herald kinds: `kText,kID,kBool,kDate,kEnum`.

- [ ] **Step 2: Write `internal/hql/schema.go`** with pixkb's field map (spec table) and a new `kTagPrefix` kind. Concrete content:

```go
package hql

import "strings"

type fieldKind int

const (
	kText fieldKind = iota // ILIKE substring + = != IN NOTIN IS[NOT]EMPTY
	kID                    // exact = != IN NOTIN (+ ~ !~ for id)
	kInt                   // = != > >= < <=
	kDate                  // = != > >= < <= against a timestamptz column
	kTagPrefix             // tags @> ARRAY[prefix||value]; = != IN NOTIN IS[NOT]EMPTY
)

// field maps a DSL field to its concept-table source. For kTagPrefix, `column`
// is "tags" and `prefix` is prepended to the value ("domain:", "lei:", …; empty
// for the bare `tag` field). For all other kinds `prefix` is unused.
type field struct {
	column string
	prefix string
	kind   fieldKind
}

var fields = map[string]field{
	"text":         {column: "body", kind: kText},
	"body":         {column: "body", kind: kText},
	"title":        {column: "title", kind: kText},
	"description":  {column: "description", kind: kText},
	"intent_terms": {column: "intent_terms", kind: kText},
	"source_uri":   {column: "source_uri", kind: kText},
	"type":         {column: "type", kind: kID},
	"id":           {column: "id", kind: kID},
	"language":     {column: "language", kind: kID},
	"tag":          {column: "tags", prefix: "", kind: kTagPrefix},
	"domain":       {column: "tags", prefix: "domain:", kind: kTagPrefix},
	"lei":          {column: "tags", prefix: "lei:", kind: kTagPrefix},
	"livro":        {column: "tags", prefix: "livro:", kind: kTagPrefix},
	"titulo":       {column: "tags", prefix: "titulo:", kind: kTagPrefix},
	"capitulo":     {column: "tags", prefix: "capitulo:", kind: kTagPrefix},
	"secao":        {column: "tags", prefix: "secao:", kind: kTagPrefix},
	"epoch":        {column: "last_epoch", kind: kInt},
	"updated":      {column: "updated_at", kind: kDate},
}

func lookupField(name string) (field, bool) {
	f, ok := fields[strings.ToLower(name)]
	return f, ok
}
```

- [ ] **Step 3: Write `schema_test.go`**: assert `lookupField` is case-insensitive; every mapped field resolves to the expected column/prefix/kind; an unknown field returns `false`; `domain`/`lei`/… carry the right prefix; `tag` has empty prefix + `kTagPrefix`.

- [ ] **Step 4: Run.** `go test ./internal/hql/ -run Schema -v` → PASS (and `go build ./internal/hql/`).

- [ ] **Step 5: Commit.**
```bash
git add internal/hql/schema.go internal/hql/schema_test.go
git commit -m "feat(hql): pixkb field map with tagPrefix kind"
```

---

### Task 4: Rewrite sql.go — ToSQL compiler (with tagPrefix)

**Files:**
- Create: `internal/hql/sql.go`
- Test: `internal/hql/sql_test.go`

**Interfaces:**
- Consumes: `Query`/`Expr` nodes (Task 1), `EvalContext`/`escapeLike`/resolvers (Task 2), `field`/`fieldKind`/`lookupField` (Task 3).
- Produces: `func (q Query) ToSQL(ctx EvalContext) (where string, args []any, order string, err error)`. Consumed by Task 6.

- [ ] **Step 1: Read** herald's `sql.go` for the `sqlBuilder`/`ph(v)` accumulator pattern, per-kind operator validation, `ILIKE $N ESCAPE '\'`, `IN`/`NOT IN` expansion, `IS [NOT] EMPTY` lowering, and `ORDER BY`/`LIMIT` handling.

- [ ] **Step 2: Write `internal/hql/sql.go`** preserving herald's builder structure but with pixkb's kinds. Requirements (per spec):
  - `sqlBuilder` with `args []any` and `ph(v any) string` returning `"$"+strconv(len(args))` after appending — placeholders start at `$1` (this query is the sole WHERE-builder; spec Settled Fork 1). 
  - Walk the `Expr` tree: `And`→` AND `, `Or`→` OR `, `Not`→`NOT (...)`, `Comparison`→ per-field-kind lowering via `lookupField`.
  - **Per-kind operator validation** — reject with a clear error naming field+kind+allowed-ops when an operator is illegal for the kind (`kInt`/`kDate` only comparison ops; `kID` only `= != IN NOTIN` (+`~ !~` for the `id` field); `kText` `~ !~ = != IN NOTIN IS[NOT]EMPTY`; `kTagPrefix` `= != IN NOTIN IS[NOT]EMPTY`).
  - `kText`/`kID`: `~`/`!~` → `col ILIKE $N ESCAPE '\'` (with `escapeLike` on the value) / negation; `=`/`!=` → `col = $N`/`col != $N`; `IN`/`NOT IN` → `col = ANY($N)` / `col != ALL($N)`; `IS [NOT] EMPTY` → `(col IS NULL OR col = '')` (negated wrapped in `NOT`).
  - `kInt`/`kDate`: `col <op> $N`; value resolved via functions.go (`resolveScalar`/`resolveDate`, dates → `time.Time` or the parsed literal).
  - **`kTagPrefix` (the new piece):** value `v` becomes placeholder `prefix+v`. `=` → `tags @> ARRAY[$N]::text[]`; `!=` → `NOT (tags @> ARRAY[$N]::text[])`; `IN (a,b,…)` → `(tags @> ARRAY[$A]::text[] OR tags @> ARRAY[$B]::text[] …)`; `NOT IN` → `NOT (…OR…)`; `IS EMPTY` → for a non-empty prefix `NOT EXISTS (SELECT 1 FROM unnest(tags) t WHERE t LIKE $N)` with `$N = prefix+'%'`; for the bare `tag` field `IS EMPTY` → `(tags IS NULL OR cardinality(tags) = 0)`. `IS NOT EMPTY` wraps in `NOT`.
  - **`ORDER BY`:** validate each order field via `lookupField`; only non-`kTagPrefix` fields orderable (reject tagPrefix order with a clear message); emit `col ASC|DESC`. `LIMIT` passes through as an int (validated ≥ 0; 0 = no limit).
  - Return `where` WITHOUT a leading `WHERE` keyword (caller adds it), `order` WITHOUT `ORDER BY` (caller adds it), so `QueryConcepts` composes cleanly.

- [ ] **Step 3: Write `sql_test.go`** — golden `(where, args, order)` assertions (fixed `EvalContext.Now`), covering at minimum:
  - `type = LegalArticle` → `type = $1`, args `[LegalArticle]`.
  - `domain = tax` → `tags @> ARRAY[$1]::text[]`, args `[domain:tax]`.
  - `lei IN (lc-214-2025, lc-999)` → OR-of-two-containments, args `[lei:lc-214-2025, lei:lc-999]`.
  - `livro IS EMPTY` → the `NOT EXISTS … LIKE $1` form, arg `[livro:%]`.
  - `text ~ "50%_x"` → `body ILIKE $1 ESCAPE '\'`, arg the escaped `50\%\_x`.
  - `epoch >= 2` → `last_epoch >= $1`, arg `[2]`.
  - `updated > 2026-01-01` → `updated_at > $1`, a time arg.
  - `NOT (type = X OR type = Y) AND domain = tax` → parenthesization + AND correct.
  - `... ORDER BY id DESC LIMIT 5` → order `id DESC`, and an illegal `ORDER BY domain` rejected.
  - operator/kind errors: `updated ~ "x"` and `type > 3` return errors.

- [ ] **Step 4: Run.** `go test ./internal/hql/ -v` (whole package) → PASS.

- [ ] **Step 5: Commit.**
```bash
git add internal/hql/sql.go internal/hql/sql_test.go
git commit -m "feat(hql): ToSQL compiler with tagPrefix containment lowering"
```

---

### Task 5: `Store.QueryConcepts`

**Files:**
- Create: `internal/store/postgres/query.go`
- Test: `internal/store/postgres/query_test.go`

**Interfaces:**
- Produces: `func (s *Store) QueryConcepts(ctx context.Context, where string, args []any, order string, limit int) ([]okf.Concept, error)`.

- [ ] **Step 1: Find the reusable concept-row scanner.** Read `internal/store/postgres/{search.go,related.go,asof.go,concept.go}` to locate how full `okf.Concept` rows are SELECTed+scanned (columns: id, type, title, description, resource, tags, language, body, content_sha, source_uri, first_epoch/last_epoch, updated_at, intent_terms). Reuse the existing scan helper if one exists; otherwise write a local `scanConcept(rows)`.

- [ ] **Step 2: Write the failing integration test** `query_test.go`, guarded like the other DB tests (skip without the test DSN; follow the exact skip/harness pattern already in `search_test.go`). Seed via the same insert pattern used there: a `LegalArticle` (tags `{domain:tax, lei:lc-214-2025, titulo:ii}`), an `ApiEndpoint` (tags `{domain:tax, api}`), and a `ManualSection` (tags `{domain:pix, manual}`). Assert:
  - `QueryConcepts(ctx, "tags @> ARRAY[$1]::text[]", []any{"domain:tax"}, "id ASC", 0)` returns exactly the two tax ids in id order.
  - a `type = $1` where with `LegalArticle` returns only the article.
  - `order = "id DESC"` reverses; `limit = 1` truncates.
  - empty `where` (`""`) returns all seeded rows (no `WHERE` emitted).

- [ ] **Step 3: Implement `QueryConcepts`:** build `SELECT <cols> FROM concept`, append ` WHERE `+where when non-empty, ` ORDER BY `+order when non-empty, ` LIMIT `+n when >0. Pass `args...` to `pool.Query`. Scan into `[]okf.Concept`. No string interpolation of values (they arrive as `args`); `where`/`order` are HQL-built SQL fragments (parameterized).

- [ ] **Step 4: Run** against the local throwaway DB: `task testdb:up`; set `PIXKB_DSN` to the test DSN; `go test ./internal/store/postgres/ -run QueryConcepts -v` → PASS; `task testdb:down`.

- [ ] **Step 5: Commit.**
```bash
git add internal/store/postgres/query.go internal/store/postgres/query_test.go
git commit -m "feat(store): QueryConcepts — parameterized structured concept filter"
```

---

### Task 6: `pixkb query` command + wiring

**Files:**
- Create: `cmd/pixkb/query.go`
- Modify: `cmd/pixkb/commands.go` (`attachCommands` — add `newQueryCmd()`)
- Test: `cmd/pixkb/query_test.go`

**Interfaces:**
- Consumes: `hql.Parse`/`Query.ToSQL` (Tasks 1-4), `Store.QueryConcepts` (Task 5), `internal/output.Render`, `newRunner`/`openStore` (existing).

- [ ] **Step 1: Read** an existing store-backed command with `--format` (e.g. `newSearchCmd` in `cmd/pixkb/commands.go`) for the store-open + output.Render pattern.

- [ ] **Step 2: Write the failing command test** `query_test.go`: a parse-error input (e.g. `"type = = ="`) makes `newQueryCmd().Execute()` (with args + a buffer) return a non-nil error / non-zero exit; assert the error mentions the parse failure. (This test needs no DB — parse/lower happens before the store call; structure the command so `hql.Parse`+`ToSQL` run and can fail before `openStore`.)

- [ ] **Step 3: Implement `cmd/pixkb/query.go`:**
```go
func newQueryCmd() *cobra.Command {
	var format string
	var limit int
	cmd := &cobra.Command{
		Use:   "query <hql>",
		Short: "Structured HQL filter over the concept store (e.g. \"type = LegalArticle AND domain = tax\")",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			q, err := hql.Parse(args[0])
			if err != nil {
				return fmt.Errorf("parse query: %w", err)
			}
			where, qargs, order, err := q.ToSQL(hql.EvalContext{Now: time.Now()})
			if err != nil {
				return fmt.Errorf("compile query: %w", err)
			}
			lim := q.Limit
			if limit > 0 { lim = limit } // --limit overrides an in-query LIMIT
			cfg := loadConfig()
			ctx := cmd.Context()
			st, err := openStore(ctx, cfg)
			if err != nil { return err }
			defer st.Close()
			concepts, err := st.QueryConcepts(ctx, where, qargs, order, lim)
			if err != nil { return err }
			return output.Render(cmd.OutOrStdout(), format, concepts) // match the exact output.Render signature/type search uses
		},
	}
	cmd.Flags().StringVar(&format, "format", "text", "output format: text|json|md|yaml")
	cmd.Flags().IntVar(&limit, "limit", 0, "max results (overrides an in-query LIMIT; 0 = query's own/none)")
	return cmd
}
```
Adapt `output.Render`'s exact signature and the concept→renderable mapping to whatever `newSearchCmd` uses (search renders hits, not raw concepts — if `output.Render` needs a hit/row type, map `[]okf.Concept` into it or add a thin concept renderer; the plan's implementer resolves this against the real `internal/output` API and mirrors search's rendering).

- [ ] **Step 4: Wire** into `attachCommands` (`cmd/pixkb/commands.go`): add `newQueryCmd()` to the `root.AddCommand(...)` list.

- [ ] **Step 5: Run.** `go test ./cmd/pixkb/ -run Query -v` → PASS; `go build ./...` clean. Then a live smoke against the local DB (optional, in the SDD verify): `task testdb:up` → seed/ingest → `go run ./cmd/pixkb query 'type = LegalArticle AND domain = tax LIMIT 5'` → `task testdb:down`.

- [ ] **Step 6: Commit.**
```bash
git add cmd/pixkb/query.go cmd/pixkb/commands.go cmd/pixkb/query_test.go
git commit -m "feat(cli): pixkb query — HQL structured search command"
```

---

## Self-Review

- **Spec coverage:** package layout → Tasks 1-4; field map + tagPrefix → Task 3+4; ToSQL parameterization/validation/ILIKE/IN/IS-EMPTY/ORDER BY/LIMIT → Task 4; `QueryConcepts` standalone path → Task 5; `pixkb query` CLI + `--format` → Task 6. Settled forks honored: no RRF touch (no FTS/Vector edits anywhere), no `Match`/`eval.go` (never created), no MCP verb (not in plan), `epoch`→`last_epoch`/`updated`→`updated_at` (Task 3 map), `~`=ILIKE (Task 4). Error handling (parse/unknown-field/kind-mismatch/empty) → Tasks 1,3,4,6 tests.
- **Placeholder scan:** the two port tasks (1,2) reference herald's real committed source as their "complete code"; the rewrite tasks (3,4) and integration/CLI tasks (5,6) carry concrete code or a precise, real-API-anchored spec. Task 6 Step 3 explicitly flags the one adaptation point (`output.Render`'s real signature) for the implementer to resolve against the codebase — not a vague placeholder.
- **Type consistency:** `Query`/`Expr`/`Value`/`OrderField`/`EvalContext{Now}`/`fieldKind`/`field{column,prefix,kind}`/`lookupField`/`ToSQL`/`QueryConcepts` names are consistent across tasks. `kTagPrefix` introduced in Task 3, consumed in Task 4.
- **Build order:** 1→2→3→4 are pure-Go (no DB), independently testable; 5 needs the DB; 6 ties it together. Each task ends green and committed.
