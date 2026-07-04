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
	TopK          int  // hybrid hits to take (default defaultTopK)
	ExpandRelated bool // also pull the graph neighbours of the top hit
	MaxChars      int  // grounding char budget, ~4 chars/token (default defaultMaxChars)
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

// BuildGrounding retrieves for q, optionally expands the top hit's graph
// neighbours, then reads each concept from the bundle and packs them into the
// char budget — always including at least the top hit, dropping the rest once the
// budget is hit. Order follows retrieval rank (neighbours appended after the
// direct hits). A concept that fails to load or has an empty body is skipped, not
// fatal. An empty result (OOD / no hits) returns a Grounding with no Chunks.
func BuildGrounding(ctx context.Context, r Retriever, cs ConceptSource, q string, opts Options) (Grounding, error) {
	g := Grounding{Query: q}
	hits, err := r.Retrieve(ctx, q, opts.topK())
	if err != nil {
		return g, fmt.Errorf("retrieve: %w", err)
	}
	if len(hits) == 0 {
		return g, nil // OOD / empty — caller refuses
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
		// Neighbours of the single best hit — the most likely to share context.
		if nb, err := r.Related(ctx, hits[0].ID); err == nil {
			for _, id := range nb {
				add(id)
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
		g.Chunks = append(g.Chunks, Chunk{ID: id, Title: c.Title, SourceURI: c.SourceURI, Body: body})
		used += len(body)
	}
	return g, nil
}

// Render formats the grounding as the context block handed to the answerer: each
// chunk fenced with its concept id + source_uri so the agent can cite by id and a
// reader can trace provenance. Returns "" when there are no chunks.
func (g Grounding) Render() string {
	if len(g.Chunks) == 0 {
		return ""
	}
	var b strings.Builder
	for i, c := range g.Chunks {
		if i > 0 {
			b.WriteString("\n\n")
		}
		fmt.Fprintf(&b, "[concept: %s | source: %s]\n%s\n%s", c.ID, sourceOrNone(c.SourceURI), c.Title, c.Body)
	}
	return b.String()
}

func sourceOrNone(s string) string {
	if strings.TrimSpace(s) == "" {
		return "(no source)"
	}
	return s
}
