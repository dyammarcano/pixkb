package rag

import (
	"context"
	"path/filepath"

	"pixkb/internal/embed"
	"pixkb/internal/okf"
	"pixkb/internal/query"
	"pixkb/internal/store/postgres"
	"pixkb/pkg/agents"
)

// HybridRetriever adapts the hybrid search + edge graph to rag.Retriever. Thin
// wrapper, no logic — the ranking lives in query.Hybrid, the graph in
// store.Related.
type HybridRetriever struct {
	Store  *postgres.Store
	Emb    embed.Embedder
	Filter postgres.Filter
}

// Retrieve runs the hybrid search and maps the top-k hits to rag.Hit.
func (h HybridRetriever) Retrieve(ctx context.Context, q string, k int) ([]Hit, error) {
	f := h.Filter
	f.Limit = k
	hits, err := query.Hybrid(ctx, h.Store, h.Emb, q, f)
	if err != nil {
		return nil, err
	}
	out := make([]Hit, 0, len(hits))
	for _, x := range hits {
		out = append(out, Hit{ID: x.ID, Title: x.Title, Type: x.Type, Score: x.Score})
	}
	return out, nil
}

// RetrieveMulti runs the multi-query expansion (query.MultiHybrid) instead of
// single-query Hybrid — same RRF fusion, same ranking math, just seeded from
// ExpandQuery's subqueries instead of one query string. Satisfies
// rag.MultiRetriever.
func (h HybridRetriever) RetrieveMulti(ctx context.Context, q string, k int) ([]Hit, error) {
	f := h.Filter
	f.Limit = k
	hits, err := query.MultiHybrid(ctx, h.Store, h.Emb, q, f)
	if err != nil {
		return nil, err
	}
	out := make([]Hit, 0, len(hits))
	for _, x := range hits {
		out = append(out, Hit{ID: x.ID, Title: x.Title, Type: x.Type, Score: x.Score})
	}
	return out, nil
}

// Related returns the bundle ids of a concept's graph neighbours.
func (h HybridRetriever) Related(ctx context.Context, id string) ([]string, error) {
	rel, err := h.Store.Related(ctx, id)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(rel))
	for _, r := range rel {
		out = append(out, r.ID)
	}
	return out, nil
}

// BundleSource reads concept bodies from the canonical bundle dir — the same
// source of truth concept_get reads, so the grounding is exactly the curated text.
type BundleSource struct{ Dir string }

// Concept reads and parses one bundle concept by its bundle-relative id.
func (b BundleSource) Concept(_ context.Context, id string) (okf.Concept, error) {
	return okf.ReadConcept(filepath.Join(b.Dir, filepath.FromSlash(id)), b.Dir)
}

// AgentGenerator runs the answerer agent through the fleet (air-gap-compliant:
// generation spends one subscription turn, never a metered API).
type AgentGenerator struct{ Agency *agents.Agency }

// Generate runs the answerer agent and returns its raw structured reply.
func (a AgentGenerator) Generate(ctx context.Context, prompt string) (string, error) {
	res, err := a.Agency.Run(ctx, "answerer", prompt)
	if err != nil {
		return "", err
	}
	return res.Text, nil
}

// Ask is the end-to-end RAG entry point: retrieve + augment, then synthesize. It
// returns the Grounding alongside the Answer so a surface can resolve each cited
// concept id back to its source_uri for display. When Options.Cache is set, a
// hit for CacheKey(q, Options.Epoch) short-circuits Synthesize entirely — the
// whole point being to skip a real subscription-agent turn on a repeated
// question. Answer.Text is redacted for PII/LGPD before being cached, so a
// cache hit and a cache miss return identically-redacted text. Grounding is
// always rebuilt (retrieval is local and cheap, never the agent), even on a
// cache hit, so citation source_uri resolution keeps working.
func Ask(ctx context.Context, r Retriever, cs ConceptSource, gen Generator, q string, opts Options) (Answer, Grounding, error) {
	g, err := BuildGrounding(ctx, r, cs, q, opts)
	if err != nil {
		return Answer{}, Grounding{}, err
	}

	var key string
	if opts.Cache != nil {
		key = CacheKey(q, opts.Epoch)
		if a, ok := opts.Cache.Get(key); ok {
			return a, g, nil
		}
	}

	a, err := Synthesize(ctx, gen, g)
	if err != nil {
		return Answer{}, g, err
	}
	if !opts.NoPIIFilter {
		a.Text = RedactPII(a.Text)
	}
	if opts.Cache != nil {
		opts.Cache.Put(key, a)
	}
	return a, g, nil
}

// SourceFor returns the source_uri of a cited concept id from the grounding, or
// "" if not present — a small helper for surfaces rendering citations.
func (g Grounding) SourceFor(id string) string {
	for _, c := range g.Chunks {
		if c.ID == id {
			return c.SourceURI
		}
	}
	return ""
}
