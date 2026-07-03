# ADR 0002 — Recall tuning: where the fuzzy-query ceiling actually is

- **Status:** Accepted
- **Date:** 2026-06-25
- **Context item:** BACKLOG P2 "Agent-driven recall — intent_terms rollout" / ISSUES "Search quality"
- **Supersedes the open question in:** [[0001-intent-terms-storage]]

## Context

After the `intent_terms` mechanism (ADR 0001) shipped and all 207 concepts were
enriched in prod, the fuzzy-query recall did NOT improve. This ADR records the
measured investigation so the dead ends are not re-walked.

The decision metric is a **deterministic top-hit harness** (`eval/tophit.sh` +
`eval/cases-{precise,fuzzy}-ids.tsv`), not the codex judge: at ~20 cases the judge
is too noisy to detect a ranking change (one case swinging ±2 on a 0–5 scale dwarfs
the effect), whereas top@1/top@5/MRR over fixed (query, expected-id) pairs is
variance-free. The precise set is the regression GUARD (must hold top@5 100%); the
fuzzy set is the target.

## Findings (measured chain)

Baseline = committed AND-recall FTS (`websearch_to_tsquery`) + RRF hybrid, 207/207
enriched. Broadened sets: 26 precise, 17 fuzzy.

| change | precise MRR (top@5) | fuzzy MRR (top@5) | verdict |
|---|---|---|---|
| AND-recall (baseline) | 0.788 (100%) | 0.285 (41%) | kept |
| naive OR-recall (`&`→`|`) | flat | 0.162 (24%) | reverted — floods arm, short junk floats |
| coverage-ranked OR | 0.719* (100%) | 0.274 (41%) | reverted — hybrid flat |
| coverage + FTS-arm ×2 | 0.745* (100%) | 0.284 (41%) | reverted — moved only precise |
| pinned-config coverage | **0.753 (regressed from 0.788)** | 0.321 (41%) | reverted — trades precise for fuzzy |
| **intent_terms in the EMBEDDING** | **0.821 (100%)** | **0.303 (53%)** | **KEPT** |

\* the coverage variants looked like precise *wins* on the original 16-case set
(0.698→0.719); broadening the guard to 26 cases exposed the regression. Lesson:
**a small guard set overfits — broaden it before trusting a "win".**

## Decisions

1. **FTS-recall ranking is exhausted as a fuzzy lever.** Every variant trades one
   axis for the other. `websearch_to_tsquery` ANDs all query words (one
   out-of-vocab/inflected word — `pixpt` has no stemmer — zeros the match); OR
   floods and short-junk floats under length-normalized `ts_rank_cd`; coverage
   ranking fixes the FTS arm in isolation but regresses precise once the guard set
   is honest. **Do not re-attempt OR/coverage rewrites of the recall WHERE.**

2. **The win was the VECTOR arm, not FTS.** The hashing embedder fed `Title + Body`
   only, ignoring `intent_terms`. Folding them into the embedded text
   (`Title + intent_terms + Body`, `epoch/runner.go`) lets the bag-of-words cosine
   match a paraphrase query against the recall synonyms. It lifts BOTH axes
   (precise 0.788→0.821, fuzzy top@5 41%→53%) because it touches only the
   down-weighted/floored vector arm, not the precise-ranking SQL. **KEPT, prod
   reindexed.**

3. **The residual fuzzy ceiling is the hashing embedder's weak semantics** (plus a
   few concept term gaps). A learned-model embedder would break it but is
   **air-gap-forbidden** (no native runtime, no metered API). Remaining gains are
   marginal: re-tune terms (`curate --reenrich`) for the few thin misses.

## Consequences

- `eval/tophit.sh` + the two id-sets are the standing recall harness; any future
  ranking/embedding change is gated on them (precise top@5 must stay 100%).
- A core-ranking change must be validated on the FULL judge, not 16–33 curated
  cases — broaden the guard or run `eval/run-judge.sh` before shipping.
- The recall story is now: lexical precise via FTS, paraphrase via the
  intent_terms-augmented vector arm; the ceiling is the embedder, air-gap-bounded.
