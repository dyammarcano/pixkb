package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"pixkb/internal/hql"
	"pixkb/internal/ingest"
	"pixkb/internal/okf"
	"pixkb/internal/output"
	"pixkb/internal/query"
	"pixkb/internal/similar"
	"pixkb/internal/store/postgres"
)

// attachCommands wires the knowledge-base subcommands onto root.
func attachCommands(root *cobra.Command) {
	root.AddCommand(newIngestCmd(), newSearchCmd(), newReindexCmd(), newDiffCmd(), newStatsCmd(), newRelatedCmd(), newSimilarCmd(), newAgentsCmd(), newConceptCmd(), newMCPCmd(), newHygieneCmd(), newCurateCmd(), newQRCmd(), newAskCmd(), newISPBCmd(), newEvalCmd(), newVocabCmd(), newSearchHealthCmd(), newEconIndexCmd(), newQueryCmd())
}

// knownDomains are the valid KB domain values. A configured domain outside this
// set (e.g. the typo "taxx") silently produces a domain:<typo> tag that is
// invisible to both the pix and tax domain filters, so buildSources warns.
var knownDomains = map[string]bool{"pix": true, "tax": true}

// unknownConfiguredDomains returns a human-readable label for every configured
// source whose domain is neither empty nor a known KB domain. An empty domain is
// allowed — it is backfilled to domain:pix by ingest.tagDomain — so only an
// explicit, non-empty, unrecognized domain is reported.
func unknownConfiguredDomains(cfg Config) []string {
	var bad []string
	check := func(domain, label string) {
		if domain != "" && !knownDomains[domain] {
			bad = append(bad, label+" (domain: "+domain+")")
		}
	}
	for _, spec := range cfg.OpenAPISpecs {
		check(spec.Domain, "openapi_specs:"+spec.File)
	}
	for _, l := range cfg.Legislation {
		check(l.Domain, "legislation:"+l.File)
	}
	return bad
}

// buildSources assembles the ingest sources from config. The ISO-20022 message
// set is always present; PDF and git-mirror sources are added when configured.
func buildSources(cfg Config) []ingest.Source {
	for _, b := range unknownConfiguredDomains(cfg) {
		slog.Warn("configured domain is not a known KB domain (pix|tax); its concepts will be invisible to domain filters", "source", b)
	}
	srcs := []ingest.Source{ingest.NewISOSpecSource(ingest.DefaultMsgDefs())}
	if len(cfg.PDFs) > 0 {
		srcs = append(srcs, ingest.NewPDFSource(cfg.PDFs))
	}
	if len(cfg.Markdown) > 0 {
		srcs = append(srcs, ingest.NewMarkdownSource(cfg.Markdown))
	}
	if len(cfg.Repos) > 0 {
		specs := make([]ingest.RepoSpec, 0, len(cfg.Repos))
		for _, r := range cfg.Repos {
			specs = append(specs, ingest.RepoSpec{Owner: r.Owner, Name: r.Name})
		}
		srcs = append(srcs, ingest.NewGitSource(specs, cfg.MirrorDir))
		// OpenAPI specs bundled inside the staged mirrors yield endpoint concepts.
		if oa := discoverOpenAPISpecs(cfg); len(oa) > 0 {
			srcs = append(srcs, ingest.NewOpenAPISource(oa))
		}
	}
	if len(cfg.APIDocs) > 0 {
		srcs = append(srcs, ingest.NewAPIDocSource(cfg.APIDocs))
	}
	if cfg.ScoutCrawlDir != "" {
		baseURL := cfg.ScoutCrawlBaseURL
		if baseURL == "" {
			baseURL = defaultScoutCrawlBaseURL
		}
		srcs = append(srcs, ingest.NewScoutCrawlSource(cfg.ScoutCrawlDir, baseURL))
	}
	for _, spec := range cfg.OpenAPISpecs {
		srcs = append(srcs, ingest.NewOpenAPISourceWithDomain([]string{spec.File}, spec.Domain))
	}
	for _, l := range cfg.Legislation {
		srcs = append(srcs, ingest.NewLegislationSource([]string{l.File}, l.Lei, l.Domain))
	}
	return srcs
}

// discoverOpenAPISpecs finds OpenAPI/Swagger YAML files inside the staged repo
// mirrors (common layouts: <repo>/openapi.yaml and <repo>/openapi/*.yaml).
func discoverOpenAPISpecs(cfg Config) []string {
	var files []string
	seen := map[string]bool{}
	for _, r := range cfg.Repos {
		base := filepath.Join(cfg.MirrorDir, r.Name)
		patterns := []string{
			filepath.Join(base, "openapi.yaml"),
			filepath.Join(base, "openapi.yml"),
			filepath.Join(base, "openapi", "*.yaml"),
			filepath.Join(base, "openapi", "*.yml"),
			filepath.Join(base, "openapi", "*", "*.yaml"),
		}
		for _, p := range patterns {
			matches, _ := filepath.Glob(p)
			for _, f := range matches {
				if !seen[f] {
					seen[f] = true
					files = append(files, f)
				}
			}
		}
	}
	return files
}

func newIngestCmd() *cobra.Command {
	var dsn string
	cmd := &cobra.Command{
		Use:   "ingest",
		Short: "Gather sources into the OKF bundle + index, cutting a new epoch",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg := loadConfig()
			if dsn != "" {
				cfg.DSN = dsn
			}
			ctx := cmd.Context()
			r, st, err := newRunner(ctx, cfg)
			if err != nil {
				return err
			}
			defer st.Close()

			concepts, err := ingest.GatherAll(ctx, buildSources(cfg))
			if err != nil {
				return err
			}
			concepts = ingest.CrossLink(concepts)
			res, err := r.Run(ctx, concepts, "ingest")
			if err != nil {
				return err
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "epoch %d: +%d ~%d -%d commit %s\n",
				res.Epoch, res.Added, res.Changed, res.Removed, short(res.Commit))
			return nil
		},
	}
	cmd.Flags().StringVar(&dsn, "dsn", "", "Postgres DSN (overrides PIXKB_DSN)")
	return cmd
}

func newSearchCmd() *cobra.Command {
	var dsn, mode, typ, tag, format, asOfTime, where string
	var limit, asOfEpoch int
	var explain bool
	var includeTypes, excludeIDs []string
	var minVecScore float64
	cmd := &cobra.Command{
		Use:   "search <query>",
		Short: "Search the KB (hybrid FTS+vector by default; --mode fts|vector)",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var hq hql.Query
			if where != "" {
				var err error
				hq, err = hql.Parse(where)
				if err != nil {
					return fmt.Errorf("parse --where: %w", err)
				}
				if _, _, _, err := hq.ToSQLAt(hql.EvalContext{Now: time.Now()}, 0); err != nil {
					return fmt.Errorf("compile --where: %w", err)
				}
			}

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

			q := strings.Join(args, " ")
			f := postgres.Filter{
				Type: typ, Tag: tag, Limit: limit,
				IncludeTypes: includeTypes, ExcludeIDs: excludeIDs, MinVecScore: minVecScore,
			}
			if where != "" {
				f.HQLWhere = func(start int) (string, []any, error) {
					w, a, _, err := hq.ToSQLAt(hql.EvalContext{Now: time.Now()}, start)
					return w, a, err
				}
			}

			if asOfEpoch != 0 && asOfTime != "" {
				return fmt.Errorf("set only one of --as-of-epoch or --as-of-time")
			}
			if asOfEpoch != 0 {
				f.AsOfEpoch = &asOfEpoch
			}
			if asOfTime != "" {
				t, err := time.Parse(time.RFC3339, asOfTime)
				if err != nil {
					return fmt.Errorf("bad --as-of-time %q: %w", asOfTime, err)
				}
				f.AsOfTime = &t
			}

			if explain {
				if mode == "multi" {
					mh, err := query.MultiHybrid(ctx, st, emb, q, f)
					if err != nil {
						return err
					}
					return printMultiExplain(cmd.OutOrStdout(), mh)
				}
				if mode != "" && mode != "hybrid" {
					return fmt.Errorf("--explain is only supported with --mode hybrid or --mode multi (or the default)")
				}
				hits, explains, err := query.HybridExplain(ctx, st, emb, q, f)
				if err != nil {
					return err
				}
				return printExplain(cmd.OutOrStdout(), hits, explains, q, cfg.BundleDir)
			}

			var hits []postgres.Hit
			switch mode {
			case "fts":
				hits, err = st.FTS(ctx, q, f)
			case "vector":
				var vs [][]float32
				if vs, err = emb.Embed(ctx, []string{q}); err == nil {
					hits, err = st.Vector(ctx, vs[0], f)
				}
			case "multi":
				var mh []query.MultiHit
				if mh, err = query.MultiHybrid(ctx, st, emb, q, f); err == nil {
					hits = query.Hits(mh)
				}
			default:
				hits, err = query.Hybrid(ctx, st, emb, q, f)
			}
			if err != nil {
				return err
			}
			rendered, err := output.Render(format, hits)
			if err != nil {
				return err
			}
			_, _ = fmt.Fprint(cmd.OutOrStdout(), rendered)
			return nil
		},
	}
	cmd.Flags().StringVar(&dsn, "dsn", "", "Postgres DSN")
	cmd.Flags().StringVar(&mode, "mode", "hybrid", "search mode: hybrid|fts|vector|multi")
	cmd.Flags().StringVar(&typ, "type", "", "filter by concept type")
	cmd.Flags().StringVar(&tag, "tag", "", "filter by tag")
	cmd.Flags().IntVar(&limit, "limit", 20, "max results")
	cmd.Flags().BoolVar(&explain, "explain", false, "print per-hit ranking breakdown as JSON (hybrid mode) or subquery attribution (multi mode)")
	cmd.Flags().StringVar(&format, "format", "text", "output format: text|json|md|yaml")
	cmd.Flags().IntVar(&asOfEpoch, "as-of-epoch", 0, "time-travel: return the state as of this epoch (0 = unset)")
	cmd.Flags().StringVar(&asOfTime, "as-of-time", "", "time-travel: return the state as of this RFC3339 timestamp (empty = unset)")
	cmd.Flags().StringSliceVar(&includeTypes, "include-type", nil, "restrict to concepts whose type is in this list (comma-separated or repeatable; ORs with --type when both are set)")
	cmd.Flags().StringSliceVar(&excludeIDs, "exclude-id", nil, "exclude these concept ids from results (comma-separated or repeatable)")
	cmd.Flags().Float64Var(&minVecScore, "min-vector-score", 0, "drop vector-arm hits scoring below this cosine similarity (0 = disabled)")
	cmd.Flags().StringVar(&where, "where", "", "HQL predicate to narrow results before ranking, e.g. 'type = LegalArticle AND domain = tax' (any ORDER BY/LIMIT in it is ignored — ranking uses relevance + --limit)")
	return cmd
}

// explainHit combines a hit's identity (id/title/rank) with the full per-hit
// ranking breakdown from query.HybridExplain, plus the matched-token/
// matched-field-category annotation (Feature 3's remaining 2 of 7 spec
// fields), for --explain JSON output.
type explainHit struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Rank  int    `json:"rank"`
	query.Explain
	query.MatchedFields
}

// printExplain writes hits and their parallel Explain breakdowns as an
// indented JSON array to w. hits and explains must be the same length and
// index-aligned, which query.HybridExplain guarantees. q and bundleDir are
// used only to compute the matched-token/matched-field-category annotation
// (query.ComputeMatchedFields) — a read-only, post-ranking presentation
// layer, never new ranking logic. A concept that fails to read from the
// bundle (deleted/renamed since indexing) is not fatal: that hit's
// MatchedFields is left at its zero value, matching rag.BuildGrounding's
// "a missing concept is not fatal" convention.
func printExplain(w io.Writer, hits []postgres.Hit, explains []query.Explain, q, bundleDir string) error {
	out := make([]explainHit, len(hits))
	for i, h := range hits {
		out[i] = explainHit{ID: h.ID, Title: h.Title, Rank: h.Rank, Explain: explains[i]}
		if c, err := okf.ReadConcept(filepath.Join(bundleDir, filepath.FromSlash(h.ID)), bundleDir); err == nil {
			out[i].MatchedFields = query.ComputeMatchedFields(q, c.Title, c.IntentTerms, c.Body)
		}
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}

// multiExplainHit combines a fused multi-query hit's identity (id/title/rank)
// with its subquery provenance trail — Feature 3's "subquery attribution for
// multi-query search" requirement, for --explain --mode multi JSON output.
type multiExplainHit struct {
	ID         string                `json:"id"`
	Title      string                `json:"title"`
	Rank       int                   `json:"rank"`
	Subqueries []query.SubqueryMatch `json:"subqueries"`
}

// printMultiExplain writes fused multi-query hits and their subquery
// provenance (which subquery/arm/rank found each hit) as an indented JSON
// array to w. The provenance comes straight from MultiHit.Subqueries, a field
// query.MultiHybrid already populates — no new computation needed.
func printMultiExplain(w io.Writer, mh []query.MultiHit) error {
	out := make([]multiExplainHit, len(mh))
	for i, m := range mh {
		out[i] = multiExplainHit{ID: m.ID, Title: m.Title, Rank: m.Rank, Subqueries: m.Subqueries}
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}

func newRelatedCmd() *cobra.Command {
	var dsn, format string
	cmd := &cobra.Command{
		Use:   "related <concept-id>",
		Short: "List graph neighbours of a concept (out = links to, in = linked from)",
		Args:  cobra.ExactArgs(1),
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

			rels, err := st.Related(ctx, args[0])
			if err != nil {
				return err
			}
			rendered, err := output.RenderRelated(format, rels)
			if err != nil {
				return err
			}
			_, _ = fmt.Fprint(cmd.OutOrStdout(), rendered)
			return nil
		},
	}
	cmd.Flags().StringVar(&dsn, "dsn", "", "Postgres DSN")
	cmd.Flags().StringVar(&format, "format", "text", "output format: text|json|md|yaml")
	return cmd
}

func newSimilarCmd() *cobra.Command {
	var dsn, mode, typ, tag string
	var limit int
	var includeGraph bool
	cmd := &cobra.Command{
		Use:   "similar <concept-id>",
		Short: "Find concepts similar to a known concept (semantic, graph, hybrid, or more-like-this)",
		Args:  cobra.ExactArgs(1),
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

			opts := similar.Options{
				Mode:         mode,
				IncludeGraph: includeGraph,
				Filter:       postgres.Filter{Type: typ, Tag: tag, Limit: limit},
			}
			hits, err := similar.Similar(ctx, st, emb, cfg.BundleDir, args[0], opts)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			for _, h := range hits {
				_, _ = fmt.Fprintf(out, "%2d  %-34s  %-14s  %v\n", h.Rank, h.ID, h.Type, h.Why)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&dsn, "dsn", "", "Postgres DSN")
	cmd.Flags().StringVar(&mode, "mode", "hybrid", "similarity mode: hybrid|semantic|graph|more-like-this")
	cmd.Flags().StringVar(&typ, "type", "", "filter results by concept type")
	cmd.Flags().StringVar(&tag, "tag", "", "filter results by tag")
	cmd.Flags().IntVar(&limit, "limit", 20, "max results")
	cmd.Flags().BoolVar(&includeGraph, "include-graph", true, "hybrid mode: also fold in direct graph neighbours")
	return cmd
}

func newStatsCmd() *cobra.Command {
	var dsn, format string
	cmd := &cobra.Command{
		Use:   "stats",
		Short: "Show KB size and health (concepts, embeddings, epochs)",
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

			s, err := st.Stats(ctx)
			if err != nil {
				return err
			}
			rendered, err := output.RenderStats(format, s)
			if err != nil {
				return err
			}
			_, _ = fmt.Fprint(cmd.OutOrStdout(), rendered)
			return nil
		},
	}
	cmd.Flags().StringVar(&dsn, "dsn", "", "Postgres DSN")
	cmd.Flags().StringVar(&format, "format", "text", "output format: text|json|md|yaml")
	return cmd
}

func newReindexCmd() *cobra.Command {
	var dsn string
	cmd := &cobra.Command{
		Use:   "reindex",
		Short: "Rebuild the Postgres index from the canonical OKF bundle",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg := loadConfig()
			if dsn != "" {
				cfg.DSN = dsn
			}
			ctx := cmd.Context()
			r, st, err := newRunner(ctx, cfg)
			if err != nil {
				return err
			}
			defer st.Close()
			if err := r.Reindex(ctx); err != nil {
				return err
			}
			_, _ = fmt.Fprintln(cmd.OutOrStdout(), "reindex complete")
			return nil
		},
	}
	cmd.Flags().StringVar(&dsn, "dsn", "", "Postgres DSN")
	return cmd
}

func newDiffCmd() *cobra.Command {
	var dsn string
	cmd := &cobra.Command{
		Use:   "diff <n> <m>",
		Short: "Concept-level diff between two epochs",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			n, err := strconv.Atoi(args[0])
			if err != nil {
				return fmt.Errorf("bad epoch %q: %w", args[0], err)
			}
			m, err := strconv.Atoi(args[1])
			if err != nil {
				return fmt.Errorf("bad epoch %q: %w", args[1], err)
			}
			cfg := loadConfig()
			if dsn != "" {
				cfg.DSN = dsn
			}
			ctx := cmd.Context()
			r, st, err := newRunner(ctx, cfg)
			if err != nil {
				return err
			}
			defer st.Close()
			d, err := r.Diff(ctx, n, m)
			if err != nil {
				return err
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "added:   %v\nchanged: %v\nremoved: %v\n", d.Added, d.Changed, d.Removed)
			return nil
		},
	}
	cmd.Flags().StringVar(&dsn, "dsn", "", "Postgres DSN")
	return cmd
}

func short(sha string) string {
	if len(sha) >= 7 {
		return sha[:7]
	}
	return sha
}
