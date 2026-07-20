package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"pixkb/internal/link"
)

// newLinkCmd wires `pixkb link`: it scans concepts, parses BACEN normative
// citations from each body, and materializes `cites` edges to the concept
// whose norm_ref matches the citation. Default is a dry-run that prints the
// edges it would write; --apply performs the idempotent upsert. The parser
// (internal/link) is the DB-free core; only the write path touches the store.
func newLinkCmd() *cobra.Command {
	var dsn string
	var apply bool
	var limit int
	cmd := &cobra.Command{
		Use:   "link",
		Short: "Materialize BACEN citation edges (concept -> norm_ref) from concept bodies",
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

			concepts, err := st.QueryConcepts(ctx, "", nil, "id", limit)
			if err != nil {
				return err
			}

			// Index concepts by the BASE key of their norm_ref (type+number,
			// date-independent) so a citation resolves to the concept that IS
			// that normative source regardless of whether either side carries a
			// year. See link.BaseRef.
			byNormRef := make(map[string]string)
			for _, c := range concepts {
				if c.NormRef != "" {
					byNormRef[link.BaseRef(c.NormRef)] = c.ID
				}
			}

			out := cmd.OutOrStdout()
			var matched, wrote int
			for _, c := range concepts {
				for _, e := range link.ResolveEdges(c.ID, c.Body, byNormRef) {
					matched++
					if !apply {
						_, _ = fmt.Fprintf(out, "cites  %s -> %s (%s)\n", e.Src, e.Dst, e.NormRef)
						continue
					}
					inserted, err := st.UpsertEdge(ctx, e.Src, e.Dst, "cites")
					if err != nil {
						return err
					}
					if inserted {
						wrote++
					}
				}
			}

			if apply {
				_, _ = fmt.Fprintf(out, "linked %d citation edges (%d matched, %d new)\n", wrote, matched, wrote)
			} else {
				_, _ = fmt.Fprintf(out, "dry-run: %d citation edges would be written (use --apply)\n", matched)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&dsn, "dsn", "", "Postgres DSN")
	cmd.Flags().BoolVar(&apply, "apply", false, "write the edges (default: dry-run print only)")
	cmd.Flags().IntVar(&limit, "limit", 0, "max concepts to scan (0 = all)")
	return cmd
}
