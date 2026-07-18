// Package rag is the retrieve-augment half of the KB's RAG layer: it turns a
// question into a citation-tagged grounding context assembled from the canonical
// concept bodies, ready to hand to the answerer agent (the generate half lives in
// answer.go). Retrieval reuses the hybrid search; augmentation reads concept
// bodies from the bundle (the source of truth, same as concept_get) and packs
// them into a token budget, each chunk tagged with its concept id + source_uri so
// every generated claim can be cited. Generation is air-gap-compliant: it runs on
// the subscription agent fleet, never a metered API or a native model runtime.
package rag

import (
	"context"
	"fmt"
	"strings"

	"pixkb/internal/okf"
)

// Hit is a retrieved concept reference — the subset of a search hit the augment
// step needs (id + title + type + fused score), decoupled from the postgres type.
type Hit struct {
	ID    string
	Title string
	Type  string
	Score float64
}

// Retriever returns ranked concept hits for a query and the neighbour ids of a
// concept. Production wires this to query.Hybrid + store.Related; tests inject a
// fake. Keeping it an interface keeps the rag core DB-free and unit-testable.
type Retriever interface {
	Retrieve(ctx context.Context, q string, k int) ([]Hit, error)
	Related(ctx context.Context, id string) ([]string, error)
}

// MultiRetriever is an optional capability a Retriever may implement: run
// query.MultiHybrid's multi-query expansion instead of single-query Hybrid.
// BuildGrounding type-asserts for it when Options.MultiQuery is set and falls
// back to Retrieve when the assertion fails — every existing Retriever
// (including every test fake) keeps compiling with no change, and a
// Retriever that doesn't support multi-query degrades to single-query
// search rather than erroring.
type MultiRetriever interface {
	RetrieveMulti(ctx context.Context, q string, k int) ([]Hit, error)
}

// SimilarRetriever is an optional capability a Retriever may implement:
// pull concept-similarity neighbours (internal/similar.Similar) of a seed
// hit as an additional evidence-diversification source, alongside graph
// neighbours (ExpandRelated). BuildGrounding type-asserts for it when
// Options.ExpandSimilar is set and silently skips this source when the
// Retriever doesn't implement it — every existing Retriever (including
// every test fake) keeps compiling with no change.
type SimilarRetriever interface {
	RetrieveSimilar(ctx context.Context, id string) ([]Hit, error)
}

// ConceptSource resolves a concept's full content (body + provenance) by id from
// the canonical bundle — the same source concept_get reads, so the grounding
// shows exactly the curated text, never a stale DB copy.
type ConceptSource interface {
	Concept(ctx context.Context, id string) (okf.Concept, error)
}

// Chunk is one grounding fragment, tagged for citation.
type Chunk struct {
	ID        string
	Title     string
	Type      string
	SourceURI string
	Body      string
}

// Grounding is the assembled, citation-tagged context for a question. Empty
// Chunks means retrieval found nothing usable — the caller MUST refuse rather
// than let the agent answer ungrounded.
type Grounding struct {
	Query  string
	Chunks []Chunk
}

// Options tune retrieval + assembly. The zero value is usable (defaults applied).
type Options struct {
	TopK          int         // hybrid hits to take (default defaultTopK)
	ExpandRelated bool        // also pull the graph neighbours of the seed hit(s)
	MaxChars      int         // grounding char budget, ~4 chars/token (default defaultMaxChars)
	MultiQuery    bool        // use the Retriever's MultiRetriever (multi-query expansion) when it implements one; silently falls back to single-query Retrieve otherwise
	Diversify     bool        // prefer the first hit of each concept Type before filling remaining slots by rank
	ExpandSeeds   int         // how many top hits' graph neighbours to pull when ExpandRelated (default 1, preserves the pre-upgrade single-seed behavior)
	ExpandSimilar bool        // also pull the top hit's concept-similarity neighbours (internal/similar.Similar) when the Retriever implements SimilarRetriever; silently a no-op otherwise
	MinScore      float64     // refuse (empty Grounding, no agent turn spent) when the top hit's score is below this (0 = disabled)
	NoPIIFilter   bool        // skip the deterministic PII/LGPD redaction pass over Answer.Text (default false = filter ON; debugging escape hatch only)
	Cache         AnswerCache // when set, Ask checks/populates it keyed by CacheKey(q, Epoch) to skip re-spending an agent turn on a repeated question (nil = no caching, the pre-change default)
	Epoch         int         // the KB epoch this question is being answered against (see postgres.Store.Stats().LatestEpoch); only consulted when Cache is set
}

const (
	defaultTopK     = 6
	defaultMaxChars = 8000
)

func (o Options) topK() int {
	if o.TopK > 0 {
		return o.TopK
	}
	return defaultTopK
}

func (o Options) maxChars() int {
	if o.MaxChars > 0 {
		return o.MaxChars
	}
	return defaultMaxChars
}

func (o Options) expandSeeds() int {
	if o.ExpandSeeds > 0 {
		return o.ExpandSeeds
	}
	return 1
}

// retrieve dispatches to the Retriever's multi-query path when the caller
// asked for one (Options.MultiQuery) AND the Retriever supports it — a type
// assertion, not an interface requirement, so every existing single-query
// Retriever (and every existing test fake) needs no change. Without a
// MultiRetriever, MultiQuery is silently a no-op fallback to single-query
// Hybrid, matching the spec's "a failed rewrite step must fall back to
// single-query hybrid search" constraint by construction.
func retrieve(ctx context.Context, r Retriever, q string, opts Options) ([]Hit, error) {
	if opts.MultiQuery {
		if mr, ok := r.(MultiRetriever); ok {
			return mr.RetrieveMulti(ctx, q, opts.topK())
		}
	}
	return r.Retrieve(ctx, q, opts.topK())
}

// diversify reorders hits so the first hit of each distinct Type is promoted
// ahead of any later hit of a Type already seen — a stable, rank-preserving
// reshuffle, not a re-score (ties within a group keep their relative order).
// Hits with Type == "" are never deduped against each other (each is treated
// as its own group), so untyped concepts are never silently dropped.
// Deterministic: the same input order always yields the same output order.
func diversify(hits []Hit) []Hit {
	seenType := map[string]bool{}
	var first, rest []Hit
	for _, h := range hits {
		if h.Type != "" && seenType[h.Type] {
			rest = append(rest, h)
			continue
		}
		if h.Type != "" {
			seenType[h.Type] = true
		}
		first = append(first, h)
	}
	return append(first, rest...)
}

// BuildGrounding retrieves for q, optionally expands the top hit's graph
// neighbours, then reads each concept from the bundle and packs them into the
// char budget — always including at least the top hit, dropping the rest once the
// budget is hit. Order follows retrieval rank (neighbours appended after the
// direct hits). A concept that fails to load or has an empty body is skipped, not
// fatal. An empty result (OOD / no hits) returns a Grounding with no Chunks.
func BuildGrounding(ctx context.Context, r Retriever, cs ConceptSource, q string, opts Options) (Grounding, error) {
	g := Grounding{Query: q}
	hits, err := retrieve(ctx, r, q, opts)
	if err != nil {
		return g, fmt.Errorf("retrieve: %w", err)
	}
	if len(hits) == 0 {
		return g, nil // OOD / empty — caller refuses
	}
	if opts.MinScore > 0 && hits[0].Score < opts.MinScore {
		return g, nil // weak evidence — refuse without spending an agent turn
	}
	if opts.Diversify {
		hits = diversify(hits)
	}

	ids := make([]string, 0, len(hits)+4)
	seen := map[string]bool{}
	add := func(id string) {
		if id == "" || seen[id] {
			return
		}
		seen[id] = true
		ids = append(ids, id)
	}
	for _, h := range hits {
		add(h.ID)
	}
	if opts.ExpandRelated {
		// Neighbours of the top N seed hits (N = ExpandSeeds, default 1 — the
		// pre-upgrade behavior of expanding only the single best hit).
		seeds := opts.expandSeeds()
		if seeds > len(hits) {
			seeds = len(hits)
		}
		for _, h := range hits[:seeds] {
			nb, err := r.Related(ctx, h.ID)
			if err != nil {
				continue
			}
			for _, id := range nb {
				add(id)
			}
		}
	}
	if opts.ExpandSimilar {
		// Top hit only — concept-similarity is 1:1 with a seed concept, unlike
		// graph expansion (ExpandRelated) which can fan out from multiple seeds.
		if sr, ok := r.(SimilarRetriever); ok {
			if nb, err := sr.RetrieveSimilar(ctx, hits[0].ID); err == nil {
				for _, h := range nb {
					add(h.ID)
				}
			}
		}
	}

	budget := opts.maxChars()
	used := 0
	for _, id := range ids {
		c, err := cs.Concept(ctx, id)
		if err != nil {
			continue // a missing concept is not fatal to the answer
		}
		body := strings.TrimSpace(c.Body)
		if body == "" {
			continue
		}
		// Always include the first usable chunk; stop once the budget is exceeded.
		if len(g.Chunks) > 0 && used+len(body) > budget {
			break
		}
		g.Chunks = append(g.Chunks, Chunk{ID: id, Title: c.Title, Type: c.Type, SourceURI: c.SourceURI, Body: body})
		used += len(body)
	}
	return g, nil
}

// Untrusted-document envelope markers. Concept bodies are externally-authored
// (PDF/docx/legislation) text; fencing them and telling the agent never to obey
// instructions inside the envelope is the defense against prompt injection
// smuggled in ingested documents. DocEnd is stripped from bodies at render time
// so a document cannot forge the boundary and escape the envelope.
const (
	DocBegin = "<<<UNTRUSTED-DOCUMENT>>>"
	DocEnd   = "<<<END-UNTRUSTED-DOCUMENT>>>"
)

// Render formats the grounding as the context block handed to the answerer: each
// chunk carries its concept id + source_uri (trusted metadata) so the agent can
// cite by id and a reader can trace provenance, and its Body is wrapped in the
// untrusted-document envelope with any forged closing marker neutralized. Returns
// "" when there are no chunks.
func (g Grounding) Render() string {
	if len(g.Chunks) == 0 {
		return ""
	}
	var b strings.Builder
	for i, c := range g.Chunks {
		if i > 0 {
			b.WriteString("\n\n")
		}
		body := strings.ReplaceAll(c.Body, DocEnd, "")
		fmt.Fprintf(&b, "[concept: %s | source: %s]\n%s\n%s\n%s\n%s",
			c.ID, sourceOrNone(c.SourceURI), c.Title, DocBegin, body, DocEnd)
	}
	return b.String()
}

func sourceOrNone(s string) string {
	if strings.TrimSpace(s) == "" {
		return "(no source)"
	}
	return s
}
