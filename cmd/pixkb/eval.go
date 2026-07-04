package main

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"pixkb/internal/evalkit"
)

// newEvalCmd is the deterministic-retrieval-gate surface Feature 6 of
// docs/SEARCH-CAPABILITY-SPEC.md asks for ("search eval or equivalent
// command surface"). Each subcommand loads a case file, runs the matching
// evalkit runner against the live KB, and prints a plain-text report — same
// spirit as eval/tophit.sh: numbers for a human to compare before/after a
// ranking change, not a pass/fail exit code.
func newEvalCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "eval",
		Short: "Deterministic retrieval evaluation gates (Feature 6 of docs/SEARCH-CAPABILITY-SPEC.md)",
	}
	root.AddCommand(newEvalMultiCmd(), newEvalSimilarCmd(), newEvalOODCmd(), newEvalExplainCmd(), newEvalAsOfCmd())
	return root
}

func newEvalMultiCmd() *cobra.Command {
	var dsn, file string
	var limit int
	cmd := &cobra.Command{
		Use:   "multi",
		Short: "Required-id coverage for multi-intent queries (query.MultiHybrid)",
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
			cases, err := evalkit.LoadPairCases(file)
			if err != nil {
				return err
			}
			results, err := evalkit.RunMultiCoverage(ctx, st, emb, cases, limit)
			if err != nil {
				return err
			}
			return printCoverageReport(cmd.OutOrStdout(), results)
		},
	}
	cmd.Flags().StringVar(&dsn, "dsn", "", "Postgres DSN")
	cmd.Flags().StringVar(&file, "file", "eval/cases-multi-ids.tsv", "coverage case file (query<TAB>id1,id2,...)")
	cmd.Flags().IntVar(&limit, "limit", 20, "max results per fused multi-query search")
	return cmd
}

// printCoverageReport writes one line per case plus an aggregate
// required-id-coverage percentage — the metric docs/SEARCH-CAPABILITY-SPEC.md
// Feature 6 names explicitly.
func printCoverageReport(w io.Writer, results []evalkit.CoverageResult) error {
	var foundSum, totalSum int
	for _, r := range results {
		fmt.Fprintf(w, "%d/%d  %.60s\n", r.Found, r.Total, r.Case.Query)
		foundSum += r.Found
		totalSum += r.Total
	}
	fmt.Fprintln(w, "----")
	if totalSum == 0 {
		fmt.Fprintln(w, "no cases")
		return nil
	}
	fmt.Fprintf(w, "cases=%d  required-id coverage=%d/%d (%.0f%%)\n",
		len(results), foundSum, totalSum, 100*float64(foundSum)/float64(totalSum))
	return nil
}

func newEvalSimilarCmd() *cobra.Command {
	var dsn, file string
	var limit int
	cmd := &cobra.Command{
		Use:   "similar",
		Short: "Expected-neighbor rank checks per concept family (similar.Similar)",
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
			cases, err := evalkit.LoadSimilarCases(file)
			if err != nil {
				return err
			}
			results, err := evalkit.RunSimilarFamily(ctx, st, emb, cfg.BundleDir, cases, limit)
			if err != nil {
				return err
			}
			return printRankReport(cmd.OutOrStdout(), results)
		},
	}
	cmd.Flags().StringVar(&dsn, "dsn", "", "Postgres DSN")
	cmd.Flags().StringVar(&file, "file", "eval/cases-similar-ids.tsv", "similarity case file (concept-id<TAB>mode<TAB>id1,id2,...)")
	cmd.Flags().IntVar(&limit, "limit", 20, "max results per similarity query")
	return cmd
}

// printRankReport writes one line per case's best rank plus top@1/top@5/MRR
// aggregates — the same metrics eval/tophit.sh reports, for the rank-based
// runners (similarity, explain-consistency).
func printRankReport(w io.Writer, results []evalkit.RankResult) error {
	var t1, t5 int
	var mrr float64
	for _, r := range results {
		if r.Rank > 0 {
			if r.Rank <= 1 {
				t1++
			}
			if r.Rank <= 5 {
				t5++
			}
			mrr += 1.0 / float64(r.Rank)
			fmt.Fprintf(w, "%-70.70s  rank=%d\n", r.Label, r.Rank)
		} else {
			fmt.Fprintf(w, "%-70.70s  rank=—\n", r.Label)
		}
	}
	fmt.Fprintln(w, "----")
	n := len(results)
	if n == 0 {
		fmt.Fprintln(w, "no cases")
		return nil
	}
	fmt.Fprintf(w, "cases=%d  top@1=%d (%.0f%%)  top@5=%d (%.0f%%)  MRR=%.3f\n",
		n, t1, 100*float64(t1)/float64(n), t5, 100*float64(t5)/float64(n), mrr/float64(n))
	return nil
}

func newEvalOODCmd() *cobra.Command {
	var dsn, file, preciseFile, fuzzyFile string
	var limit int
	cmd := &cobra.Command{
		Use:   "ood",
		Short: "Forbidden-id absence for out-of-domain queries (query.Hybrid)",
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
			queries, err := evalkit.LoadQueries(file)
			if err != nil {
				return err
			}
			precise, err := evalkit.LoadPairCases(preciseFile)
			if err != nil {
				return err
			}
			fuzzy, err := evalkit.LoadPairCases(fuzzyFile)
			if err != nil {
				return err
			}
			forbidden := evalkit.ForbiddenIDs(precise, fuzzy)
			results, err := evalkit.RunOOD(ctx, st, emb, queries, forbidden, limit)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			leaks := 0
			for _, r := range results {
				if len(r.Leaked) > 0 {
					leaks++
					fmt.Fprintf(out, "LEAK  %-60.60s  %v\n", r.Query, r.Leaked)
				} else {
					fmt.Fprintf(out, "clean %-60.60s\n", r.Query)
				}
			}
			fmt.Fprintln(out, "----")
			fmt.Fprintf(out, "cases=%d  clean=%d  leaked=%d\n", len(results), len(results)-leaks, leaks)
			return nil
		},
	}
	cmd.Flags().StringVar(&dsn, "dsn", "", "Postgres DSN")
	cmd.Flags().StringVar(&file, "file", "eval/cases-ood.tsv", "OOD query file (one query per line)")
	cmd.Flags().StringVar(&preciseFile, "precise-file", "eval/cases-precise-ids.tsv", "forbidden-id source: precise cases")
	cmd.Flags().StringVar(&fuzzyFile, "fuzzy-file", "eval/cases-fuzzy-ids.tsv", "forbidden-id source: fuzzy cases")
	cmd.Flags().IntVar(&limit, "limit", 5, "max results per OOD query")
	return cmd
}

func newEvalExplainCmd() *cobra.Command {
	var dsn, file string
	cmd := &cobra.Command{
		Use:   "explain",
		Short: "Structural consistency of --explain output (query.HybridExplain)",
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
			cases, err := evalkit.LoadPairCases(file)
			if err != nil {
				return err
			}
			issues, err := evalkit.RunExplainConsistency(ctx, st, emb, cases)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			for _, iss := range issues {
				fmt.Fprintf(out, "ISSUE  %-50.50s  %s\n", iss.Query, iss.Detail)
			}
			fmt.Fprintln(out, "----")
			fmt.Fprintf(out, "cases=%d  issues=%d\n", len(cases), len(issues))
			return nil
		},
	}
	cmd.Flags().StringVar(&dsn, "dsn", "", "Postgres DSN")
	cmd.Flags().StringVar(&file, "file", "eval/cases-precise-ids.tsv", "queries to check (only the query column is used)")
	return cmd
}

func newEvalAsOfCmd() *cobra.Command {
	var dsn, file string
	cmd := &cobra.Command{
		Use:   "asof",
		Short: "As-of-at-latest-epoch equals unfiltered (query.Hybrid + Filter.AsOfEpoch)",
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
			s, err := st.Stats(ctx)
			if err != nil {
				return err
			}
			cases, err := evalkit.LoadPairCases(file)
			if err != nil {
				return err
			}
			issues, err := evalkit.RunAsOfInvariant(ctx, st, emb, cases, s.LatestEpoch)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			for _, iss := range issues {
				fmt.Fprintf(out, "ISSUE  %-50.50s  %s\n", iss.Query, iss.Detail)
			}
			fmt.Fprintln(out, "----")
			fmt.Fprintf(out, "cases=%d  latest-epoch=%d  issues=%d\n", len(cases), s.LatestEpoch, len(issues))
			return nil
		},
	}
	cmd.Flags().StringVar(&dsn, "dsn", "", "Postgres DSN")
	cmd.Flags().StringVar(&file, "file", "eval/cases-precise-ids.tsv", "queries to check (only the query column is used)")
	return cmd
}
