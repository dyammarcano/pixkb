# ADR 0001 — Storage for agent-generated intent terms (recall enrichment)

- **Status:** Accepted
- **Date:** 2026-06-24
- **Context item:** BACKLOG P2 "Agent-driven recall (replaces ONNX)"

## Context

The default embedder is a pure-Go hashing vectorizer with weak semantic recall
(ISSUES "Hashing embedder…"). The air-gap rule forbids a learned-model embedder
(no metered API, no native model runtime), so the planned recall lever is the
agent fleet enriching concepts with **intent terms** — synonyms, alternate
phrasings, and the words a Pix/SPB engineer would actually search for ("chave
aleatória" ↔ "EVP", "reservas bancárias" ↔ "liquidação no SPI"). The open
question (BACKLOG) is **where those terms live** so search actually uses them.

Decisive constraint discovered while deciding: the FTS arm builds its tsvector
inline from **title (weight A) + body (weight D) only** —
`internal/store/postgres/search.go`. **Tags are NOT in the tsvector.** So intent
terms placed in `tags` would not change FTS ranking at all; they would only be
filter facets.

## Options

1. **Append to `tags`.** Zero code. But tags are unindexed by FTS → no recall
   gain, and it overloads a field whose job is faceted filtering (`--tag`).
2. **Append to the concept `body`.** Already indexed (weight D), zero schema
   change. But it pollutes the canonical BACEN content with non-normative search
   bait, muddies `concept_get` output, and breaks the charter's "body is the
   normative view" contract. The hygiene engine would also see the injected
   terms as content.
3. **Dedicated `intent_terms` field**, woven into the FTS tsvector at a middle
   weight (B or C — above body, below title), kept OUT of the rendered body.
   Requires a column + a tsvector change + a write path, but keeps the canonical
   body clean and gives the terms real, tunable ranking influence.

## Decision

**Option 3 — a dedicated `intent_terms` field, FTS-weighted B/C, excluded from
the rendered body.**

- Schema: add a nullable `intent_terms text` column to `concept`; include it in
  the FTS expression as `setweight(to_tsvector(<lang>, coalesce(intent_terms,'')), 'B')`
  alongside the existing title(A)/body(D) terms.
- OKF: add `IntentTerms string` to `okf.Concept`; persist/round-trip it in
  frontmatter (e.g. `intent_terms:`), never in the markdown body.
- Provenance: terms are agent-generated; record that they are enrichment, not
  source content (so they are excluded from the BACEN-normative body and from
  deviation scanning of content).
- Hygiene: `intent_terms` is metadata, not body — the stub/deviation checks keep
  reading the body only.
- Rollout: the agy/research fleet fills `intent_terms` in bounded `curate`/enrich
  batches (per BACKLOG, not one 195-concept run); measure lift on the judge suite.

## Consequences

- **Positive:** real, air-gap-compliant recall lever with no model; canonical
  body stays clean; ranking weight is tunable independently of title/body;
  measurable against the eval judge.
- **Negative:** a schema migration + FTS expression change + write-path plumbing
  before any enrichment lands; weight B/C needs tuning against the judge to avoid
  intent-term spam outranking real title matches.
- **Neutral:** the hashing vector arm is unaffected (stays a cheap fallback);
  this strengthens the lexical arm, which is where the title-overlap boost
  already does precision work.

## Follow-ups

- Schema migration + `okf.Concept.IntentTerms` + FTS weave (the mechanism).
- A 10-concept pilot to validate the lever and tune the B/C weight before the
  full rollout (the recall-pilot backlog item).
