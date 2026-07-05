package kbmcp

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"pixkb/internal/rag"
	"pixkb/internal/store/postgres"
)

type askIn struct {
	Question      string  `json:"question" jsonschema:"natural-language question to answer from the KB"`
	Type          string  `json:"type,omitempty" jsonschema:"optional concept-type filter for retrieval"`
	TopK          int     `json:"top_k,omitempty" jsonschema:"concepts to ground on (default 6)"`
	Expand        bool    `json:"expand,omitempty" jsonschema:"also ground on the seed hit(s)' graph neighbours"`
	Multi         bool    `json:"multi,omitempty" jsonschema:"expand the question into multiple subqueries before retrieving"`
	Diversify     bool    `json:"diversify,omitempty" jsonschema:"prefer one concept per type before filling remaining grounding slots by rank"`
	ExpandSeeds   int     `json:"expand_seeds,omitempty" jsonschema:"graph-neighbour seed hits to expand when expand is set (0 = default 1)"`
	ExpandSimilar bool    `json:"expand_similar,omitempty" jsonschema:"also ground on the top hit's concept-similarity neighbours (internal/similar.Similar, hybrid mode)"`
	MinScore      float64 `json:"min_score,omitempty" jsonschema:"refuse when the top retrieved hit's score is below this (0 = disabled)"`
	NoPIIFilter   bool    `json:"no_pii_filter,omitempty" jsonschema:"disable the deterministic PII/LGPD redaction post-filter (debugging only)"`
	NoCache       bool    `json:"no_cache,omitempty" jsonschema:"bypass the answer cache and force a fresh agent turn"`
}

type askCitationOut struct {
	ID     string `json:"id"`
	Source string `json:"source,omitempty"`
}
type askOut struct {
	Answer    string           `json:"answer"`
	Refused   bool             `json:"refused"`
	Citations []askCitationOut `json:"citations"`
}

// askCache is shared across every kb_ask call for this server's lifetime — the
// CLI gets a fresh cache per invocation (no reuse possible there), but the MCP
// server is long-running, so this is the path that actually skips repeated
// agent turns in practice.
var askCache = rag.NewLRUCache(256)

// registerAsk wires the RAG answer verb: retrieve + augment + a grounded, cited
// answer synthesized by the answerer agent. Registered only when an Agency is
// present (generation needs the subscription fleet). Refuses (refused=true) when
// retrieval is empty/off-domain or the context does not support an answer.
func registerAsk(s *mcp.Server, d Deps) {
	mcp.AddTool(s, &mcp.Tool{
		Name:        "kb_ask",
		Description: "Answer a question from the Pix/SPB KB — grounded, citation-backed, refuses when unsupported (RAG).",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in askIn) (*mcp.CallToolResult, askOut, error) {
		stats, err := d.Store.Stats(ctx)
		if err != nil {
			return nil, askOut{}, err
		}
		cache := rag.AnswerCache(askCache)
		if in.NoCache {
			cache = nil
		}
		ans, g, err := rag.Ask(ctx,
			rag.HybridRetriever{Store: d.Store, Emb: d.Emb, Filter: postgres.Filter{Type: in.Type}, BundleDir: d.Bundle},
			rag.BundleSource{Dir: d.Bundle},
			rag.AgentGenerator{Agency: d.Agency},
			in.Question,
			rag.Options{
				TopK:          in.TopK,
				ExpandRelated: in.Expand,
				MultiQuery:    in.Multi,
				Diversify:     in.Diversify,
				ExpandSeeds:   in.ExpandSeeds,
				ExpandSimilar: in.ExpandSimilar,
				MinScore:      in.MinScore,
				NoPIIFilter:   in.NoPIIFilter,
				Cache:         cache,
				Epoch:         stats.LatestEpoch,
			},
		)
		if err != nil {
			return nil, askOut{}, err
		}
		out := askOut{Answer: ans.Text, Refused: ans.Refused}
		for _, id := range ans.Citations {
			out.Citations = append(out.Citations, askCitationOut{ID: id, Source: g.SourceFor(id)})
		}
		return textResult(fmt.Sprintf("answer (%d citations, refused=%v)", len(out.Citations), out.Refused)), out, nil
	})
}
