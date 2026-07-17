package main

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"pixkb/internal/hql"
	"pixkb/internal/okf"
	"pixkb/internal/output"
	"pixkb/internal/store/postgres"
)

// newQueryCmd builds the "pixkb query" command: a parameterized boolean/field
// filter over the concept store, compiled from an HQL expression (see
// internal/hql). Parsing and SQL compilation happen before the store is
// opened, so a malformed query fails fast without needing a DSN.
func newQueryCmd() *cobra.Command {
	var format string
	var limit int
	cmd := &cobra.Command{
		Use:   "query <hql>",
		Short: "Structured HQL filter over the concept store (e.g. \"type = LegalArticle AND domain = tax\")",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			q, err := hql.Parse(args[0])
			if err != nil {
				return fmt.Errorf("parse query: %w", err)
			}
			where, qargs, order, err := q.ToSQL(hql.EvalContext{Now: time.Now()})
			if err != nil {
				return fmt.Errorf("compile query: %w", err)
			}
			lim := q.Limit
			if limit > 0 {
				lim = limit // --limit overrides an in-query LIMIT
			}

			cfg := loadConfig()
			ctx := cmd.Context()
			st, err := openStore(ctx, cfg)
			if err != nil {
				return err
			}
			defer st.Close()

			concepts, err := st.QueryConcepts(ctx, where, qargs, order, lim)
			if err != nil {
				return err
			}
			rendered, err := output.Render(format, conceptsToHits(concepts))
			if err != nil {
				return err
			}
			_, _ = fmt.Fprint(cmd.OutOrStdout(), rendered)
			return nil
		},
	}
	cmd.Flags().StringVar(&format, "format", "text", "output format: text|json|md|yaml")
	cmd.Flags().IntVar(&limit, "limit", 0, "max results (overrides an in-query LIMIT; 0 = query's own/none)")
	return cmd
}

// conceptsToHits adapts QueryConcepts' []okf.Concept result into the
// []postgres.Hit shape output.Render expects, mirroring how search renders
// its results. Query has no ranking score, so Rank is the 1-based result
// position and Score is left at zero.
func conceptsToHits(concepts []okf.Concept) []postgres.Hit {
	hits := make([]postgres.Hit, len(concepts))
	for i, c := range concepts {
		hits[i] = postgres.Hit{
			ID:    c.ID,
			Title: c.Title,
			Type:  c.Type,
			Rank:  i + 1,
		}
	}
	return hits
}
