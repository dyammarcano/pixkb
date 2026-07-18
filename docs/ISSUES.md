# pixkb Known Issues & Limitations
<!-- rev:016 -->

Current known limitations.

## Content quality
- ~~**PDF table-of-contents entries leak in as junk `ManualSection` titles
  (51 flagged by `hygiene`), plus ~27 duplicate titles.**~~ **RESOLVED (2026-07-18,
  merge `4c62a6d`) — via TOC-region suppression, NOT dropcap-rejoin.** A fresh
  design grounded in the real extracted text
  (`.superpowers/research/pdf-junk-title-analysis.md`) found that dot-leader lines
  (`^\.{4,}$`) occur ONLY in the TOC and the whole TOC is one `"Sumário"`-delimited
  block — so `internal/ingest/pdf.go`'s new `stripTOCRegion` drops that block
  wholesale before `splitSections`, rather than trying to un-mangle the dropcap
  fragments (attempt 1's approach, which broke on 2-letter abbreviations like
  `QR`). **Measured on the real manual: ~93 mostly-junk sections → 21 clean, with
  ZERO dot-leader/bare-page-number/dropcap-artifact titles** (DB-free acceptance
  gate `TestPDFFetch_NoTOCJunk`); TOC-vs-body duplicates are eliminated by
  construction (the TOC copy is gone). Generic (no-ops on a `Sumário`-less PDF, so
  non-manual sources are unaffected); whole-branch review READY. Spec/plan:
  `docs/superpowers/{specs,plans}/2026-07-17-pdf-toc-suppression*`.
  **Residual body-content-quality follow-ups (P3, exposed once the TOC junk was
  cleared — a DIFFERENT problem, best handled by the curate/hygiene agent fleet,
  NOT more extractor heuristics):** (i) the numbered/caption body-heading join was
  measured net-neutral for the extractor and DESCOPED — a generic body caption
  `"DIAGRAMA DE ESTADOS"` repeats 8× above distinct state-diagram sections (the
  specific labels like `"COBR PAGA NA DATA NORMAL"` are their own sections);
  joining the caption with its label would give unique titles; (ii) an OCR typo in
  the PDF text layer (`"DRIAGRAMA DE ESTADOS"`); (iii) example data promoted to a
  heading (`"FULANO DE TAL EIRELI"`); (iv) `stripTOCRegion` hardening — a stray
  body dots-only line within 40 lines of the TOC could in theory extend the region
  (low likelihood, gap-bounded), and an accentless `"Sumario"` heading would no-op
  (safe degrade). Historical root-cause detail retained below.
- **(historical) PDF TOC junk root cause.** Root-caused 2026-07-17.
  The BCB manual's main TOC and per-chapter mini-TOCs are extracted by
  `internal/ingest/pdf.go`, and the PDF renders each TOC entry with **every
  word's first letter split onto its own line** (a small-caps/dropcap style):
  `"3.2." / "S" / "ERVIÇO DE " / "I" / "NICIAÇÃO DE " / "T" / "RANSAÇÃO DE " /
  "P" / "AGAMENTO"` then a dot-leader line (`"........"`) and a page number
  (`"38"`). `splitSections` treats each fragment line as its own heading, so a
  single TOC entry yields several mangled all-caps fragment titles missing their
  first letter (`ERVIÇO DE`, `ECOMENDAÇÕES DE SEGURANÇA`, `ODE ESTÁTICO PARA
  PACS`). Because every section appears in both the TOC and the body, this also
  produces the duplicate-title findings.
  **Fix design (spec-worthy — regression risk to the 93 real sections):**
  (1) a text-normalization pass that rejoins a lone single-uppercase-letter line
  with the following continuation (`"S"+"ERVIÇO DE "` → `"SERVIÇO DE "`) and
  merges consecutive uppercase continuation lines into one heading;
  (2) detect and drop TOC regions — an entry whose reconstructed heading is
  followed by a dot-leader line + bare page number is a TOC entry, not content;
  (3) re-ingest (now runnable, `08b7ec0`) and diff section count/titles before/
  after to prove the real sections survive. Do NOT ship a partial heuristic:
  rejoining dropcaps without merging + TOC-skip would MULTIPLY the fragment
  headings, not remove them. `hygiene` already flags the symptoms; the extractor
  is the fix site.
  **Attempt 1 (2026-07-17) — reverted, findings recorded.** Implemented dropcap
  rejoin + uppercase-fragment merge + bare-number attach + drop sections whose
  body is only a dot-leader+page-number (`isTOCBody`). Re-ingested the real PDF
  locally. Result: titles DID reconstruct correctly (`MAPEAMENTO DA RESPOSTA A UM
  CANCELAMENTO...`, `SERVIÇO DE INICIAÇÃO...` — the core mangling is fixable), but
  overall a NET REGRESSION by the metrics (junk-title 51→68, duplicate 27→46,
  ManualSection 93→96), so it was reverted. Three concrete gaps the next attempt
  must close: (1) **incomplete merges** — `QR CODE ESTÁTICO` still lost `QR`
  (some fragments don't sit consecutively as modeled); (2) **page-number
  mis-attach** — the number-attach grabbed page numbers (`65 ANEXO II`), so only
  attach a preceding number if it is a real section number (has a dot, e.g.
  `3.2.`, not a bare page number); (3) **TOC entries still duplicated the body**
  — `isTOCBody` didn't catch every TOC entry, so dedup needs a stronger signal
  (e.g. suppress the whole front-TOC region, and drop a title that also appears
  as a body section). Also note `hygiene`'s junk-title check flags legitimate
  ALL-CAPS headings (the manual really uses them), so it over-counts — the real
  target is correct/de-duplicated titles, not zero all-caps. The PDF's text layer
  is chaotic (per-word dropcaps, interleaved dot-leaders, 25k fragment lines):
  budget for iterative measurement against a re-ingest diff, not a one-shot fix.

## Search quality
- **FTS recall arm ANDs every query word (`websearch_to_tsquery`) — defeats
  synonym recall.** The WHERE gate is `fts @@ websearch_to_tsquery('pixpt', $1)`,
  and websearch ANDs all content words. So a natural-language query
  (`estornar um pix recebido por engano`) only matches a concept that contains
  EVERY word — and (a) `pixpt` has NO stemmer, so `estornar` ≠ the indexed
  `estorno`, and (b) layperson filler like `engano` is in no concept. Result: the
  whole match zeroes regardless of how good `intent_terms` is. PROVEN: with the
  full 207/207 `intent_terms` rollout done, `search --mode fts "estornar um pix
  recebido por engano"` returns ZERO, yet `"estorno pix recebido"` (all three in
  the devolução concept's `intent_terms`) ranks it **#1** — the lever works, the
  query semantics defeat it. The fuzzy judge is consequently FLAT pre/post rollout
  (rel 1.95→2.05, prec 1.13→1.00). The naive OR-recall fix (rewrite the tsquery
  `&`→`|`) was TRIED and MEASURED WORSE on the deterministic harness
  (`eval/tophit.sh`): fuzzy top@5 41%→24%, MRR 0.285→0.162, precise flat — OR
  floods the FTS arm and length-normalized `ts_rank_cd` floats short junk, which RRF
  then dilutes the target down. A COVERAGE-ranked OR (length-independent distinct-
  term sort) WAS then tried: it fixed the FTS arm (fts-only fuzzy 0.285→0.313,
  pacs.008/camt #1) and a FTS arm-weight ×2 lifted PRECISE (0.698→0.745, top@5
  100%) — but HYBRID fuzzy stayed pinned at top@5 41% (MRR 0.274–0.284) in every
  config. So the fuzzy ceiling is NOT FTS recall: the residual misses are (1) en/pt
  config mismatch (coverage uses each doc's own language config — a pt query vs an
  EN pacs/camt concept tokenizes apart), (2) the weak hashing-vector arm (can't pull
  a target into top-5; air-gap forbids a learned embedder), (3) a few missing terms.
  ALL reverted to committed AND-recall (a core-ranking change that moved only
  precise, not the fuzzy goal, must not ship on 33 curated cases without the full
  judge). The WIN came from the OTHER arm: the vector embedder was fed
  `Title + Body` only, IGNORING intent_terms. Folding intent_terms INTO the
  embedded text (`epoch/runner.go`) lets the hashing bag-of-words cosine match a
  paraphrase query against the recall synonyms — and since it touches only the
  down-weighted/floored vector arm (NOT the FTS/precise-ranking SQL) it lifted BOTH
  axes on the broadened harness: precise MRR 0.788→0.821 (top@5 held 100%), fuzzy
  top@5 41%→53% / MRR 0.285→0.303. KEPT (reindexed prod). Residual fuzzy gap is
  still the hashing embedder's weak semantics + a few term gaps; a learned embedder
  is air-gap-forbidden. Measure any attempt with `eval/tophit.sh` on
  `cases-precise-ids.tsv` (guard, top@5 must stay 100%) + `cases-fuzzy-ids.tsv`.
- ~~**Generated `fts` uses `'simple'` — Portuguese stopwords kill recall.**~~
  RESOLVED (migration 0003, `pixpt`). The `'simple'` config kept stopwords, so
  "estorno **de um** pix" made `de`/`um` required AND-terms and zeroed recall. A
  blanket `'portuguese'` config was tried and reverted — its snowball STEMMER
  regressed precision (judge 4.42/3.00 vs 4.92/3.92). The fix is a CUSTOM config
  `pixpt` = `pg_catalog.simple` template + Portuguese STOPWORDS, NO stemmer:
  stopwords drop, precise terms are NOT stemmed. The generated `fts` column and
  the search query both use `pixpt`. Validated: all 12 judge-query top-hits stay
  correct (the codex judge is too noisy at 12 cases to detect the change — a
  no-stopword query like `chave-tipos` swung in score despite identical results),
  and stopword-heavy natural queries now recall (`o que é o webhook de notificação`
  → the webhook concept). The intent_terms rollout sits on top of this.
- **Hashing embedder has weak semantic recall.** The default embedder is a
  hashing vectorizer; it under-performs on paraphrase / semantic-match queries,
  and its vectors are near-random for unseen vocabulary — actively noisy in the
  hybrid vector arm. The arm is down-weighted to 0.5 in RRF
  (`internal/query/hybrid.go`) so strong lexical / title matches win. The
  air-gap-compliant fix is agent-driven recall (the agy fleet enriches concepts
  + curates ranking over pixdb) plus a vector-score floor — NOT a learned-model
  embedder, which would need a metered API or native runtime (BACKLOG P2). The
  recall LEVER now exists: an FTS-woven `intent_terms` field (ADR 0001 / migration
  0002); the enrichment pilot + rollout that fill it are pending a database.
- **RRF discards score magnitude** (mitigated). Reciprocal-rank fusion ranks by
  position, not score, so a near-exact FTS title match only barely outscores a
  weak one. **Mitigated** by the title-overlap boost (`internal/query/hybrid.go`
  `titleBoost`, `90bebdd`): a concept's fused score is multiplied by the fraction
  of distinct query tokens its title covers (bounded ≤1.5, never penalising), so
  a title-intent match wins. "prazos de implementação" and "lote de cobranças com
  vencimento" now top-hit correctly. Residual: a junk concept with overwhelming
  lexical body mass can still outrank a richer one beyond any bounded boost — the
  remedy there is removal (`pixkb concept rm`), not more ranking weight.

## Testing
- **Integration tests need their OWN throwaway Postgres database.** They
  truncate/drop tables, so `PIXKB_TEST_DSN` must point at a separate database
  (e.g. `pixkb_test`), NEVER the production KB (`PIXKB_DSN`). A guard now
  `t.Fatal`s if the two DSNs are equal, so tests can no longer wipe the live KB.
  Easiest: spin a local throwaway container — `task testdb:up` (or
  `bash deploy/local-testdb.sh up`) — and set
  `PIXKB_TEST_DSN=postgres://pixkb:pixkb@localhost:5433/pixkb?sslmode=disable`.
  Alternatively provision a remote DB as superuser via `deploy/sql/create-test-db.sql`.
  Run the full suite with `-p 1` (no parallel test binaries) since the tests
  share that one database.

## Operations
- ~~**`export-bundle` caps copy size.**~~ Resolved: there is no artificial
  per-file size limit — large artifacts are archived in full. The header is now
  sized from the open fd, and a file that shrinks mid-archive is zero-padded to
  its declared size (instead of producing a "write too short" / corrupt tar),
  while a growing file is still bounded to avoid "write too long" (`cmd/pixkb/ops.go`).

## Air-gap / embeddings
- **No learned-model embedder.** ONNX/MiniLM was removed: bundling model weights
  + a native runtime violates the air-gap rule (subscription agents only, no
  metered API, no native model runtime). Recall improvements must come from the
  agy agent fleet curating over pixdb, not a model (BACKLOG P2).
