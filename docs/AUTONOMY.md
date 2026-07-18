# pixkb Autonomy Charter
<!-- rev:009 -->

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
