package main

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"pixkb/internal/okf"
	"pixkb/internal/searchhealth"
)

// newSearchHealthCmd is the one-command search-readiness health surface
// Feature 8 of docs/SEARCH-CAPABILITY-SPEC.md ("Search Quality Operations")
// asks for: missing intent_terms, noisy titles, sparse graph links,
// embedding coverage/consistency, failing deterministic eval cases, and a
// prioritized re-enrichment recommendation list — all from one run.
func newSearchHealthCmd() *cobra.Command {
	var dsn string
	var asJSON bool
	var evalFiles []string
	cmd := &cobra.Command{
		Use:   "search-health",
		Short: "Report search-readiness health: enrichment gaps, graph sparsity, embedding coverage, eval regressions",
		RunE: func(cmd *cobra.Command, _ []string) error {
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
			concepts, err := okf.ReadBundle(cfg.BundleDir)
			if err != nil {
				return fmt.Errorf("read bundle %q: %w", cfg.BundleDir, err)
			}

			rep, err := searchhealth.BuildReport(ctx, concepts, st, emb, evalFiles...)
			if err != nil {
				return err
			}

			if asJSON {
				enc := json.NewEncoder(cmd.OutOrStdout())
				enc.SetIndent("", "  ")
				return enc.Encode(rep)
			}
			return printSearchHealthReport(cmd.OutOrStdout(), rep)
		},
	}
	cmd.Flags().StringVar(&dsn, "dsn", "", "Postgres DSN")
	cmd.Flags().BoolVar(&asJSON, "json", false, "emit the full report as JSON")
	cmd.Flags().StringSliceVar(&evalFiles, "eval-file", []string{"eval/cases-precise-ids.tsv", "eval/cases-fuzzy-ids.tsv"}, "deterministic eval case files to check for regressions (empty to skip)")
	return cmd
}

// printSearchHealthReport writes a compact, human-readable summary followed
// by the prioritized re-enrichment recommendation list. Enrichment gaps are
// reported as counts, not per-item errors — the spec's own acceptance
// criterion warns against "treating all missing enrichment as errors";
// individual concept ids are visible in --json or in the recommendation
// list for concepts with multiple/stronger signals.
func printSearchHealthReport(w io.Writer, rep searchhealth.Report) error {
	fmt.Fprintf(w, "concepts:            %d\n", rep.TotalConcepts)
	fmt.Fprintf(w, "missing intent_terms: %d\n", len(rep.MissingIntentTerms))
	fmt.Fprintf(w, "noisy titles:        %d\n", len(rep.NoisyTitles))
	fmt.Fprintf(w, "sparse graph links:  %d\n", len(rep.SparseGraph))
	fmt.Fprintf(w, "embedding coverage:  %d/%d concepts", rep.Embedding.EmbeddedConcepts, rep.Embedding.TotalConcepts)
	if !rep.Embedding.Consistent() {
		fmt.Fprintf(w, "  ⚠ %d distinct model/dim combinations found (inconsistent)\n", len(rep.Embedding.Models))
	} else {
		fmt.Fprintln(w)
	}

	if len(rep.EvalRegressions) > 0 {
		failed := 0
		for _, r := range rep.EvalRegressions {
			if r.Rank == 0 {
				failed++
			}
		}
		fmt.Fprintf(w, "eval regressions:    %d/%d cases found no acceptable hit\n", failed, len(rep.EvalRegressions))
		for _, r := range rep.EvalRegressions {
			if r.Rank == 0 {
				fmt.Fprintf(w, "  FAIL  %.70s\n", r.Query)
			}
		}
	}

	if len(rep.Recommendations) == 0 {
		fmt.Fprintln(w, "\nno re-enrichment candidates — search readiness looks healthy")
		return nil
	}
	fmt.Fprintln(w, "\nre-enrichment candidates (highest priority first):")
	for _, r := range rep.Recommendations {
		kinds := make([]string, 0, len(r.Signals))
		for _, s := range r.Signals {
			kinds = append(kinds, s.Kind)
		}
		fmt.Fprintf(w, "  [score %d] %-40.40s %s\n", r.Score, r.ConceptID, strings.Join(kinds, ", "))
	}
	return nil
}
