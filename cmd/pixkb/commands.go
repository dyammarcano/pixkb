package main

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"pixkb/internal/ingest"
	"pixkb/internal/query"
	"pixkb/internal/store/postgres"
)

// attachCommands wires the knowledge-base subcommands onto root.
func attachCommands(root *cobra.Command) {
	root.AddCommand(newIngestCmd(), newSearchCmd(), newReindexCmd(), newDiffCmd(), newStatsCmd(), newRelatedCmd(), newAgentsCmd(), newConceptCmd(), newMCPCmd(), newHygieneCmd(), newCurateCmd(), newQRCmd(), newAskCmd(), newISPBCmd())
}

// buildSources assembles the ingest sources from config. The ISO-20022 message
// set is always present; PDF and git-mirror sources are added when configured.
func buildSources(cfg Config) []ingest.Source {
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
		srcs = append(srcs, ingest.NewScoutCrawlSource(cfg.ScoutCrawlDir, "https://www.bcb.gov.br"))
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
	var dsn, mode, typ, tag string
	var limit int
	cmd := &cobra.Command{
		Use:   "search <query>",
		Short: "Search the KB (hybrid FTS+vector by default; --mode fts|vector)",
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

			q := strings.Join(args, " ")
			f := postgres.Filter{Type: typ, Tag: tag, Limit: limit}
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
			for _, h := range hits {
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "%2d  %-34s  %s\n", h.Rank, h.ID, h.Title)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&dsn, "dsn", "", "Postgres DSN")
	cmd.Flags().StringVar(&mode, "mode", "hybrid", "search mode: hybrid|fts|vector|multi")
	cmd.Flags().StringVar(&typ, "type", "", "filter by concept type")
	cmd.Flags().StringVar(&tag, "tag", "", "filter by tag")
	cmd.Flags().IntVar(&limit, "limit", 20, "max results")
	return cmd
}

func newRelatedCmd() *cobra.Command {
	var dsn string
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
			out := cmd.OutOrStdout()
			for _, r := range rels {
				_, _ = fmt.Fprintf(out, "%-3s %-34s  %s\n", r.Direction, r.ID, r.Title)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&dsn, "dsn", "", "Postgres DSN")
	return cmd
}

func newStatsCmd() *cobra.Command {
	var dsn string
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
			out := cmd.OutOrStdout()
			_, _ = fmt.Fprintf(out, "concepts:    %d\n", s.Concepts)
			_, _ = fmt.Fprintf(out, "embeddings:  %d\n", s.Embeddings)
			_, _ = fmt.Fprintf(out, "epochs:      %d (latest: %d)\n", s.Epochs, s.LatestEpoch)
			if len(s.TypeOrder) > 0 {
				_, _ = fmt.Fprintln(out, "by type:")
				for _, typ := range s.TypeOrder {
					_, _ = fmt.Fprintf(out, "  %-16s %d\n", typ, s.ByType[typ])
				}
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&dsn, "dsn", "", "Postgres DSN")
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
