# Knowledge Base Authoring — Good Practices
<!-- rev:001 -->

A field guide to building a high-quality, durable, searchable knowledge base,
distilled from building **pixkb** (an air-gap OKF knowledge base for Brazil's
Pix/SPB). Every practice here is earned: it traces to a concrete decision, bug,
or measurement during the build, not generic advice. Use it to build the next
KB without re-learning the same lessons.

---

## 0. The one-sentence thesis

**A KB is a *canonical, human-readable corpus* plus a *rebuildable derived index*,
continuously *measured* against real questions.** Get those three right —
canonical source, derived index, evaluation — and everything else is detail.

---

## 1. Separate the canonical source from the derived index

- **Canonical** = the durable truth: concept-per-file markdown + frontmatter, in
  git. Human-readable, diffable, portable, survives any tool.
- **Derived** = the query engine: Postgres + pgvector here. Fast, disposable,
  **rebuildable from canonical at any time** (`pixkb reindex`).

**Why it matters.** When integration tests truncated the live index, nothing was
lost — `reindex` rebuilt all 275 concepts from the bundle. If the query store had
been authoritative, that would have been data loss.

**Rules**
- The index must be reconstructible *exactly* from canonical. If reindex can't
  reproduce a field (e.g. graph edges), that field is a bug waiting to happen.
- Never write data that lives *only* in the index. History lives in git.
- Treat the index as a cache you can blow away.

**Pitfall.** Mixing authoritative state into the derived store. The moment the
index holds the only copy of something, you've lost the property that makes the
whole design safe.

---

## 2. Concept granularity and stable identity

- One concept = one file. **Path is identity.** IDs must be stable across
  re-ingests so diffs, links, and edges stay meaningful.
- Granularity is a quality dial:
  - Too fine → fragments. Our PDF extractor first split on a permissive heading
    regex and produced junk "concepts" titled `63 04`, `EMV QRCPS`,
    `1 de autorização do Pix`. These **outranked real content** in search.
  - Too coarse → one giant blob, no recall precision.

**Lesson: segmentation quality *is* KB quality.** We spent real effort tightening
heading detection (numbered headings must be Title-Case; all-caps headings need a
real word with a vowel; reject leading-numeric table rows). Bad segmentation
silently poisons every downstream search.

**Titles are search surface, not decoration.** A concept titled `CONCLUÍDA é`
helps no one and ranks for the wrong queries. If you can't derive a meaningful
title, the segment probably shouldn't be its own concept.

---

## 3. Ingestion: pluggable adapters, provenance, idempotency

- Model each source as an adapter that emits concepts: ISO-20022 spec, PDF, git
  mirror, OpenAPI, API-DICT HTML. Adding a source must not touch the others.
- **Provenance on every concept** (`SourceURI`, `Resource`). Trust requires
  knowing where a fact came from; re-ingest requires knowing what to refresh.
- **Idempotency is non-negotiable.** Same input → same IDs → same content → same
  content hash. Only then are "added / changed / removed" diffs trustworthy and
  re-ingest safe. Sort everything; never depend on map iteration order.

**Pitfall.** Non-deterministic output (unsorted iteration, timestamps baked into
content) makes every re-ingest look like everything changed, destroying the
signal in epoch diffs.

---

## 4. Enrichment beats raw extraction

A dump of raw text is a low-value KB. The value is in the layer you add.

- Our API endpoints started as bare titles (`POST /cob`). We enriched them with
  parameters, request/response schemas, and status codes — so a search for a
  schema name (`CobGerada`) or status (`400`) finds the endpoint.
- **Bridge the vocabulary gap.** Users query in their words, not the source's.
  An OpenAPI path says `POST /cob`; the user types "criar cobrança imediata". We
  synthesize intent terms (`verb × resource`: criar/gerar/consultar ×
  cobrança/chave dict/qr code) into the concept body so natural-language queries
  surface the right structured concept. This single change moved multiple judge
  cases from *fail* to *pass*.

**Rule.** For every concept type, ask: "what words would someone use to look for
this, and are those words in the concept?" If not, enrich.

---

## 5. The graph is a first-class feature

Isolated concepts are a worse KB than connected ones.

- Build edges deterministically: shared salient domain terms + structural
  families (every `/cob*` endpoint relates to the others).
- **Bound it.** Cap links per concept (≤6) and ignore over-generic terms (a term
  in >N concepts links nothing) — otherwise you get a hairball where everything
  links to everything and nothing is useful.
- Store links *in the canonical source* (a `## Related` section of real markdown
  links), so edges are reproduced identically at ingest and reindex time.

**Pitfall.** Computing the graph only in the index. Then reindex loses it. Put it
in canonical.

---

## 6. History: bitemporal, epoch-based, derived from git

- Each ingest cuts an **epoch** and a git commit. Concepts carry valid-time and
  transaction-time ranges (`concept_fact`) so you can ask "what did the KB say at
  epoch N / time T".
- **Keep the *index* current-only; keep *history* in git.** This is the subtle
  one. Embeddings accumulated one full set per epoch — 1659 rows for 275 concepts
  after 8 epochs. The vector search's `DISTINCT ON` scan over that backlog grew
  until **hybrid search timed out**, silently dropping callers to a poor
  FTS-only fallback. Pruning the derived index to latest-per-id fixed both speed
  and correctness.

**Lesson.** Derived-index bloat is a silent quality killer. Bound every derived
table to the current set; reconstruct history from canonical when asked.

---

## 7. Retrieval: hybrid, with tunable authority

- **No single retrieval mode is enough.** Lexical (FTS) catches exact terms and
  codes; vector catches paraphrase and semantics. Fuse them (Reciprocal Rank
  Fusion). We ship both and a hybrid default.
- **Weight by concept authority.** Extracted manual fragments kept outranking the
  exact API endpoint or ISO message. A modest type-authority boost (canonical
  structured concepts ×1.15 over manual fragments) fixed the dominant failure
  mode — *without* a blanket penalty that would hurt genuinely manual-intent
  queries. Keep such weights small and justify each one with a measurement.

**Pitfall.** Shipping one retrieval mode, or hard-coding ranking without measuring
the effect on a held-out case set.

---

## 8. Embeddings: portable by default, upgradeable

- Use a dependency-free, deterministic embedder (pure-Go hashing) so the KB
  builds and runs anywhere, including air-gapped. Higher recall comes from the
  agy agent fleet curating concepts over pixdb — NOT a learned-model embedder
  (a native runtime / metered API violates the air-gap rule; ONNX/MiniLM was
  removed for exactly that reason).
- **Guard the dimension.** Don't let embedders of different dimensions write to
  the same vector column; a runtime guard beats silent corruption.

**Lesson.** Make the default work everywhere; make quality an opt-in upgrade, not
a hard dependency that blocks the air-gap target.

---

## 9. Evaluation is the steering wheel (not optional)

This is the practice that separates a KB that *feels* done from one that *is* good.

- **You cannot improve what you don't measure.** Stand up an LLM-as-judge loop:
  author cases → the judge runs the real searches → scores relevance/precision →
  critiques → you apply the fix → re-judge → measure the lift.
- **Separate author from judge.** Here Claude authors cases; an independent agent
  (Codex) performs the searches and critiques. Independence keeps the eval honest
  — the judge isn't grading its own homework.
- **Structured verdicts.** Force the judge to emit a schema (relevance 0–5,
  precision 0–5, verdict, critique, concrete enhancements). Aggregate to a single
  trend line.
- **Broad cases, not happy paths.** Cover the whole KB surface (every concept
  type, every domain area) and include an **out-of-domain control** where the
  correct outcome is *low* relevance — so you detect a KB that hallucinates
  relevance for anything.
- **The judge finds what unit tests can't.** Ours surfaced a search timeout from
  index bloat and a ranking bug (fragments beating endpoints) — both invisible to
  green unit tests. It turned vague "make it better" into a ranked, concrete
  backlog.

**Measured result.** Three enhancement iterations driven purely by judge critique
moved the suite from **rel 3.70 / prec 2.40** to **4.20 / 3.20**, eliminating all
hard failures. The loop, not any single fix, is the asset.

---

## 10. Operations, reproducibility, safety

- **Determinism end to end** (stable IDs, sorted iteration, content hashing) so
  reindex and re-ingest reproduce byte-for-byte.
- **Never run destructive tests against the production KB.** Tests truncate and
  drop tables; pointing `PIXKB_TEST_DSN` at the live database wiped it once. The
  fix is a guard that refuses to run when the test DSN equals the prod DSN, plus a
  throwaway test database. Treat the KB's data store like production data.
- **Delivery = canonical bundle + a static binary + a rebuildable index.** For
  air-gap, bake the bundle into an image and `reindex` on first run, or ship the
  binary against an existing database. The index is never the thing you ship as
  truth — the bundle is.

---

## Anti-patterns (each one cost us time)

| Anti-pattern | Symptom | Fix |
|---|---|---|
| Authoritative derived store | Data loss when index is cleared | Canonical bundle is truth; index rebuildable |
| Sloppy segmentation | Junk-titled fragments outrank real content | Invest in extraction/heading quality |
| Raw dump, no enrichment | Bare titles, queries miss | Enrich; synthesize intent terms |
| Vocabulary mismatch | NL query never hits structured concept | Add the user's words to the concept |
| Graph computed only in index | Reindex loses links | Store links in canonical markdown |
| Unpruned derived history | Search slows, then times out; stale hits | Bound derived tables to current set |
| Single retrieval mode | Blind spots (lexical *or* semantic) | Hybrid + RRF |
| Unmeasured ranking tweaks | Regressions you can't see | Gate every change behind the judge |
| No evaluation | "Looks done" ≠ "is good" | LLM-as-judge loop, broad cases |
| Shared test/prod data store | Catastrophic wipes | DSN guard + throwaway test DB |

---

## A starting checklist for a new KB

1. Define the **concept** (unit, identity, frontmatter) and the **canonical**
   format. Decide granularity deliberately.
2. Write **source adapters** with provenance; make output deterministic.
3. **Enrich**: add the words users will search; cross-link deterministically.
4. Stand up a **rebuildable derived index** (lexical + vector) with reindex.
5. Add **history** (epochs/commits) but keep the index current-only (prune).
6. Build the **evaluation loop first**, not last: broad cases + independent judge.
7. Iterate: critique → fix → re-judge → watch the trend line.
8. Harden ops: determinism, test isolation, air-gap delivery.

The KB is never "finished" — it's *measured*. Ship the loop, and the quality
follows.
