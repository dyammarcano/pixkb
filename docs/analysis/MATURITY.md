# pixkb — Project Maturity Rating

**Type:** Go (single-language, task-runner: Taskfile.yml) · **Stage: 3 — Release-candidate** · **Weighted score: 77.3 / 100** · **Confidence: Medium** · **Date: 2026-07-18**

Air-gapped OKF knowledge base (Postgres + pgvector, hybrid FTS+vector search via RRF, bitemporal epoch engine, corral agent fleet) for the Brazilian Pix/SPB + Receita Federal tax domain. 27 packages, 14,457 prod LOC / 10,307 test LOC (test:prod 0.71), 220 commits (all within 30 days).

Confidence is **Medium**, not High, because the two load-bearing unknowns are structural: the crown-jewel DB path's test coverage is unmeasurable without live infra, and **CI has never actually executed** (no git remote, no tags, no runs).

## Scorecard

Points: A=100 · B=82 · C=65 · D=48 · F=25 (interpolated for ±). Weighted score = Σ(points × weight) / 35.

| # | Dimension | W | Grade | Evidence (measured unless noted) |
|---|-----------|---|-------|----------------------------------|
| 1 | Architecture & Boundaries | 3 | **A−** | 27 packages, 15 interface seams (`ingest.Source`, `rag.Generator`, `embed.Embedder`, …), layout guarded by `internal/layout_test.go`, build cycle-free. Caps: only 2 ADRs; god-file pressure (`cmd/pixkb/commands.go` 507 LOC). |
| 2 | Testing & Coverage | 5 | **B** | 467 test funcs; measured DB-free coverage: ingest 88.5%, similar 91.2%, brcode 88.3%, hql 86.6%, embed 86.0%, okf 85.7%, rag 75.3% — **but** evalkit 41.6%, cmd/pixkb 38.0%. `store/postgres`/`epoch`/`query`/`kbmcp` **N/A — DB-gated**; default CI runs `-short` and emits no coverage profile. |
| 3 | CI/CD & Release | 4 | **C** | 5-job workflow (`ci.yml`) + `.goreleaser.yaml` v2. **No git remote, no tags, no runs → CI never executed**; version `0.0.0-dev`; release manual-only; golangci `version: latest` + no `.golangci.yml`. |
| 4 | Security | 4 | **C** | SQL fully parameterized (whitelist cols/ops, positional `$N`); no zip-slip; PII redaction default-on; exec only `LookPath`. Caps: **no SAST/vuln/secret scan in CI**; weak default DSN `pixkb:pixkb` + `sslmode=disable` in `deploy/Dockerfile`; ISPB/econindex network egress vs air-gap. |
| 5 | Documentation | 2 | **A** | README (rev4, quickstart+source tables), ARCHITECTURE (mermaid), ROADMAP/BACKLOG/ISSUES/guides, 9 `doc.go`, rev-tag convention on 19 living docs, 24 superpowers specs/plans. Minor: no root CHANGELOG; 2 ADRs. |
| 6 | Operational Readiness | 4 | **B** | Config precedence (defaults<yaml<env<flags); ~78% `%w` wrap density; slog-JSON in MCP server; `search-health` readiness cmd; `pool.Ping`; strong air-gap deploy. Caps: **no signal handling/graceful shutdown**; no metrics/tracing; Debian base (not distroless/nonroot) + baked creds. |
| 7 | Code Quality & Tech Debt | 3 | **B** | 0 TODO/FIXME/HACK, 0 prod nolint (1 justified test nolint), prior golangci `0 issues`. Caps: lint non-reproducible; open dup DEBT-05/06; deprecated `currentTxPred` still used (DEBT-07). |
| 8 | Dependency & Supply-chain | 3 | **D** | go.sum lockfile; core deps fresh (pgx 5.10, cobra 1.10, excelize 2.11). **17 direct vs ~502 indirect**: `corral v0.1.1` (0.x) drags goreleaser + AWS/Azure/GCP SDKs + cosign + IPFS + kind + ko into the air-gap binary; duplicate majors (jwt4/5, yaml2/3/4). govulncheck N/A. |
| 9 | Stability & Change Mgmt | 3 | **B** | Strong discipline: BACKLOG(rev77)/ISSUES/ROADMAP cross-referenced with Shipped entries, deprecation policy, documented reversals (pkg/agents→corral, PDF attempt-1 revert). Caps: 220 commits 100% in 30 days (~7/day); zero tags/no SemVer; 1 Deprecated marker. |
| 10 | Correctness & Robustness | 4 | **B** | epoch write-path now mutex-serialized (race fixed), 1 prod panic, low genuine ignored-error density, good defer-Close cleanup, typed rate-limit. Caps: **Reindex TRUNCATE-before-ReadBundle** (no rollback); bitemporal fact defects (REVIEW.md HIGH); non-atomic epoch write; race detector never exercises the DB path. |

**Arithmetic:** (94·3)+(82·5)+(65·4)+(65·4)+(100·2)+(82·4)+(82·3)+(48·3)+(82·3)+(82·4) = 2704 ÷ 35 = **77.3 → Stage 3 (Release-candidate, 68–79)**.

## Ranked weak points (by leverage, not severity)

1. **CI has never run** — no remote/tags/runs; every execution-dependent signal is dark (CI/CD C).
2. **DB path coverage unmeasurable + no coverage profile in CI** — the highest-value packages skip without infra (Testing B, w5).
3. **No SAST/vuln/secret scanning** — 502 indirect modules and a 0.x linchpin dep unaudited (Security C, Dependencies D).
4. **Lint non-reproducible** — `version: latest` + no committed config (Code-Quality/CI/CD).
5. **Reindex truncate-before-read + bitemporal fact defects** — most *severe* correctness hazards, but isolated (low fan-out).
6. **corral dep bloat** — D-grade supply chain, but upstream-gated (L effort).

## Route — Stabilize → Harden → Mature

### The one thing: make CI real, then make it DB-backed
A two-link chain sits in front of ~half the graph:
> **git remote + first push (S)** → the 5 CI jobs execute for the first time → **add a `postgres:pgvector` service + drop `-short` + emit a coverage profile (M)** → true integration coverage (Testing) + race detector on the live write-path (Correctness) + a proven-green baseline to tag against (CI/CD, Stability).

Nearly every other fix either depends on CI running or only becomes *verifiable* once it does.

### Phase 1 — Stabilize
1. **Add git remote + push** — trigger the never-run CI. *S · unblocks Testing/Correctness/CI/CD/Security/Stability.* First action: `git remote add origin <url> && git push -u origin HEAD`.
2. **Ephemeral Postgres+pgvector in default CI** — services block + drop `-short` + `-race -coverprofile`. *M · unblocks integration coverage + DB-path race + green baseline.*
3. **Commit `.golangci.yml` + pin lint version** — end `version: latest` drift. *S · unblocks CI/CD reproducibility + Code-Quality.* (= improve-sweep plan 002.)
4. **Add govulncheck + gitleaks + gosec to CI** — first real read on the corral-dragged surface. *S · unblocks Security + Dependencies vuln signal.*
5. **Fix Reindex TRUNCATE-before-ReadBundle + epoch atomicity** — read/validate bundle before destructive DDL. *M · Ops recovery safety* (= improve-sweep plan 004).
6. **Fix bitemporal fact defects** (`fact.go` range close; stop sharing `createdAt`). *M · data integrity.*

### Phase 2 — Harden
1. **Graceful shutdown** — `signal.NotifyContext` → clean pgx teardown. *S.*
2. **Distroless/nonroot image + remove baked creds/`sslmode=disable`**. *M · Ops+Security.*
3. **Coverage to target on now-measurable packages** (the DB-gated four + evalkit/cmd outliers). *M.*
4. **Clear DEBT-05/06/07 + the `!= http.ErrServerClosed` → `errors.Is` slip**. *S.*
5. **File the corral dep-hygiene split upstream** (build-tag the release tooling). *L · upstream-gated → scheduled here, not front-loaded.*

### Phase 3 — Mature
1. **Cut first tagged `v0.x` + root CHANGELOG + tag-triggered goreleaser**. *S (needs P1 green).*
2. **Write missing ADRs** (corral migration, HQL DSL, epoch/as-of). *M.*
3. **Split `cmd/pixkb/commands.go` (507 LOC)** along command groups. *S.*

---
*Severity ≠ leverage: the Reindex/bitemporal fixes (P1.5/1.6) are the worst correctness hazards but each unblocks little, so they sit below the reproducible-lint and SAST fixes, which each unblock two dimensions for Small effort. Analysis + planning only — no source was edited by this rating.*
