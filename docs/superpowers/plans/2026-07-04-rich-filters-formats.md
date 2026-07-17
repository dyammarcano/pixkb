# Rich Search Filters and Output Formats Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development.

**Goal:** Feature 4 of `docs/SEARCH-CAPABILITY-SPEC.md`, scoped to its highest-value, lowest-risk subset: (a) a shared `--format text|json|md|yaml` output flag for `pixkb search`, (b) CLI/MCP wiring for the as-of filters (`AsOfEpoch`/`AsOfTime`) that already exist on `postgres.Filter` but aren't exposed as flags today. Include/exclude-id-lists and a min-vector-score filter are NOT in `postgres.Filter` today and would need Postgres/SQL surgery — explicitly out of scope, backlogged.

**Architecture:** New `internal/output` package: `func Render(format string, hits []postgres.Hit) (string, error)` — `text` (today's plain columns), `json` (indent-2 array), `md` (a markdown table), `yaml` (using `gopkg.in/yaml.v3`, already a dependency). `pixkb search` gets `--format` (default `text`) and `--as-of-epoch`/`--as-of-time` flags populating `postgres.Filter`. MCP `search` tool gets `as_of_epoch`/`as_of_time` fields (JSON is already MCP's native format, so no format param needed there).

## Global Constraints

- Go 1.25.0, `CGO_ENABLED=0`.
- Default behavior (no `--format`) must be byte-identical to today's plain-text output.
- `internal/output` must not touch `internal/query`/`internal/store/postgres` ranking or filtering logic — purely a rendering layer over already-computed `[]postgres.Hit`.
- `--as-of-time` accepts RFC3339; `--as-of-epoch` an integer; both optional, mutually exclusive with each other (same as `postgres.Filter`'s existing pointer-based either/or).

---

### Task 1: `internal/output` package — Render for all 4 formats

**Files:** Create `internal/output/output.go`, `internal/output/output_test.go`.

- [ ] Implement `func Render(format string, hits []postgres.Hit) (string, error)`:
  - `"", "text"`: same format `pixkb search` prints today: `fmt.Sprintf("%2d  %-34s  %s\n", h.Rank, h.ID, h.Title)` per hit, concatenated.
  - `"json"`: `json.MarshalIndent(hits, "", "  ")`.
  - `"md"`: a markdown table with header `| rank | id | title | type | score |` and one row per hit.
  - `"yaml"`: `yaml.Marshal(hits)` (`gopkg.in/yaml.v3`, already in go.mod).
  - Unknown format: return an error `fmt.Errorf("output: unknown format %q (want text|json|md|yaml)", format)`.
- [ ] Tests: one per format asserting exact/structural output for a 2-hit fixture; one for the unknown-format error.
- [ ] `go test ./internal/output/... -v`, `go build ./...`, `go vet ./...`.
- [ ] Commit `git add internal/output/output.go internal/output/output_test.go && git commit -m "feat: add internal/output package (text/json/md/yaml rendering)"`.

---

### Task 2: CLI — `pixkb search --format` + `--as-of-epoch`/`--as-of-time`

**Files:** Modify `cmd/pixkb/commands.go` (`newSearchCmd`).

- [ ] Add `--format` (string, default `"text"`), `--as-of-epoch` (int, 0 = unset), `--as-of-time` (string RFC3339, "" = unset) flags. Populate `postgres.Filter.AsOfEpoch`/`AsOfTime` (both `*int`/`*time.Time` — only set the pointer when the flag's zero-value check says the user actually passed it; if both are set, return an error "set only one of --as-of-epoch or --as-of-time"). Replace the final per-hit print loop with a call to `output.Render(format, hits)` printed via `fmt.Fprint(cmd.OutOrStdout(), rendered)` — for `format=="text"` (or unset) this must produce byte-identical output to today's loop (verify: `Render`'s text branch uses the exact same format string the current loop uses).
- [ ] No new test (needs live DB, matches convention). Build+vet; smoke-test manually if a DB is reachable: `--format json`, `--format md`, `--format yaml`, and no `--format` (confirm byte-identical to pre-change).
- [ ] Commit `git add cmd/pixkb/commands.go && git commit -m "feat: add pixkb search --format and --as-of-epoch/--as-of-time"`.

---

### Task 3: MCP — as-of fields on the `search` tool + backlog

**Files:** Modify `internal/kbmcp/server.go` (`searchIn`); `docs/BACKLOG.md`.

- [ ] Add `AsOfEpoch *int `json:"as_of_epoch,omitempty"`` and `AsOfTime string `json:"as_of_time,omitempty"`` (RFC3339 string, parsed to `*time.Time` inside `registerSearch` — return a tool error on unparseable input) to `searchIn`. Populate `postgres.Filter.AsOfEpoch`/`AsOfTime` from them the same either/or way as Task 2's CLI.
- [ ] Add `TestServerSearch_AsOfEpoch` mirroring the existing DSN-gated test pattern in `server_test.go`.
- [ ] Backlog (P2, `docs/BACKLOG.md`): include/exclude concept-id and concept-type lists, and a minimum-vector-score filter — none exist on `postgres.Filter` today; adding them needs new SQL predicates in `FTS`/`Vector`/`Hybrid`/`MultiHybrid`, a bigger unit of work deliberately deferred here. Also backlog: HTTP `/search` format param (this plan only touches CLI+MCP, not `pixkb serve`).
- [ ] `go test ./internal/kbmcp/... -run 'TestServerReadTools|TestServerSearch_AsOfEpoch' -v` (PASS or SKIP consistently), `go build ./...`, `go vet ./...`.
- [ ] Commit `git add internal/kbmcp/server.go internal/kbmcp/server_test.go docs/BACKLOG.md && git commit -m "feat: add as-of filters to search MCP tool; backlog remaining Feature 4 scope"`.
