# Search Capability Specification
<!-- rev:001 -->

Date: 2026-07-04

This document specifies the search capabilities pixkb should add or harden so
the knowledge base supports precise lookup, broad exploratory retrieval,
multi-search retrieval, similarity browsing, and grounded RAG. It is a product
and engineering specification only; it does not prescribe code-level
implementation.

## Purpose

pixkb already has a capable hybrid search foundation: full-text search,
pgvector cosine search, RRF fusion, intent-term enrichment, title boost, type
weighting, graph neighbors, and deterministic recall evaluation. The remaining
gap is not basic search. The gap is broader retrieval behavior: decomposing
ambiguous questions, searching from multiple angles, finding concepts similar to
an existing concept, explaining why a result matched, and exposing enough
controls for users and agents to steer retrieval safely.

The target state is a retrieval layer that supports four usage modes:

1. Precise lookup: a user knows the term, endpoint, message, identifier, or Pix
   concept they want.
2. Broad discovery: a user asks in natural language and expects related
   regulatory, API, message, and operational concepts to surface together.
3. Similarity browsing: a user starts from a known concept and asks what else is
   nearby by content, graph links, or domain relationship.
4. RAG grounding: an agent needs a compact, high-recall, citation-safe evidence
   set before answering.

## Existing Foundation

The current search design should be preserved as the baseline:

- `internal/store/postgres.FTS` provides weighted lexical recall over title,
  intent terms, and body.
- `internal/store/postgres.Vector` provides exact cosine KNN over stored
  embeddings.
- `internal/query.Hybrid` fuses FTS and vector hits with RRF, title overlap
  boost, type authority weight, and an out-of-domain vector floor.
- `internal/store/postgres.Related` exposes graph neighbors.
- `internal/rag.BuildGrounding` retrieves hybrid top-k hits and can append graph
  neighbors of the top hit.
- `eval/tophit.sh` and the precise/fuzzy id sets provide deterministic ranking
  regression checks.
- ADR 0002 records that broad OR/coverage FTS rewrites were measured and rejected
  because they traded precision for fuzzy recall.

Any new search feature must be additive around this foundation unless a new
measurement proves a better default.

## Non-Goals

- Do not replace the canonical OKF bundle with Postgres as source of truth.
- Do not introduce metered online embedding APIs into the runtime search path.
- Do not add broad OR-style FTS recall as the default without beating the
  precise and fuzzy eval gates.
- Do not pollute concept bodies with search-only terms. Search enrichment belongs
  in metadata such as `intent_terms`.
- Do not treat graph neighbors as relevance-ranked similarity by themselves;
  graph relation and semantic similarity are separate signals.

## Feature 1: Multi-Query Retrieval

### Problem

Current hybrid retrieval accepts one query string. Natural questions often
contain several intents, synonyms, entity names, or implied tasks. One query can
under-recall because all retrieval arms see the same wording.

Examples:

- "como estornar um pix recebido por engano e consultar depois pela API"
- "mensagem de devolução entre instituições e endpoint de refund"
- "cobrança com vencimento, juros, multa e consulta por txid"

### Required Behavior

Add a multi-query retrieval mode that expands one user request into a small set
of independent retrieval queries, runs the existing hybrid search for each, and
fuses all result lists into one ranked result set.

The query set should include:

- the original user query unchanged;
- a concise domain-term rewrite;
- one or more entity-specific subqueries when the question mentions distinct
  objects such as Pix refund, webhook, DICT key, API endpoint, pacs/camt message,
  certificate, QR code, or settlement;
- optional bilingual variants when the query likely maps to English ISO message
  concepts.

The retrieval layer must preserve provenance for each result:

- which subquery matched;
- whether the hit came from FTS, vector, or both;
- the original per-query rank;
- the final fused rank.

### Constraints

- Default expansion count should be small, preferably 3 to 5 queries.
- Expansion must be deterministic when no agent/LLM is configured.
- Agent-generated rewrites may be optional for RAG, but core search should have a
  non-agent fallback.
- A failed rewrite step must fall back to single-query hybrid search.
- Multi-query retrieval must call the existing hybrid search path rather than
  creating a second ranking implementation.

### Acceptance Criteria

- A multi-intent query can return relevant concepts for each major intent in one
  result set.
- Exact lookup quality must not regress on `eval/cases-precise-ids.tsv`.
- Fuzzy recall should be measured against `eval/cases-fuzzy-ids.tsv`; any
  improvement must not reduce precise top@5.
- RAG can request multi-query retrieval and receive enough attribution to cite
  the chosen grounding concepts.

## Feature 2: Concept Similarity Search

### Problem

The project has graph neighbors, but no direct "find concepts similar to this
concept" capability. Users and agents need to start from a known concept and
discover related endpoints, messages, manual sections, and reference concepts.

### Required Behavior

Add a concept-similarity surface that accepts a concept id and returns ranked
nearby concepts using multiple signals:

- vector similarity from the concept's stored embedding;
- lexical overlap between title, intent terms, and body;
- graph relationship strength;
- type-aware domain relationships, such as API refund endpoints near Pix refund
  concepts, pacs/camt messages near payment lifecycle concepts, and DICT
  endpoints near key concepts.

The result should distinguish why each item appeared:

- `semantic`: embedding/content similarity;
- `lexical`: shared terms or identifiers;
- `graph`: direct incoming or outgoing edge;
- `domain`: rule-based domain adjacency.

### Modes

Similarity should support at least these modes:

- `semantic`: nearest concepts by embedding.
- `graph`: direct graph neighbors only.
- `hybrid`: fused semantic, lexical, and graph signals.
- `more-like-this`: use the concept's title, intent terms, and summary/body as a
  generated query through existing hybrid search.

### Acceptance Criteria

- The queried concept is excluded from results by default.
- The user can include or exclude graph neighbors.
- Results are stable and deterministic for the same index state.
- API endpoints, ISO messages, and reference concepts can surface together when
  they describe the same Pix workflow.
- The feature is exposed through CLI, MCP, and the internal retrieval interface.

## Feature 3: Search Explanation and Debug Output

### Problem

Search tuning is currently measurable through evals, but individual results are
hard to inspect. Users and agents need to understand why a result ranked highly
or why an expected result did not appear.

### Required Behavior

Add an optional explanation mode for search results. Each hit should be able to
show:

- final fused score or comparable rank contribution;
- FTS rank and raw lexical score when present;
- vector rank and cosine similarity when present;
- title boost contribution;
- type authority contribution;
- matched query tokens when available;
- matched field categories: title, intent terms, body;
- subquery attribution for multi-query search.

### Constraints

- Explanation output must be optional and disabled by default.
- Normal search output should remain compact.
- Explanations must be available in machine-readable JSON for agents and tests.
- Explanations must not expose hidden prompt text or agent internals.

### Acceptance Criteria

- A developer can inspect why the top 5 results were ranked.
- A failed eval case can be debugged without adding temporary logging.
- MCP search can return structured explanation data when requested.

## Feature 4: Rich Search Filters and Output Formats

### Problem

The lower-level filter model supports more than the current CLI and MCP expose.
Search is also difficult to script because output is plain text only.

### Required Behavior

Expose richer filters consistently across CLI, HTTP, MCP, and internal retrieval:

- concept type;
- tag;
- limit;
- as-of epoch;
- as-of timestamp;
- include or exclude concept ids;
- include or exclude concept types;
- minimum vector score when applicable;
- mode: `fts`, `vector`, `hybrid`, `multi`, `similar`.

Expose output formats:

- text for humans;
- JSON for scripts and agents;
- markdown for reports;
- YAML where consistent with existing configuration workflows.

### Acceptance Criteria

- `pixkb search` can emit JSON with rank, id, title, type, score, and optional
  explanation.
- MCP search can accept the same core filter set as CLI search.
- As-of filtering is test-covered at the public surface, not only the store layer.

## Feature 5: RAG Retrieval Upgrade

### Problem

RAG grounding currently retrieves top-k hybrid hits and optionally graph neighbors
of the single top hit. This is safe but can miss supporting concepts for broad or
multi-part questions.

### Required Behavior

Upgrade RAG retrieval to support:

- multi-query retrieval;
- concept diversity across types;
- optional graph expansion from more than one seed hit;
- budget-aware chunk selection;
- citation-aware result attribution;
- refusal when all retrieval evidence is weak or out-of-domain.

### Retrieval Policy

RAG should prefer a diverse evidence set over many near-duplicates. For broad Pix
questions, a good grounding set often includes:

- one canonical reference concept;
- one API endpoint concept when API behavior is involved;
- one ISO message concept when interbank messaging is involved;
- one manual section only when it adds normative detail not present elsewhere.

### Acceptance Criteria

- RAG answers for broad questions cite multiple complementary concept types when
  appropriate.
- RAG still refuses out-of-domain questions without spending an answerer turn.
- `eval/run-rag-judge.sh` must pass or improve before changing default RAG
  retrieval behavior.

## Feature 6: Search Evaluation Expansion

### Problem

Precise and fuzzy top-hit evals exist, but new multi-query and similarity
features need dedicated gates.

### Required Behavior

Add evaluation sets for:

- multi-intent queries;
- concept similarity expectations;
- RAG grounding diversity;
- as-of filtered search;
- search explanation consistency;
- out-of-domain rejection.

### Metrics

Use deterministic metrics where possible:

- top@1;
- top@5;
- MRR;
- required-id coverage for multi-query cases;
- forbidden-id absence for out-of-domain or noisy cases;
- type diversity for RAG grounding.

Judged evals may still be used for RAG answer quality, but ranking changes must
have deterministic gates first.

### Acceptance Criteria

- Multi-query changes are rejected if they improve broad recall but regress
  precise top@5.
- Similarity search has at least one expected-neighbor test per major concept
  family: API endpoint, ISO message, reference concept, manual section.
- Eval outputs are easy to compare before and after a ranking change.

## Feature 7: Domain-Aware Query Understanding

### Problem

The current search layer relies on stored intent terms and hashing overlap. It
does not explicitly understand common Pix/SPB domain aliases and object
families.

### Required Behavior

Add a small domain vocabulary layer for query normalization and expansion. This
should be curated, deterministic, and auditable.

Candidate mappings:

- `estorno`, `devolução`, `refund` -> Pix refund concepts and refund endpoints;
- `chave aleatória`, `EVP` -> DICT key type concepts;
- `txid`, `identificador da transação` -> cobrança lookup endpoints;
- `e2eid`, `endToEndId`, `identificador fim a fim` -> payment/refund lifecycle;
- `webhook`, `notificação`, `aviso automático` -> Pix webhook concepts;
- `liquidação`, `reservas`, `SPI` -> settlement concepts;
- `certificado`, `mTLS`, `ICP-Brasil` -> security/connectivity concepts;
- `pacs.008`, `ordem de crédito` -> customer credit transfer message;
- `pacs.004`, `devolução entre instituições` -> payment return message;
- `camt.054`, `extrato`, `lançamento` -> account notification message.

### Constraints

- The vocabulary must be versioned with the repo.
- The vocabulary must not override user filters.
- The vocabulary must be used as retrieval assistance, not as generated
  normative content.
- Changes must be measured against precise and fuzzy evals.

### Acceptance Criteria

- Natural-language aliases improve recall without modifying concept bodies.
- Each mapping has at least one eval case or documented reason to exist.
- Users can inspect or disable domain expansion when debugging.

## Feature 8: Search Quality Operations

### Problem

Search quality depends on content quality, intent terms, embeddings, graph links,
 and ranking parameters. The project needs operational workflows to keep these
healthy.

### Required Behavior

Add search-quality operations for:

- detecting concepts with missing or stale `intent_terms`;
- detecting concepts whose title is too noisy for title boosting;
- detecting concepts with empty or unusually sparse graph links;
- reporting embedding coverage and model/dimension consistency;
- listing search eval regressions by case;
- recommending concepts for re-enrichment.

### Acceptance Criteria

- A maintainer can run one command to see search-readiness health.
- Re-enrichment candidates are prioritized by failed evals, sparse terms, or
  weak retrieval history.
- Search quality reports avoid treating all missing enrichment as errors;
  enrichment remains an opportunity unless it breaks an eval or user workflow.

## API Surface Requirements

All major retrieval capabilities should be exposed consistently:

### CLI

- `search`: precise, vector, FTS, hybrid, multi-query modes.
- `similar`: concept-to-concept similarity.
- `related`: graph neighbors, preserved as graph-specific behavior.
- `ask`: RAG with selectable retrieval mode.
- `search eval` or equivalent command surface for deterministic retrieval gates.

### MCP

- `search`: hybrid and multi-query retrieval with structured results.
- `similar`: concept similarity.
- `related`: graph neighbors.
- `concept_get`: unchanged canonical concept read.
- `kb_ask`: RAG with retrieval options and citation-safe grounding.

### HTTP

- `/search`: query retrieval with filters and JSON output.
- `/similar`: concept-id similarity.
- `/related`: graph neighbors.
- Optional `/explain/search` or `explain=true` parameter for debug output.

## Ranking Principles

New ranking work should follow these rules:

- Preserve exact lookup quality before optimizing fuzzy recall.
- Prefer measured improvements over intuition.
- Keep lexical, vector, graph, and domain signals separately inspectable.
- Use RRF or another rank-fusion method when combining independent retrieval
  lists.
- Penalize neither manual sections nor API endpoints globally; use type authority
  only as a modest tiebreaker.
- Avoid a broad OR default unless deterministic evals prove it helps both precise
  and fuzzy cases.
- Treat out-of-domain silence as better than confident noise.

## Recommended Implementation Order

1. Search explanations and JSON output.
   This makes every later ranking change easier to debug and evaluate.

2. Concept similarity.
   This is a bounded feature that reuses existing embeddings and graph data while
   adding a clearly missing user workflow.

3. Multi-query retrieval behind an explicit mode.
   Keep the current hybrid search default while measuring the new behavior.

4. RAG retrieval upgrade.
   Once multi-query retrieval is measured, allow RAG to use it with grounding
   diversity and citation attribution.

5. Domain vocabulary layer.
   Add deterministic domain expansion only after explanation and eval tooling can
   show whether it helps.

6. Search quality operations.
   Use eval failures and explanation data to drive enrichment and curation.

7. Default-mode promotion.
   Promote multi-query or domain expansion to a default only after precise,
   fuzzy, OOD, and RAG evals all pass.

## Verification Gates

Before shipping any retrieval change:

- Run unit tests for affected packages.
- Run deterministic precise top-hit evals.
- Run deterministic fuzzy top-hit evals.
- Run any new multi-query or similarity evals.
- Run RAG judge when RAG grounding behavior changes.
- Inspect at least three representative explanation outputs:
  precise lookup, fuzzy natural-language query, and out-of-domain query.

No ranking change should be accepted solely because it improves one anecdotal
query.

## Open Questions

- Should multi-query rewriting be fully deterministic, agent-assisted, or both?
- Should similarity use only stored embeddings, or should it generate a
  "more-like-this" search query from concept fields?
- What result diversity policy should RAG use when top hits are all the same
  concept type?
- Should domain vocabulary live in YAML configuration, OKF metadata, or a Go data
  package?
- What similarity expectations should be considered normative for each concept
  family?

## Definition of Done

The search system can be considered complete for broad retrieval when:

- precise lookup remains stable;
- fuzzy search improves or is at least explainably bounded;
- multi-intent questions retrieve evidence for each major intent;
- users can browse from one concept to similar concepts;
- graph neighbors and semantic similarity are distinct but composable;
- RAG can ground broad questions with diverse, citation-safe concepts;
- ranking decisions are explainable in JSON;
- all search surfaces expose consistent filters and output formats;
- search quality has deterministic eval and maintenance workflows.
