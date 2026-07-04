package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"pixkb/internal/rag"
	"pixkb/internal/store/postgres"
	"pixkb/pkg/agents"
	_ "pixkb/pkg/agents/all" // registers codex/claude/agy providers
)

// newAskCmd is the RAG surface: `pixkb ask "<question>"` returns a grounded,
// citation-backed answer synthesized from the KB. Retrieval is the hybrid search;
// generation runs on the subscription agent fleet (air-gap: no metered API, no
// native model runtime). The answerer refuses when retrieval is empty/off-domain
// or the context does not support an answer — a wrong fact about a normative
// arrangement is worse than no answer.
func newAskCmd() *cobra.Command {
	var dsn, provider, typ string
	var topK, expandSeeds int
	var expand, asJSON, multi, diversify, noPIIFilter bool
	var minScore float64
	cmd := &cobra.Command{
		Use:   "ask <question>",
		Short: "Ask the KB a question — grounded, cited answer (RAG)",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := loadConfig()
			if dsn != "" {
				cfg.DSN = dsn
			}
			ctx := cmd.Context()
			st, err := openStore(ctx, cfg)
			if err != nil {
				return err
			}
			defer st.Close()
			emb, err := newEmbedder(cfg)
			if err != nil {
				return err
			}
			dir, err := os.Getwd()
			if err != nil {
				return err
			}
			ag, err := agents.NewAgency(provider, dir)
			if err != nil {
				return err
			}
			defer func() { _ = ag.Close() }()

			ans, g, err := rag.Ask(ctx,
				rag.HybridRetriever{Store: st, Emb: emb, Filter: postgres.Filter{Type: typ}},
				rag.BundleSource{Dir: cfg.BundleDir},
				rag.AgentGenerator{Agency: ag},
				strings.Join(args, " "),
				rag.Options{
					TopK:          topK,
					ExpandRelated: expand,
					MultiQuery:    multi,
					Diversify:     diversify,
					ExpandSeeds:   expandSeeds,
					MinScore:      minScore,
					NoPIIFilter:   noPIIFilter,
				},
			)
			if err != nil {
				return err
			}
			return renderAnswer(cmd.OutOrStdout(), ans, g, asJSON)
		},
	}
	cmd.Flags().StringVar(&dsn, "dsn", "", "Postgres DSN")
	cmd.Flags().StringVar(&provider, "provider", "claude", "answerer backend: claude|codex|agy")
	cmd.Flags().StringVar(&typ, "type", "", "restrict retrieval to a concept type")
	cmd.Flags().IntVar(&topK, "top-k", 0, "concepts to ground on (0 = default 6)")
	cmd.Flags().BoolVar(&expand, "expand", false, "also ground on the top hit's graph neighbours")
	cmd.Flags().BoolVar(&asJSON, "json", false, "emit answer + citations as JSON")
	cmd.Flags().BoolVar(&multi, "multi", false, "expand the question into multiple subqueries before retrieving (query.MultiHybrid)")
	cmd.Flags().BoolVar(&diversify, "diversify", false, "prefer one concept per type before filling remaining grounding slots by rank")
	cmd.Flags().IntVar(&expandSeeds, "expand-seeds", 0, "graph-neighbour seed hits to expand when --expand is set (0 = default 1)")
	cmd.Flags().Float64Var(&minScore, "min-score", 0, "refuse when the top retrieved hit's score is below this (0 = disabled)")
	cmd.Flags().BoolVar(&noPIIFilter, "no-pii-filter", false, "disable the deterministic PII/LGPD redaction post-filter (debugging only — do not use for output shown to end users)")
	return cmd
}

// askJSON is the machine-readable shape of an answer with resolved citations.
type askJSON struct {
	Answer    string        `json:"answer"`
	Refused   bool          `json:"refused"`
	Citations []askCitation `json:"citations"`
}

type askCitation struct {
	ID     string `json:"id"`
	Source string `json:"source,omitempty"`
}

func renderAnswer(w interface{ Write([]byte) (int, error) }, a rag.Answer, g rag.Grounding, asJSON bool) error {
	cites := make([]askCitation, 0, len(a.Citations))
	for _, id := range a.Citations {
		cites = append(cites, askCitation{ID: id, Source: g.SourceFor(id)})
	}
	if asJSON {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(askJSON{Answer: a.Text, Refused: a.Refused, Citations: cites})
	}
	_, _ = fmt.Fprintln(w, a.Text)
	if len(cites) > 0 {
		_, _ = fmt.Fprintln(w, "\nCitations:")
		for _, c := range cites {
			if c.Source != "" {
				_, _ = fmt.Fprintf(w, "  - %s  (%s)\n", c.ID, c.Source)
			} else {
				_, _ = fmt.Fprintf(w, "  - %s\n", c.ID)
			}
		}
	}
	return nil
}
