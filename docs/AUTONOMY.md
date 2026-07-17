# pixkb Autonomy Charter
<!-- rev:004 -->

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
