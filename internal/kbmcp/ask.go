package kbmcp

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"pixkb/internal/rag"
	"pixkb/internal/store/postgres"
)

type askIn struct {
	Question string `json:"question" jsonschema:"natural-language question to answer from the KB"`
	Type     string `json:"type,omitempty" jsonschema:"optional concept-type filter for retrieval"`
	TopK     int    `json:"top_k,omitempty" jsonschema:"concepts to ground on (default 6)"`
	Expand   bool   `json:"expand,omitempty" jsonschema:"also ground on the top hit's graph neighbours"`
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

// registerAsk wires the RAG answer verb: retrieve + augment + a grounded, cited
// answer synthesized by the answerer agent. Registered only when an Agency is
// present (generation needs the subscription fleet). Refuses (refused=true) when
// retrieval is empty/off-domain or the context does not support an answer.
func registerAsk(s *mcp.Server, d Deps) {
	mcp.AddTool(s, &mcp.Tool{
		Name:        "kb_ask",
		Description: "Answer a question from the Pix/SPB KB — grounded, citation-backed, refuses when unsupported (RAG).",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in askIn) (*mcp.CallToolResult, askOut, error) {
		ans, g, err := rag.Ask(ctx,
			rag.HybridRetriever{Store: d.Store, Emb: d.Emb, Filter: postgres.Filter{Type: in.Type}},
			rag.BundleSource{Dir: d.Bundle},
			rag.AgentGenerator{Agency: d.Agency},
			in.Question,
			rag.Options{TopK: in.TopK, ExpandRelated: in.Expand},
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
