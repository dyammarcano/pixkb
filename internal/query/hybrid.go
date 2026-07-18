// Package query exposes the hybrid FTS-plus-vector search entrypoint.
package query

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"pixkb/internal/embed"
	"pixkb/internal/store/postgres"
)

// typeWeight is a modest authority multiplier applied to the fused RRF score.
// Canonical, structured concepts (API endpoints, ISO messages) outrank extracted
// manual-PDF fragments when relevance is otherwise close — the dominant failure
// mode surfaced by the Codex judge was noisy manual fragments beating the exact
// endpoint/message. Manual sections are not penalised below 1.0 so manual-intent
// queries (e.g. a status section) still win on raw relevance.
func typeWeight(t string) float64 {
	switch t {
	case "ApiEndpoint", "PacsMessage", "CamtMessage":
		return 1.15
	default:
		return 1.0
	}
}

// titleBoostWeight is the magnitude of the title-overlap boost. Pure RRF fuses by
// RANK and discards score magnitude, so a concept whose TITLE answers the query
// only barely outscores a noisy body match at the same rank — letting an OCR
// fragment (e.g. secao-73 "FULANO DE TAL EIRELI") outrank the canonical concept
// titled for the topic. titleBoost rewards a concept by how many DISTINCT query
// tokens its title covers, so an unambiguous title-intent match wins. The boost
// is bounded (≤ 1+weight) and never penalises, so body-intent queries are
// unaffected when no title covers them. Measured against the judge suite.
const titleBoostWeight = 0.5

// titleStop are tokens too common to signal title intent; ignored on both sides.
var titleStop = map[string]bool{
	"de": true, "da": true, "do": true, "e": true, "a": true, "o": true,
	"no": true, "na": true, "em": true, "para": true, "com": true, "por": true,
	"of": true, "the": true, "to": true, "in": true, "and": true,
}

// foldTokens lowercases, strips Portuguese diacritics, splits on non-alphanumeric
// runs, and drops stopwords and single characters — yielding comparable tokens.
func foldTokens(s string) []string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		switch r {
		case 'á', 'â', 'ã', 'à', 'ä':
			b.WriteRune('a')
		case 'é', 'ê', 'è', 'ë':
			b.WriteRune('e')
		case 'í', 'î', 'ì', 'ï':
			b.WriteRune('i')
		case 'ó', 'ô', 'õ', 'ò', 'ö':
			b.WriteRune('o')
		case 'ú', 'û', 'ù', 'ü':
			b.WriteRune('u')
		case 'ç':
			b.WriteRune('c')
		default:
			if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
				b.WriteRune(r)
			} else {
				b.WriteRune(' ')
			}
		}
	}
	var out []string
	for t := range strings.FieldsSeq(b.String()) {
		if len(t) < 2 || titleStop[t] {
			continue
		}
		out = append(out, t)
	}
	return out
}

// titleBoost returns a multiplier in [1, 1+titleBoostWeight] proportional to the
// fraction of distinct query tokens that appear in the title.
func titleBoost(query, title string) float64 {
	q := foldTokens(query)
	if len(q) == 0 {
		return 1
	}
	inTitle := make(map[string]bool)
	for _, t := range foldTokens(title) {
		inTitle[t] = true
	}
	seen := make(map[string]bool, len(q))
	matched := 0
	for _, t := range q {
		if seen[t] {
			continue
		}
		seen[t] = true
		if inTitle[t] {
			matched++
		}
	}
	return 1 + titleBoostWeight*float64(matched)/float64(len(seen))
}

// Searcher is the subset of *postgres.Store that Hybrid needs. Defined as an
// interface so Hybrid can be unit-tested with a fake; *postgres.Store satisfies
// it directly.
type Searcher interface {
	FTS(ctx context.Context, q string, f postgres.Filter) ([]postgres.Hit, error)
	Vector(ctx context.Context, vec []float32, f postgres.Filter) ([]postgres.Hit, error)
}

const rrfK = 60

// Per-arm RRF weights. Kept equal: the two arms are complementary and
// query-dependent — lexical wins exact-term/title queries (e.g. "prazos de
// implementação"), the vector arm wins conceptual/endpoint queries (e.g.
// "consultar cobrança por txid", where short API concepts lose the lexical
// term-frequency race to verbose manual sections). Down-weighting either arm
// regressed the other's queries in the Codex judge, so neither is suppressed;
// FTS length-normalization (see search.go) does the precision work instead.
const (
	ftsArmWeight = 1.0
	vecArmWeight = 1.0
)

// vecScoreFloor is a cosine-similarity floor on the vector arm. Hashing vectors
// are hashed bag-of-words, so cosine is a lexical-overlap proxy: an out-of-domain
// query (no shared vocabulary — e.g. a weather question) scores near-zero against
// every concept, and the exact-kNN arm would otherwise still return its K
// least-bad matches as noise. Dropping sub-floor vector hits BEFORE fusion makes
// such a query (which also yields no FTS hits) return nothing, instead of
// unrelated concepts. The floor is deliberately low so genuine in-domain hits —
// which share vocabulary and score well above it — are never affected.
const vecScoreFloor = 0.05

// Explain captures the per-hit ranking components hybridCore computes
// internally, for callers that opt into search explanation (HybridExplain,
// CLI --explain, MCP explain: true). Rank fields are 1-based so 0 doubles as
// the "not present in this arm" sentinel; FinalScore always equals the
// corresponding Hit's Score field.
type Explain struct {
	FTSRank    int
	VecRank    int
	VecScore   float64
	TypeWeight float64
	TitleBoost float64
	FinalScore float64
	Arm        string
}

// Hybrid runs full-text and vector search for q and fuses the two result sets
// with reciprocal-rank fusion (RRF), returning hits ordered by fused score. The
// query is embedded once with emb for the vector arm. Titles are hydrated from
// whichever arm returned the concept. The Filter (type/tag/as-of) applies to
// both arms; f.Limit caps the fused result.
//
// Hybrid is a thin wrapper over hybridCore that discards the Explain side
// channel; behavior and output are unchanged from before hybridCore existed.
func Hybrid(ctx context.Context, s Searcher, emb embed.Embedder, q string, f postgres.Filter) ([]postgres.Hit, error) {
	hits, _, err := hybridCore(ctx, s, emb, q, f)
	return hits, err
}

// HybridExplain runs the same fused search as Hybrid but also returns the
// parallel []Explain slice of per-hit ranking components, for callers that
// opt into search explanation (CLI --explain, MCP explain: true).
func HybridExplain(ctx context.Context, s Searcher, emb embed.Embedder, q string, f postgres.Filter) ([]postgres.Hit, []Explain, error) {
	return hybridCore(ctx, s, emb, q, f)
}

// hybridCore is the shared implementation behind Hybrid and HybridExplain. It
// returns the fused hits AND a parallel []Explain slice — built in the same
// final loop, so index i in both slices always refers to the same hit — from
// data already computed while fusing (never a second computation, so there is
// no way for the two outputs to disagree).
func hybridCore(ctx context.Context, s Searcher, emb embed.Embedder, q string, f postgres.Filter) ([]postgres.Hit, []Explain, error) {
	ftsHits, err := s.FTS(ctx, q, f)
	if err != nil {
		return nil, nil, err
	}

	vecs, err := emb.Embed(ctx, []string{q})
	if err != nil {
		return nil, nil, err
	}
	if len(vecs) == 0 {
		return nil, nil, fmt.Errorf("embedder returned no vector for query")
	}
	vecHits, err := s.Vector(ctx, vecs[0], f)
	if err != nil {
		return nil, nil, err
	}
	// Drop near-zero-overlap vector hits so an out-of-domain query returns nothing
	// instead of hashing-vector noise (see vecScoreFloor). Filter in place.
	kept := vecHits[:0]
	for _, h := range vecHits {
		if h.Score >= vecScoreFloor {
			kept = append(kept, h)
		}
	}
	vecHits = kept

	// Build a title + type lookup (FTS title wins) and per-arm rank scores.
	titles := make(map[string]string)
	types := make(map[string]string)
	scores := make(map[string]float64)
	firstSeen := make(map[string]int)
	fromFTS := make(map[string]bool)
	fromVec := make(map[string]bool)
	// 1-based per-arm rank and per-arm vector score, tracked alongside
	// fromFTS/fromVec purely for Explain — never read by the fusion math below.
	ftsRank := make(map[string]int)
	vecRank := make(map[string]int)
	vecScoreByID := make(map[string]float64)
	order := 0
	note := func(h postgres.Hit, rank int, weight float64) {
		if _, ok := titles[h.ID]; !ok {
			titles[h.ID] = h.Title
		}
		if h.Type != "" {
			types[h.ID] = h.Type
		}
		scores[h.ID] += weight / float64(rrfK+rank)
		if _, ok := firstSeen[h.ID]; !ok {
			firstSeen[h.ID] = order
			order++
		}
	}
	for i, h := range ftsHits {
		note(h, i, ftsArmWeight)
		fromFTS[h.ID] = true
		ftsRank[h.ID] = i + 1
	}
	for i, h := range vecHits {
		note(h, i, vecArmWeight)
		fromVec[h.ID] = true
		vecRank[h.ID] = i + 1
		vecScoreByID[h.ID] = h.Score
	}

	// Apply the type-authority weight, then sort (score desc, first-seen, id).
	typeWeightByID := make(map[string]float64, len(scores))
	titleBoostByID := make(map[string]float64, len(scores))
	ids := make([]string, 0, len(scores))
	for id := range scores {
		tw := typeWeight(types[id])
		tb := titleBoost(q, titles[id])
		typeWeightByID[id] = tw
		titleBoostByID[id] = tb
		scores[id] *= tw * tb
		ids = append(ids, id)
	}
	sort.SliceStable(ids, func(a, b int) bool {
		if scores[ids[a]] != scores[ids[b]] {
			return scores[ids[a]] > scores[ids[b]]
		}
		if firstSeen[ids[a]] != firstSeen[ids[b]] {
			return firstSeen[ids[a]] < firstSeen[ids[b]]
		}
		return ids[a] < ids[b]
	})

	limit := f.Limit
	if limit <= 0 {
		limit = 20
	}
	out := make([]postgres.Hit, 0, len(ids))
	explains := make([]Explain, 0, len(ids))
	for i, id := range ids {
		if i >= limit {
			break
		}
		arm := armLabel(fromFTS[id], fromVec[id])
		out = append(out, postgres.Hit{
			ID: id, Title: titles[id], Type: types[id], Rank: i + 1,
			Score: scores[id],
			Arm:   arm,
		})
		explains = append(explains, Explain{
			FTSRank:    ftsRank[id],
			VecRank:    vecRank[id],
			VecScore:   vecScoreByID[id],
			TypeWeight: typeWeightByID[id],
			TitleBoost: titleBoostByID[id],
			FinalScore: scores[id],
			Arm:        arm,
		})
	}
	return out, explains, nil
}

// armLabel names which retrieval arm(s) surfaced a hit, for provenance
// (used directly by query.MultiHybrid, and by any future search-explanation
// surface).
func armLabel(fts, vec bool) string {
	switch {
	case fts && vec:
		return "both"
	case fts:
		return "fts"
	case vec:
		return "vector"
	default:
		return ""
	}
}
