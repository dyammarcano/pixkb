package rag

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"

	"github.com/inovacc/corral"
	"pixkb/internal/embed"
	"pixkb/internal/okf"
	"pixkb/internal/query"
	"pixkb/internal/similar"
	"pixkb/internal/store/postgres"
)

// ErrRateLimited is returned by AgentGenerator.Generate when the agent fleet's
// provider has reached its usage limit and the run was withheld (corral signals
// this with corral.ErrRateLimited). Surfaces can errors.Is against this to tell
// the operator to try again later, instead of showing an opaque error. NOTE:
// there is no automatic backoff-retry — corral exposes no reset time, and the
// run was withheld, so a within-request retry would spin; retry is deferred
// until a reset window is available.
var ErrRateLimited = errors.New("rag: agent fleet rate-limited")

// HybridRetriever adapts the hybrid search + edge graph to rag.Retriever. Thin
// wrapper, no logic — the ranking lives in query.Hybrid, the graph in
// store.Related.
type HybridRetriever struct {
	Store     *postgres.Store
	Emb       embed.Embedder
	Filter    postgres.Filter
	BundleDir string // bundle root, needed by RetrieveSimilar (similar.Similar reads concept type from the bundle)
}

// Retrieve runs the hybrid search and maps the top-k hits to rag.Hit.
func (h HybridRetriever) Retrieve(ctx context.Context, q string, k int) ([]Hit, error) {
	f := h.Filter
	f.Limit = k
	hits, err := query.Hybrid(ctx, h.Store, h.Emb, q, f)
	if err != nil {
		return nil, err
	}
	return toHits(hits), nil
}

// toHits projects search hits onto the rag.Hit shape — the single mapping shared
// by Retrieve/RetrieveMulti/RetrieveSimilar.
func toHits(hits []postgres.Hit) []Hit {
	out := make([]Hit, 0, len(hits))
	for _, x := range hits {
		out = append(out, Hit{ID: x.ID, Title: x.Title, Type: x.Type, Score: x.Score})
	}
	return out
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
	ph := make([]postgres.Hit, len(hits))
	for i, x := range hits {
		ph[i] = x.Hit
	}
	return toHits(ph), nil
}

// RetrieveSimilar pulls concept-similarity neighbours of id via
// internal/similar's hybrid mode with graph included — satisfies
// rag.SimilarRetriever.
func (h HybridRetriever) RetrieveSimilar(ctx context.Context, id string) ([]Hit, error) {
	hits, err := similar.Similar(ctx, h.Store, h.Emb, h.BundleDir, id, similar.Options{
		Mode:         "hybrid",
		IncludeGraph: true,
		Filter:       h.Filter,
	})
	if err != nil {
		return nil, err
	}
	ph := make([]postgres.Hit, len(hits))
	for i, x := range hits {
		ph[i] = x.Hit
	}
	return toHits(ph), nil
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
type AgentGenerator struct{ Agency *corral.Agency }

// Generate runs the answerer agent and returns its raw structured reply. A
// provider rate-limit (corral.ErrRateLimited) is mapped to the typed
// ErrRateLimited so surfaces can present a clear "try again later" instead of an
// opaque error. When the provider reports itself already exhausted, the run is
// short-circuited to avoid spending a doomed attempt.
func (a AgentGenerator) Generate(ctx context.Context, prompt string) (string, error) {
	if status, supported, err := a.Agency.LimitStatus(); err == nil && supported && status.Exhausted() {
		return "", ErrRateLimited
	}
	res, err := a.Agency.Run(ctx, "answerer", prompt)
	if err != nil {
		return "", mapGenErr(err)
	}
	return res.Text, nil
}

// mapGenErr translates a corral run error into pixkb's typed rag error where one
// applies, leaving all other errors unchanged. Split out so it is unit-testable
// without constructing a real *corral.Agency.
func mapGenErr(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, corral.ErrRateLimited) {
		// Wrap both so errors.Is matches the typed rag sentinel and the corral cause.
		return fmt.Errorf("%w: %w", ErrRateLimited, err)
	}
	return err
}

// Ask is the end-to-end RAG entry point: retrieve + augment, then synthesize. It
// returns the Grounding alongside the Answer so a surface can resolve each cited
// concept id back to its source_uri for display. When Options.Cache is set (and
// only when the PII filter is ON), a hit for cacheKeyFor(q, opts) — which folds
// in the epoch and every retrieval-scoping option — short-circuits Synthesize
// entirely, to skip a real subscription-agent turn on a repeated question. A
// NoPIIFilter request never reads or writes the cache, so un-redacted text can
// never be cached and served to a later normal request. Grounding is always
// rebuilt (retrieval is local and cheap, never the agent), even on a cache hit,
// so citation source_uri resolution keeps working.
func Ask(ctx context.Context, r Retriever, cs ConceptSource, gen Generator, q string, opts Options) (Answer, Grounding, error) {
	g, err := BuildGrounding(ctx, r, cs, q, opts)
	if err != nil {
		return Answer{}, Grounding{}, err
	}

	// Never touch the cache when the PII filter is disabled: a NoPIIFilter
	// answer holds un-redacted text that must not persist in a shared process
	// cache where a later normal request could be served it. The key also folds
	// in the retrieval scope so differently-scoped answers never collide.
	var key string
	useCache := opts.Cache != nil && !opts.NoPIIFilter
	if useCache {
		key = cacheKeyFor(q, opts)
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
	if useCache {
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
