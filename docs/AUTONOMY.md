# pixkb Autonomy Charter
<!-- rev:015 -->

Standing authority for autonomous roadmap execution, granted by the operator via
`/steps:autonomous` on 2026-07-17. This is the durable, auditable record of the
envelope the operator approved once; it governs all subsequent hands-free work
until amended.

## Standing authority

Drive the project roadmap to completion **until blocked or exhausted** (whole
roadmap → backlog, in priority order, unblockers first). Per phase, run the full
cycle continuously without per-step approval:

spec → self-review → plan → SDD (implementer + task review + fix loop) →
whole-branch review → independent green verify → merge → docs sync → next phase.

**Design forks:** decide autonomously, pick the recommended option, and **log it**
(spec "Settled forks" section + the Decision Log below). Do not pause for forks —
the operator reviews decisions in batch and can reverse any later.

**Check-in cadence:** report at each **phase boundary** (what shipped, what the
reviews caught, what's next). Otherwise silent.

## Guardrails — allowed WITHOUT asking

- **Merge** feature branches to `master` (no-ff), after whole-branch review +
  independent green verify (build + tests + lint/vet).
- **Run/smoke-test locally** against the throwaway DB (localhost:5433,
  `task testdb:up`/`testdb:down`).
- **Mutate the PROD KB** (`192.168.15.100`) — explicitly granted 2026-07-17.
  Still validate every prod-mutating operation on the local throwaway DB FIRST
  (unchanged good practice), then apply to prod. This lifts the earlier
  session-long "never touch prod without asking" rule.
- **Push** to the authorized git remote. NOTE: this repo currently has **no
  remote**; push is a no-op until the operator adds one. Do NOT invent/add a
  remote autonomously — that is publishing to an unapproved location.

## Guardrails — NEVER without explicit say-so

- Rewrite or delete git history; force-push; `reset --hard` a shared ref.
- Delete or overwrite operator data/files this work did not create (esp. the
  `%LocalAppData%\PixKB\mirror\` operator sources and the `kb-data`/bundle repo).
- Publish anywhere but an operator-authorized remote; add a new git remote.
- Spend money / call paid-cloud services.
- Change the project LICENSE or any safety invariant below.
- Anything hard to reverse and unauthorized.

## Project safety invariants (must survive every change)

- **Air-gap:** every `ingest.Source` reads local offline files only — never the
  live network at ingest time.
- **CRLF landmine:** LF line endings only; stage explicit paths — NEVER
  `git add -A`/`.`/a directory (a careless add bakes CRLF into history and wipes
  blame). `.gitattributes` enforces `* text=auto eol=lf`.
- **Bundle is source of truth:** `kb-data/` (a separate git repo) is canonical;
  `reindex` truncates and rebuilds from it. DB-only concepts are orphans.
- **Currency:** BRL money is Decimal, never `float64`.
- **No AI attribution** in commits; use conventional commits; git user.name/email.
- **Scripts-first (operator convention):** run CLI work from `.scripts/`
  (gitignored) with the `{NUM}-{LETTER}_{verb}_{target}.ps1` naming; never inline.
- **No secret leaks** to child process env or committed files.

## Stop conditions

- A genuine blocker that can't be resolved (missing capability/source file, a
  gate that won't go green, an ambiguity that changes *product direction*).
- An action would cross a NEVER guardrail or a gated action not authorized above.
- The scope is satisfied (roadmap + backlog exhausted).

## Decision Log (newest first)

- 2026-07-18 — **Harden-tier quality batch shipped to master** (merge `91881e9`),
  via `/steps:next` `all` (8 items: the improve-sweep vetted list + maturity
  Harden). Closed SECURITY-04 (no_pii_filter now server-gated behind
  `--allow-pii-bypass`, fan-out clamped), TEST-02/03/04 (extracted `buildFTSWhere`,
  the evalkit invariant checks, and buildSources into pure DB-free unit tests),
  PERF-02 (MultiHybrid subqueries now concurrent, fusion byte-identical), and
  DEBT-05 (epoch write-seq dedup into applyAll/finishEpoch). **PERF-01 done in
  part** — embedding is batched into one `Emb.Embed` call, but the per-concept DB
  round trips are **deferred** (measure-first: batching them touches the
  bitemporal RecordFact and the win is remote-DB-latency only; logged BACKLOG P3).
  Coverage on the two low outliers rose (evalkit 41.6→49.2%, cmd 38.0→41.3%); the
  80% target stays gated on the DB-in-CI harness (blocked on billing). Whole-branch
  review (opus) READY, no findings — every refactor confirmed behavior-preserving.
  Verified `-race` against a live pgvector DB; the lone `-race` failure was the
  `-short`-gated live-agent curate E2E (unrelated, CI-skipped). The doc-derived
  non-blocked backlog is now essentially exhausted — what remains is
  operator-blocked (CI billing, mirror PDFs, English policy) or upstream (corral
  dep split).
- 2026-07-18 — **Maturity-route Stabilize/Harden batch shipped to master** (merge
  `b47c955`), via `/steps:next` `all` after the `/project:rating` produced
  `docs/analysis/MATURITY.md` (Stage 3, 77.3/100). Ten route items in one branch,
  verified under `-race` against a live throwaway pgvector DB (localhost:5433):
  embed guards, hit-mapping dedup + deprecated-symbol removal, graceful shutdown,
  a security-scan CI job (govulncheck 0 reachable / gitleaks 0 with a doc-prose
  allowlist), Reindex read-before-truncate, prompt-injection fencing, sources.go
  extraction, and ADRs 0003-0005. Key judgment calls: **item 1 (bitemporal fact
  defects) was a CHECKOFF** — the current `fact.go` already implements exactly
  REVIEW.md's recommended fix (REVIEW.md reviewed a pre-fix revision; cited a
  line the 56-line file doesn't have), so REVIEW.md was marked resolved, not
  re-implemented; **item 8 (distroless) was NOT-APPLICABLE** — the Dockerfile is
  a deliberate all-in-one pgvector air-gap image (can't be distroless), so the
  DSN's loopback posture was documented instead of forcing a design-breaking
  change; **DEBT-05 (epoch write-seq dedup) and the Reindex atomic-swap were
  deferred** (measure-first — they overlap correctness-critical paths). The
  whole-branch review caught one Important prompt-injection bypass (a split forged
  envelope marker defeats single-pass ReplaceAll) — fixed pre-merge with
  fixed-point neutralization + regression tests. Remaining route work is
  operator/upstream-blocked: CI green baseline (**GitHub account billing lock** —
  every Actions run startup-fails account-wide, public or private), Postgres-in-CI
  coverage (needs Actions), corral dep split (upstream), `v0.x` tag (needs CI).
- 2026-07-18 — **corral agent-fleet integration hardening shipped to master**
  (merge `fb061bf`), after an operator-requested maturity assessment of the
  corral integration (graded B−: clean boundary, but immature on dependency
  version/footprint + runtime resilience + concurrency). Executed the two
  actionable maturity levers as plans 006+007: (006) map `corral.ErrRateLimited`
  to a typed `rag.ErrRateLimited` (double-`%w` so both causes stay in the chain),
  proactively short-circuit on a provider-exhausted `LimitStatus`, and surface a
  friendly "try again later" in the ask CLI + kb_ask MCP — **deliberately no
  auto-retry** (corral exposes no reset window, so a within-request backoff would
  just spin; deferred until a reset time is available); (007) serialize
  `Runner.Run/UpsertBatch/Reindex` with an unexported mutex, closing the
  CORRECTNESS-02 concurrent-`concept_upsert` epoch/git race the audit found, with
  a `-race` DB-gated concurrency test. Whole-branch review (opus) READY, no
  Critical/Important. Deferred maturity items (out of scope, logged): the L-effort
  corral dependency-graph split (DEPS-01, needs upstream) and gating the live
  agent e2e in CI. The two remaining improve quick-win plans (002 golangci pin,
  003 embed guards) + 004/005 still await a go-ahead.
- 2026-07-18 — **`improve` maturation sweep + top-plan execution** (via
  `/steps:next` `all`, items 1-4). Ran a standard-depth `improve` audit (4
  parallel read-only auditors: correctness/security/tests+debt/perf+deps+dx),
  vetted findings against the code, and wrote `plans/` (README index + 5
  executor-ready plans + a vetted-but-unplanned backlog). **Shipped the P1 fix
  (merge `5347423`):** the RAG answer cache was keyed only by (question, epoch),
  so a `no_pii_filter=true` call cached un-redacted PII a later normal call was
  served — a real LGPD leak whose own doc comment claimed the opposite. Fixed:
  scope-aware `cacheKeyFor` + never cache when `NoPIIFilter`. Whole-branch review
  caught a missing `MaxChars` key field (scope collision) — fixed pre-merge.
  **Reconciled the other `/steps:next` items:** item 3 (doc refresh) shipped
  (README `818926a` + ARCHITECTURE `b4e7632`); **item 4 (vector-score floor) was
  already implemented** (`vecScoreFloor=0.05` in `hybrid.go`) — checkoff, no new
  code; **item 2 (basename-slug collision) deliberately NOT rushed** — the sweep
  showed the real failure mode is a *loud `GatherAll` dup-id abort*, not the
  silent overwrite the backlog implied, and the proper fix (source-type ID
  namespacing) is a bundle-ID migration under the deprecation policy, so it was
  re-scoped in BACKLOG rather than hacked in-place. The remaining 4 plans
  (golangci pin, embed guards, Reindex failure-safety, prompt-injection
  hardening) + the vetted backlog await a go-ahead. Audit surfaced no live
  secrets and confirmed the HQL/SQL layer is fully parameterized.
- 2026-07-18 — **Ingest-layer hardening batch shipped to master** (merge
  `2d28e5b`), via `/steps:next` `all` (items 1-6). Implemented test-first
  directly (small, self-contained, single-subsystem) with an opus whole-branch
  review as the gate — READY, no Critical/Important. Key judgment call on item 6
  (reArt robustness): implemented only the **unambiguous** wrapped-`Art.`
  line-join and **deferred running-header stripping** to real-PDF validation,
  because the backlog explicitly warns against tuning statute markers blind (no
  LC 214 PDF on the machine — Phase B validation stays operator-blocked) and
  attempt-1 of the PDF fix regressed exactly by over-tuning. Two review Minors
  (joinWrappedArt prose false-positive; duplicate "Overview" titles — IDs stay
  unique via index prefix) documented, no change needed. Docs synced
  (BACKLOG/ISSUES items marked done). This clears the non-blocked polish tier;
  remaining substantial work stays operator-blocked (mirror PDFs / English
  decision).
- 2026-07-18 — **docx + xlsx ingest sources shipped to master** (merge `2058f19`).
  3-task SDD: docx source (`95a7235`), xlsx source + excelize dep (`949aac9`),
  config wiring (`7e8ee68`). Whole-branch review (opus) returned Not-READY on one
  Important — xlsx concept IDs lacked the index prefix docx/markdown use, so
  sheet names that slugify identically (or to "" for all-non-ASCII names) would
  collide and silently overwrite on upsert. **Fixed pre-merge** (`302a950`,
  index-prefixed ID). 3 Minors (docx table-cell text dropped; empty-heading
  merge; pre-existing basename-slug collision inherited from markdown) logged to
  BACKLOG P3 as measure-first completeness polish, not fixed blind. excelize
  v2.11.0 confirmed pure-Go + local-file-only (air-gap invariant holds). This
  completes the operator's md/pdf/docx/excel-parser directive. Remaining
  substantial roadmap work stays operator-blocked (Phases C/D + Phase B real-PDF
  validation need mirror-dir PDFs; the English-standardization item needs a
  product decision).
- 2026-07-18 — **New operator directive: docx + xlsx ingest sources.** md/pdf
  already exist, so scope is the two missing formats (operator-confirmed). Parse:
  `github.com/xuri/excelize/v2` for xlsx (pure-Go, air-gap-safe) + stdlib
  `archive/zip`+`encoding/xml` for docx (operator-confirmed). Decided: both emit
  `Reference` concepts (no new Type), domain via the default `tagDomain` backfill,
  config as plain `[]string` lists like `pdfs:`/`markdown:`. First new third-party
  dependency added this session (excelize) — justified by the operator's explicit
  approve of the lib approach; it is pure Go and reads only local files. Spec:
  `docs/superpowers/specs/2026-07-18-docx-xlsx-ingest-design.md`.
- 2026-07-18 — **PDF TOC-suppression fix shipped to master** (merge `4c62a6d`).
  Measured on the real manual: ~93 mostly-junk sections → 21 clean, 0 TOC-junk
  titles. Key decision: **descoped Task 3 (body numbered-heading join) after
  measurement** — Task 2 (TOC suppression) alone fixes the reported bug, and the
  residual body-quality issues it exposed (repeated `DIAGRAMA DE ESTADOS` caption,
  an OCR typo, example-data heading) are content-quality work for the curate/
  hygiene agent fleet, not more extractor heuristics (which is what regressed in
  attempt 1). This is the measure-first discipline the ISSUES warning demands.
  Residual items + 3 whole-branch-review Minors logged to ISSUES/BACKLOG.
- 2026-07-17 — **Picked up the PDF TOC-suppression fix** (last substantial
  non-blocked item; ROADMAP 0-9 all done, this is the open ISSUES.md content-
  quality bug). Attempt 1 (dropcap-rejoin) net-regressed; grounded a fresh design
  in the real extracted text (`.superpowers/research/pdf-junk-title-analysis.md`).
  **Forks settled:** (1) suppress the whole `Sumário`-delimited TOC block rather
  than un-mangle its fragments (the junk + duplicates share that single source);
  (2) detect TOC end by dot-leader density gap (dot-leaders occur ONLY in the TOC),
  not a line count; (3) add grounded body numbered-heading detection (number-line +
  empty-separator title join — the `QR` 2-letter case that broke attempt 1 joins
  correctly), keeping the existing ALL-CAPS path; (4) written generically (no-ops
  on a `Sumário`-less PDF). Deterministic DB-free acceptance gate (inspect
  `NewPDFSource(manual).Fetch` titles). Spec:
  `docs/superpowers/specs/2026-07-17-pdf-toc-suppression-design.md`.
- 2026-07-17 — **HQL v2 (`search --where`) shipped to master** (merge `45afc89`),
  after the user said "keep going" past the earlier saturation point. This was the
  last substantial non-blocked item — folding the HQL filter into ranked search.
  Since it touches the core search SQL (FTS/Vector), decided to (a) design the
  injection-sensitive placeholder-composition fork up front (offset-numbered
  `ToSQLAt`, never string-renumber; store-agnostic `Filter.HQLWhere` closure),
  (b) run the 3 tasks strictly sequentially with an opus review of the FTS/Vector
  crux, (c) live-validate end-to-end on a local DB before merge. Also shipped this
  session after the checkpoint: MCP `query` verb (`bd88fb7`), HQL golden-test gaps
  (`309e23e`), domain-validation P3 (`45b0e34`), and closed two stale-doc items
  (CLI `--format`, similar-eval). Remaining non-blocked work is now genuinely
  minor/deferred; substantial work is operator-blocked (Phases C/D + PDFs; English
  policy decision).
- 2026-07-17 — **Autonomous run reached saturation; stopping to check in.** After
  shipping Phase B, HQL v1, and the domain-validation P3 (`45b0e34`), every
  high-value non-blocked roadmap item is done. What remains is either blocked on
  the operator (Phases C/D + Phase B real-PDF validation need mirror-dir PDFs; the
  "KB standardized in English" item needs a product-direction decision), or
  low-value/deferred polish (remaining `--format` commands; HQL v2 follow-ups just
  logged). Per the charter's stop conditions (a blocker + product-direction
  ambiguity), stopping here and surfacing rather than grinding deferred polish.
  Resuming continues from the ledgers + `git log`.
- 2026-07-17 — **HQL DSL v1 shipped to master** (merge `7ad5233`). Built via SDD
  (6 TDD tasks + fixes). Forks settled autonomously in the spec (standalone filter
  not RRF-folded; `Match` deferred; `epoch`/`updated`→real columns; no MCP verb in
  v1). Whole-branch review confirmed zero injection surface and caught one must-fix
  (`~`/`!~` was exact-match not substring) — fixed pre-merge (`46ab4b1`). Also
  fixed a Task-5 nullable-column scan bug pre-merge. v2 follow-ups (RRF fold-in,
  MCP verb, Match, bitemporal field, test-coverage minors) logged to BACKLOG.
- 2026-07-17 — **Phases C/D blocked → pivot to HQL (P2).** A read-only source
  inventory found no offline BACEN split-payment / Pix-rail material on the
  machine (nav-boilerplate only; the calculadora backend is the tax calculator
  with no split component; the SPI/LC-214 PDFs were never downloaded). Per the
  charter scope (whole roadmap *until blocked* → skip the blocked item, continue),
  decided to **pivot to the HQL structured-query DSL (BACKLOG P2)** — the
  highest-priority non-blocked item, fully self-contained (a port of herald's
  `internal/hql`, no external sources), and it now has real Phase A/B data to
  query. Logged the exact missing files in BACKLOG so the operator can unblock
  C/D by dropping PDFs in the mirror dir; I'll resume C/D when they appear.
- 2026-07-17 — **Phase B shipped to master** (merge `c1b7fa8`). Whole-branch
  review returned READY-WITH-MINORS; decided to **fix findings 1+3 pre-merge**
  (ANEXO false-positive guard + zero-article warning — they affect the real-PDF
  ingest this phase exists for) and **defer findings 2+4 to backlog** as P3
  real-PDF-validation follow-ups (tuning marker tolerance blind, with no LC 214
  PDF on the checkout, would risk false positives). Also fixed a `strings.SplitSeq`
  lint finding the per-task gate missed.
- 2026-07-17 — **Charter committed on the `feat/kb-scope-phase-b` branch** rather
  than dancing back to `master` mid-SDD; it rides into `master` with the Phase B
  merge. Low-cost, auditable; avoids a disruptive branch switch during an active
  subagent-driven build.
- 2026-07-17 — **Phase B forks settled** (in the committed spec
  `2026-07-17-kb-scope-expansion-phase-b-design.md`): LegalArticle as a new Type
  string (not a schema migration); statute-aware sectioner reusing extractPDFText;
  structural position as namespaced tags; article IDs zero-padded to 4 digits;
  Parágrafo único folded into its article; one Reference per Anexo; committed
  text fixture for the automated gate + a manual real-PDF recipe.
