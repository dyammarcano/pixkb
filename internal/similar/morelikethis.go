package similar

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"pixkb/internal/embed"
	"pixkb/internal/okf"
	"pixkb/internal/query"
	"pixkb/internal/store/postgres"
)

// mltBodyChars caps how much of a concept's body feeds the synthetic
// more-like-this query — the goal is topical signal for retrieval, not a
// full-body echo (which would just re-run search on near-arbitrary length
// text and cost more without adding recall quality).
const mltBodyChars = 500

// MoreLikeThis builds a synthetic query from id's own title, intent_terms,
// and a body excerpt, then runs it through the existing, unmodified
// query.Hybrid — reusing precise/fuzzy-safe ranking rather than inventing a
// second one. Each hit's Why is derived from Hybrid's own Arm field
// (fts->lexical, vector->semantic, both->both), so more-like-this directly
// reuses the multi-query-retrieval plan's Score/Arm provenance work.
func MoreLikeThis(ctx context.Context, s Store, emb embed.Embedder, bundleDir, id string, f postgres.Filter) ([]Hit, error) {
	c, err := okf.ReadConcept(filepath.Join(bundleDir, filepath.FromSlash(id)), bundleDir)
	if err != nil {
		return nil, fmt.Errorf("similar: read concept %q: %w", id, err)
	}

	q := c.Title
	if c.IntentTerms != "" {
		q += " " + c.IntentTerms
	}
	q += " " + truncate(c.Body, mltBodyChars)

	hits, err := query.Hybrid(ctx, s, emb, q, withHeadroom(f))
	if err != nil {
		return nil, fmt.Errorf("similar: more-like-this hybrid search for %q: %w", id, err)
	}

	limit := f.Limit
	if limit <= 0 {
		limit = defaultLimit
	}
	out := make([]Hit, 0, len(hits))
	for _, h := range hits {
		if h.ID == id {
			continue
		}
		out = append(out, Hit{Hit: h, Why: whyFromArm(h.Arm)})
		if len(out) >= limit {
			break
		}
	}
	for i := range out {
		out[i].Rank = i + 1
	}
	return out, nil
}

// truncate returns body's first n bytes trimmed back to a valid UTF-8 rune
// boundary (never splits a multi-byte character mid-sequence — this corpus
// is Portuguese-heavy with common accented characters, and a split rune
// here would feed invalid UTF-8 straight into query.Hybrid's FTS query
// parameter, which Postgres rejects outright).
func truncate(body string, n int) string {
	if len(body) <= n {
		return body
	}
	return strings.ToValidUTF8(body[:n], "")
}

// whyFromArm maps query.Hybrid's Arm field ("fts"/"vector"/"both"/"") onto
// this package's Signal constants.
func whyFromArm(arm string) []string {
	switch arm {
	case "both":
		return []string{SignalSemantic, SignalLexical}
	case "vector":
		return []string{SignalSemantic}
	case "fts":
		return []string{SignalLexical}
	default:
		return nil
	}
}
