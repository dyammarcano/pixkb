package main

import (
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"

	"pixkb/internal/evalkit"
	"pixkb/internal/rag"
	"pixkb/pkg/agents"
	_ "pixkb/pkg/agents/all" // registers codex/claude/agy providers
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
	root.AddCommand(newEvalMultiCmd(), newEvalSimilarCmd(), newEvalOODCmd(), newEvalExplainCmd(), newEvalAsOfCmd(), newEvalRAGDiversityCmd())
	return root
}

func newEvalMultiCmd() *cobra.Command {
	var dsn, file string
	var limit int
	var failUnder float64
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
			coverage, err := printCoverageReport(cmd.OutOrStdout(), results)
			if err != nil {
				return err
			}
			return checkFailUnder("multi", coverage, failUnder)
		},
	}
	cmd.Flags().StringVar(&dsn, "dsn", "", "Postgres DSN")
	cmd.Flags().StringVar(&file, "file", "eval/cases-multi-ids.tsv", "coverage case file (query<TAB>id1,id2,...)")
	cmd.Flags().IntVar(&limit, "limit", 20, "max results per fused multi-query search")
	cmd.Flags().Float64Var(&failUnder, "fail-under", 0, "fail if required-id coverage drops below this percentage (0-100); 0 or unset = never fail")
	return cmd
}

// printCoverageReport writes one line per case plus an aggregate
// required-id-coverage percentage — the metric docs/SEARCH-CAPABILITY-SPEC.md
// Feature 6 names explicitly. It returns that percentage (0-100 scale) so
// callers can optionally gate on it via --fail-under; a return of -1 means
// the metric is undefined (no cases had any required ids), in which case
// --fail-under must never trigger.
func printCoverageReport(w io.Writer, results []evalkit.CoverageResult) (float64, error) {
	var foundSum, totalSum int
	for _, r := range results {
		_, _ = fmt.Fprintf(w, "%d/%d  %.60s\n", r.Found, r.Total, r.Case.Query)
		foundSum += r.Found
		totalSum += r.Total
	}
	_, _ = fmt.Fprintln(w, "----")
	if totalSum == 0 {
		_, _ = fmt.Fprintln(w, "no cases")
		return -1, nil
	}
	coverage := 100 * float64(foundSum) / float64(totalSum)
	_, _ = fmt.Fprintf(w, "cases=%d  required-id coverage=%d/%d (%.0f%%)\n",
		len(results), foundSum, totalSum, coverage)
	return coverage, nil
}

// checkFailUnder is the shared opt-in CI-gating check for every `pixkb eval`
// subcommand. Every subcommand always prints its report regardless; this is
// called afterward to decide whether to also fail the process. failUnder <=
// 0 means the flag was left at its zero value (never fail — the default,
// unchanged behavior). metric < 0 means the subcommand's headline metric is
// undefined (e.g. no cases were loaded), which also never fails since there
// is nothing meaningful to gate on. metric and failUnder are both on the
// same 0-100 percentage scale.
func checkFailUnder(label string, metric, failUnder float64) error {
	if failUnder <= 0 || metric < 0 {
		return nil
	}
	if metric < failUnder {
		return fmt.Errorf("eval %s: metric %.1f%% below --fail-under %.1f%%", label, metric, failUnder)
	}
	return nil
}

func newEvalSimilarCmd() *cobra.Command {
	var dsn, file string
	var limit int
	var failUnder float64
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
			top5, err := printRankReport(cmd.OutOrStdout(), results)
			if err != nil {
				return err
			}
			return checkFailUnder("similar", top5, failUnder)
		},
	}
	cmd.Flags().StringVar(&dsn, "dsn", "", "Postgres DSN")
	cmd.Flags().StringVar(&file, "file", "eval/cases-similar-ids.tsv", "similarity case file (concept-id<TAB>mode<TAB>id1,id2,...)")
	cmd.Flags().IntVar(&limit, "limit", 20, "max results per similarity query")
	cmd.Flags().Float64Var(&failUnder, "fail-under", 0, "fail if top@5 drops below this percentage (0-100); 0 or unset = never fail")
	return cmd
}

// printRankReport writes one line per case's best rank plus top@1/top@5/MRR
// aggregates — the same metrics eval/tophit.sh reports, for the rank-based
// runners (similarity, explain-consistency). It returns top@5 (0-100 scale)
// as the headline metric for --fail-under gating; a return of -1 means no
// cases were run, so --fail-under must never trigger.
func printRankReport(w io.Writer, results []evalkit.RankResult) (float64, error) {
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
			_, _ = fmt.Fprintf(w, "%-70.70s  rank=%d\n", r.Label, r.Rank)
		} else {
			_, _ = fmt.Fprintf(w, "%-70.70s  rank=—\n", r.Label)
		}
	}
	_, _ = fmt.Fprintln(w, "----")
	n := len(results)
	if n == 0 {
		_, _ = fmt.Fprintln(w, "no cases")
		return -1, nil
	}
	top5 := 100 * float64(t5) / float64(n)
	_, _ = fmt.Fprintf(w, "cases=%d  top@1=%d (%.0f%%)  top@5=%d (%.0f%%)  MRR=%.3f\n",
		n, t1, 100*float64(t1)/float64(n), t5, top5, mrr/float64(n))
	return top5, nil
}

func newEvalOODCmd() *cobra.Command {
	var dsn, file, preciseFile, fuzzyFile string
	var limit int
	var failUnder float64
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
			cleanRate := printOODReport(cmd.OutOrStdout(), results)
			return checkFailUnder("ood", cleanRate, failUnder)
		},
	}
	cmd.Flags().StringVar(&dsn, "dsn", "", "Postgres DSN")
	cmd.Flags().StringVar(&file, "file", "eval/cases-ood.tsv", "OOD query file (one query per line)")
	cmd.Flags().StringVar(&preciseFile, "precise-file", "eval/cases-precise-ids.tsv", "forbidden-id source: precise cases")
	cmd.Flags().StringVar(&fuzzyFile, "fuzzy-file", "eval/cases-fuzzy-ids.tsv", "forbidden-id source: fuzzy cases")
	cmd.Flags().IntVar(&limit, "limit", 5, "max results per OOD query")
	cmd.Flags().Float64Var(&failUnder, "fail-under", 0, "fail if the clean rate (non-leaked fraction) drops below this percentage (0-100); 0 or unset = never fail")
	return cmd
}

// printOODReport writes the existing per-query LEAK/clean lines plus the
// aggregate "cases=.. clean=.. leaked=.." line, byte-for-byte as before, and
// additionally returns the clean rate (0-100 scale) for --fail-under gating.
// A return of -1 means no queries were run, so --fail-under must never
// trigger.
func printOODReport(w io.Writer, results []evalkit.OODResult) float64 {
	leaks := 0
	for _, r := range results {
		if len(r.Leaked) > 0 {
			leaks++
			_, _ = fmt.Fprintf(w, "LEAK  %-60.60s  %v\n", r.Query, r.Leaked)
		} else {
			_, _ = fmt.Fprintf(w, "clean %-60.60s\n", r.Query)
		}
	}
	_, _ = fmt.Fprintln(w, "----")
	_, _ = fmt.Fprintf(w, "cases=%d  clean=%d  leaked=%d\n", len(results), len(results)-leaks, leaks)
	if len(results) == 0 {
		return -1
	}
	return 100 * float64(len(results)-leaks) / float64(len(results))
}

func newEvalExplainCmd() *cobra.Command {
	var dsn, file string
	var failUnder float64
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
			consistency := printExplainReport(cmd.OutOrStdout(), cases, issues)
			return checkFailUnder("explain", consistency, failUnder)
		},
	}
	cmd.Flags().StringVar(&dsn, "dsn", "", "Postgres DSN")
	cmd.Flags().StringVar(&file, "file", "eval/cases-precise-ids.tsv", "queries to check (only the query column is used)")
	cmd.Flags().Float64Var(&failUnder, "fail-under", 0, "fail if the structural-consistency rate (cases with zero issues) drops below this percentage (0-100); 0 or unset = never fail")
	return cmd
}

// printExplainReport writes the existing per-issue and aggregate lines,
// byte-for-byte as before, and additionally returns the structural
// consistency rate — the percentage of distinct cases with zero issues
// (0-100 scale) — for --fail-under gating. A return of -1 means no cases
// were run, so --fail-under must never trigger.
func printExplainReport(w io.Writer, cases []evalkit.PairCase, issues []evalkit.ExplainIssue) float64 {
	for _, iss := range issues {
		_, _ = fmt.Fprintf(w, "ISSUE  %-50.50s  %s\n", iss.Query, iss.Detail)
	}
	_, _ = fmt.Fprintln(w, "----")
	_, _ = fmt.Fprintf(w, "cases=%d  issues=%d\n", len(cases), len(issues))
	if len(cases) == 0 {
		return -1
	}
	faulty := make(map[string]bool, len(issues))
	for _, iss := range issues {
		faulty[iss.Query] = true
	}
	return 100 * float64(len(cases)-len(faulty)) / float64(len(cases))
}

func newEvalRAGDiversityCmd() *cobra.Command {
	var dsn, provider, file string
	var failUnder float64
	cmd := &cobra.Command{
		Use:   "rag-diversity",
		Short: "Distinct concept-type coverage of RAG citations (rag.Ask)",
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
			dir, err := os.Getwd()
			if err != nil {
				return err
			}
			ag, err := agents.NewAgency(provider, dir)
			if err != nil {
				return err
			}
			defer func() { _ = ag.Close() }()

			cases, err := evalkit.LoadRAGDiversityCases(file)
			if err != nil {
				return err
			}
			results, err := evalkit.RunRAGDiversity(ctx,
				rag.HybridRetriever{Store: st, Emb: emb},
				rag.BundleSource{Dir: cfg.BundleDir},
				rag.AgentGenerator{Agency: ag},
				cases,
			)
			if err != nil {
				return err
			}
			passRate := printRAGDiversityReport(cmd.OutOrStdout(), results)
			return checkFailUnder("rag-diversity", passRate, failUnder)
		},
	}
	cmd.Flags().StringVar(&dsn, "dsn", "", "Postgres DSN")
	cmd.Flags().StringVar(&provider, "provider", "claude", "answerer backend: claude|codex|agy")
	cmd.Flags().StringVar(&file, "file", "eval/cases-rag-diversity.tsv", "RAG diversity case file (id<TAB>question<TAB>min-types)")
	cmd.Flags().Float64Var(&failUnder, "fail-under", 0, "fail if the min-types pass rate drops below this percentage (0-100); 0 or unset = never fail")
	return cmd
}

// printRAGDiversityReport writes the existing per-case and aggregate lines,
// byte-for-byte as before, and additionally returns the min-types pass rate
// (0-100 scale) for --fail-under gating. A return of -1 means no cases were
// run, so --fail-under must never trigger.
func printRAGDiversityReport(w io.Writer, results []evalkit.DiversityResult) float64 {
	below := 0
	for _, r := range results {
		status := "ok"
		if len(r.Types) < r.MinTypes {
			status = "BELOW MIN"
			below++
		}
		_, _ = fmt.Fprintf(w, "%-9s  %-40.40s  types=%v (want>=%d)\n", status, r.ID, r.Types, r.MinTypes)
	}
	_, _ = fmt.Fprintln(w, "----")
	_, _ = fmt.Fprintf(w, "cases=%d  below-min=%d\n", len(results), below)
	if len(results) == 0 {
		return -1
	}
	return 100 * float64(len(results)-below) / float64(len(results))
}

func newEvalAsOfCmd() *cobra.Command {
	var dsn, file string
	var failUnder float64
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
			passRate := printAsOfReport(cmd.OutOrStdout(), cases, s.LatestEpoch, issues)
			return checkFailUnder("asof", passRate, failUnder)
		},
	}
	cmd.Flags().StringVar(&dsn, "dsn", "", "Postgres DSN")
	cmd.Flags().StringVar(&file, "file", "eval/cases-precise-ids.tsv", "queries to check (only the query column is used)")
	cmd.Flags().Float64Var(&failUnder, "fail-under", 0, "fail if the as-of invariant pass rate drops below this percentage (0-100); 0 or unset = never fail")
	return cmd
}

// printAsOfReport writes the existing per-issue and aggregate lines,
// byte-for-byte as before, and additionally returns the as-of invariant pass
// rate (0-100 scale) for --fail-under gating. RunAsOfInvariant contributes at
// most one issue per case, so len(issues) is already a case count. A return
// of -1 means no cases were run, so --fail-under must never trigger.
func printAsOfReport(w io.Writer, cases []evalkit.PairCase, latestEpoch int, issues []evalkit.AsOfIssue) float64 {
	for _, iss := range issues {
		_, _ = fmt.Fprintf(w, "ISSUE  %-50.50s  %s\n", iss.Query, iss.Detail)
	}
	_, _ = fmt.Fprintln(w, "----")
	_, _ = fmt.Fprintf(w, "cases=%d  latest-epoch=%d  issues=%d\n", len(cases), latestEpoch, len(issues))
	if len(cases) == 0 {
		return -1
	}
	return 100 * float64(len(cases)-len(issues)) / float64(len(cases))
}
