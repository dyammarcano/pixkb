# HQL filter in hybrid search (`search --where`) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development. Steps use checkbox (`- [ ]`) syntax.

**Goal:** `pixkb search "<q>" --where "<hql>"` narrows the hybrid candidate set with an HQL predicate before RRF ranks; ranking math untouched.

**Architecture:** Add `hql.Query.ToSQLAt(ctx, startArg)` (offset-numbered placeholders, no string renumbering). Add `postgres.Filter.HQLWhere func(startArg int) (string, []any, error)` closure; `FTS`/`Vector` AND it in (nil = no-op). Wire a `--where` flag on `newSearchCmd` and a `where` field on the MCP `search` tool. Reuse `internal/hql` v1 verbatim.

**Tech Stack:** Go, pgx, cobra, the MCP go-sdk, testify, local pgvector.

## Global Constraints

- **Injection invariant:** every HQL value stays a `$N` placeholder; the ONLY new mechanism is offset-numbering via `startArg` — NO string substitution of placeholder numbers anywhere. `~`/`!~` still escape metachars (unchanged v1 code).
- **Zero behavior change when `--where` is absent:** a nil `Filter.HQLWhere` must make `FTS`/`Vector` byte-identical to today. The existing search/eval suite must stay green.
- **Ranking math (ADR 0002) is untouched:** no edits to `hybridCore`/RRF/type-weight/title-boost. `--where` only adds an AND-ed predicate to the two arms' WHERE.
- **Composition point:** in `FTS`/`Vector`, the HQL WHERE is AND-ed AFTER the existing predicates (type/tag/exclude/as-of) and BEFORE the trailing `limit` arg is appended — so placeholder numbering stays contiguous and the `LIMIT $N` still binds last.
- **LF; explicit `git add` paths; no AI attribution; conventional commits; scripts to `.scripts/`.**
- Spec is binding: `docs/superpowers/specs/2026-07-17-hql-search-filter-design.md`.

---

### Task 1: `hql.Query.ToSQLAt(ctx, startArg)`

**Files:**
- Modify: `internal/hql/sql.go`
- Test: `internal/hql/sql_test.go` (append offset tests)

**Interfaces:**
- Produces: `func (q Query) ToSQLAt(ctx EvalContext, startArg int) (where string, args []any, order string, err error)`; `ToSQL(ctx)` becomes `return q.ToSQLAt(ctx, 0)`. Placeholders number from `startArg+1`.

- [ ] **Step 1: Read** `internal/hql/sql.go` — locate the `sqlBuilder` struct and its `ph(v any) string` method (returns `"$"+strconv.Itoa(len(b.args))` after appending), and the `ToSQL` method body.

- [ ] **Step 2: Write the failing test** (append to `sql_test.go`):
```go
func TestToSQLAt_Offset(t *testing.T) {
	q, err := Parse("type = LegalArticle AND domain = tax")
	require.NoError(t, err)

	// Offset 0 == ToSQL (v1 behavior).
	w0, a0, _, err := q.ToSQLAt(EvalContext{Now: fixedNow()}, 0)
	require.NoError(t, err)
	wc, ac, _, err := q.ToSQL(EvalContext{Now: fixedNow()})
	require.NoError(t, err)
	require.Equal(t, wc, w0)
	require.Equal(t, ac, a0)

	// Offset 5: first placeholder is $6, second $7; args unchanged.
	w5, a5, _, err := q.ToSQLAt(EvalContext{Now: fixedNow()}, 5)
	require.NoError(t, err)
	require.Contains(t, w5, "$6")
	require.Contains(t, w5, "$7")
	require.NotContains(t, w5, "$1 ")
	require.Equal(t, a0, a5) // same values, just renumbered in the SQL text
}
```
(If the file's fixed-clock helper is named differently than `fixedNow()`, use the real one.)

- [ ] **Step 3: Run** `go test ./internal/hql/ -run ToSQLAt -v` → FAIL (ToSQLAt undefined).

- [ ] **Step 4: Implement.** Give `sqlBuilder` a `base int` field. Change `ph` to `return "$" + strconv.Itoa(b.base+len(b.args))` (append first, then number = base+len). Rename the current `ToSQL` body into `ToSQLAt(ctx EvalContext, startArg int)`, constructing the builder with `base: startArg`. Re-add `func (q Query) ToSQL(ctx EvalContext) (string, []any, string, error) { return q.ToSQLAt(ctx, 0) }`. NOTE: `base+len(b.args)` after an append where `len` already counts the new arg gives `base+1` for the first — verify the existing `ph` already does "append then len" (offset 0 must reproduce v1 exactly; the `TestToSQLAt_Offset` offset-0 assertion + all existing goldens guard this).

- [ ] **Step 5: Run** `go test ./internal/hql/ -v` (whole package incl. all v1 goldens) → PASS; `go vet ./internal/hql/`; `gofmt -l internal/hql/`; `golangci-lint run ./internal/hql/...` if available.

- [ ] **Step 6: Commit.**
```bash
git add internal/hql/sql.go internal/hql/sql_test.go
git commit -m "feat(hql): ToSQLAt(startArg) for placeholder-offset composition"
```

---

### Task 2: `Filter.HQLWhere` + compose into FTS and Vector

**Files:**
- Modify: `internal/store/postgres/search.go` (the `Filter` struct + `FTS`), `internal/store/postgres/vector.go` (`Vector`)
- Test: `internal/store/postgres/query_test.go` or a new `hqlfilter_test.go` (local-DB)

**Interfaces:**
- Consumes: nothing from hql (store stays hql-agnostic — the closure is the seam).
- Produces: `Filter.HQLWhere func(startArg int) (where string, args []any, err error)` (nil = no HQL predicate).

- [ ] **Step 1: Read** `search.go`'s `Filter` struct + `FTS`, and `vector.go`'s `Vector` (its `add(cond string)` closure over `args`). Confirm both number placeholders by `len(args)` and append `limit` last.

- [ ] **Step 2: Add the field** to `Filter` (in search.go, after `MinVecScore`):
```go
	// HQLWhere, when non-nil, contributes an additional parameterized WHERE
	// fragment (from internal/hql's ToSQLAt) AND-ed into FTS/Vector. It receives
	// the count of args already bound and must number its own placeholders from
	// startArg+1; it returns the fragment (no leading AND) + its arg values. The
	// store never imports hql — the caller supplies this closure.
	HQLWhere func(startArg int) (where string, args []any, err error)
```

- [ ] **Step 3: Compose in `FTS`.** Immediately AFTER the `asOfConceptPredicate` block and BEFORE `args = append(args, limit)`:
```go
	if f.HQLWhere != nil {
		hw, ha, err := f.HQLWhere(len(args))
		if err != nil {
			return nil, fmt.Errorf("hql filter: %w", err)
		}
		if hw != "" {
			where += " AND (" + hw + ")"
			args = append(args, ha...)
		}
	}
```

- [ ] **Step 4: Compose in `Vector`.** Find the equivalent point (after the last existing predicate, before the limit arg is appended). Vector uses an `add(cond)` closure and a local `args`; insert the same block, numbering from `len(args)` at that point, appending the HQL args. If Vector applies `MinVecScore` post-query (per the code comment), the HQL WHERE still goes in the SQL WHERE — keep it there. Mirror FTS's error propagation (fail closed).

- [ ] **Step 5: Write the local-DB test** (`hqlfilter_test.go`, same skip/harness as `query_test.go`/`search_test.go`). Seed a `LegalArticle` (tags `{domain:tax}`), an `ApiEndpoint` (tags `{domain:tax}`), a `ManualSection` (tags `{domain:pix}`). Build a closure from a real parsed HQL query to keep the test self-contained WITHOUT importing hql — define a tiny inline closure that returns a hand-written fragment, e.g.:
```go
f := Filter{Limit: 10, HQLWhere: func(start int) (string, []any, error) {
	return fmt.Sprintf("type = $%d", start+1), []any{"LegalArticle"}, nil
}}
hits, err := s.FTS(ctx, "pix", f)   // raw query "pix" would match all 3
require.NoError(t, err)
// only the LegalArticle survives the HQL predicate
```
Assert: (a) with the closure, only the `LegalArticle` id returns (no pgx "wrong number of parameters" error — the exact symptom a numbering bug causes); (b) a closure returning an error aborts (`FTS` returns that error, not an unfiltered result); (c) `Filter{HQLWhere: nil}` returns the same hits as before (byte-identical path). Repeat the (a) assertion for `Vector`.

- [ ] **Step 6: Run** against the local throwaway DB (`task testdb:up`; `$env:PIXKB_DSN` = `.env` `PIXKB_TEST_DSN`; `go run ./cmd/pixkb db up`; `go test ./internal/store/postgres/ -run 'HQLFilter|FTS|Vector' -v`; `task testdb:down`). Then `go build ./...`, `go vet`, `gofmt -l`, `golangci-lint` on the package.

- [ ] **Step 7: Commit.**
```bash
git add internal/store/postgres/search.go internal/store/postgres/vector.go internal/store/postgres/hqlfilter_test.go
git commit -m "feat(store): Filter.HQLWhere composes an HQL predicate into FTS/Vector"
```

---

### Task 3: `--where` flag + MCP `search` `where` field

**Files:**
- Modify: `cmd/pixkb/commands.go` (`newSearchCmd`), `internal/kbmcp/server.go` (`searchIn` + `registerSearch`)
- Test: `cmd/pixkb/commands_test.go` (or a new test file), `internal/kbmcp/server_test.go`

**Interfaces:**
- Consumes: `hql.Parse`/`Query.ToSQLAt` (Task 1), `Filter.HQLWhere` (Task 2).

- [ ] **Step 1: Read** `newSearchCmd` (how it builds `postgres.Filter` and runs `query.Hybrid`/`MultiHybrid`) and `registerSearch` in `server.go`.

- [ ] **Step 2: Write the failing cmd test:** `pixkb search "x" --where "type = ="` (bad HQL) makes the command return a non-nil error mentioning the parse failure, WITHOUT needing a DB (parse happens before store open). Mirror the existing `query_test.go` parse-error test.

- [ ] **Step 3: Implement the CLI flag.** Add `var where string` + `cmd.Flags().StringVar(&where, "where", "", "HQL predicate to narrow results before ranking, e.g. 'type = LegalArticle AND domain = tax'")`. In RunE, before opening the store: if `where != ""`, `q, err := hql.Parse(where)` (fail on error); pre-check lowering with `if _, _, _, err := q.ToSQLAt(hql.EvalContext{Now: time.Now()}, 0); err != nil { return ... }` (fail fast); then set
```go
f.HQLWhere = func(start int) (string, []any, error) {
	w, a, _, err := q.ToSQLAt(hql.EvalContext{Now: time.Now()}, start)
	return w, a, err
}
```
Import `pixkb/internal/hql`. Confirm `--where` composes with `--mode multi` (the same `f` flows into `MultiHybrid`'s subquery Filters — it should; note in the report if not).

- [ ] **Step 4: Implement the MCP field.** Add `Where string \`json:"where,omitempty" jsonschema:"optional HQL predicate to narrow results before ranking (e.g. type = LegalArticle AND domain = tax)"\`` to `searchIn`. In `registerSearch`, if `in.Where != ""`, parse + set `f.HQLWhere` the same way (return the parse/compile error as the tool error before searching).

- [ ] **Step 5: Write the MCP test** (DB-gated, in `server_test.go`): call the `search` tool with `where: "type = ManualSection"` and assert no error; optionally that a `where` that matches nothing returns zero hits without error.

- [ ] **Step 6: Run** `go test ./cmd/pixkb/ -run 'Search|Where' -v`, `go build ./...`, `go vet`, `gofmt -l`, `golangci-lint`; and the DB-gated MCP/search tests against the local throwaway DB.

- [ ] **Step 7: Commit.**
```bash
git add cmd/pixkb/commands.go cmd/pixkb/commands_test.go internal/kbmcp/server.go internal/kbmcp/server_test.go
git commit -m "feat(cli,mcp): search --where / search tool where — HQL-filtered ranking"
```

---

## Self-Review

- **Spec coverage:** Fork 1 (ToSQLAt offset) → Task 1; Fork 2 (Filter.HQLWhere closure, store stays hql-agnostic) → Task 2; Fork 3 (only WHERE composes; ORDER BY/LIMIT ignored in search) → the closure only wires ToSQLAt's `where`/`args`, dropping `order` (Tasks 2-3). Error handling (fail-fast parse, fail-closed closure) → Tasks 2 Step 5 + 3 Steps 2-3. `--as-of-*` AND-composition → inherited (HQL WHERE is appended after the as-of predicate). MCP surface → Task 3.
- **Injection invariant:** no task renumbers placeholder strings; all numbering is via `startArg`+`len(args)`. Task 2's test asserts the "wrong number of parameters" symptom is absent.
- **Zero-behavior-change:** nil `HQLWhere` path asserted byte-identical (Task 2 Step 5c); RRF/hybridCore never edited.
- **Type consistency:** `ToSQLAt(ctx, startArg)`, `Filter.HQLWhere func(int)(string,[]any,error)`, the closure signature, and `hql.EvalContext{Now}` are identical across tasks.
