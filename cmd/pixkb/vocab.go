package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"pixkb/internal/query"
)

// newVocabCmd is the domain-vocabulary inspection surface (Feature 7 of
// docs/SEARCH-CAPABILITY-SPEC.md): lists every entry (enabled and disabled),
// its stems and subquery, and — with --reasons — the documented reason it is
// (or isn't) live. Satisfies the spec's "Users can inspect or disable domain
// expansion when debugging" acceptance criterion (the disable half is the
// PIXKB_DISABLE_DOMAIN_VOCAB env var, surfaced here for visibility).
func newVocabCmd() *cobra.Command {
	var showReasons bool
	cmd := &cobra.Command{
		Use:   "vocab",
		Short: "Inspect the domain vocabulary (Feature 7 of docs/SEARCH-CAPABILITY-SPEC.md)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			out := cmd.OutOrStdout()
			if v := os.Getenv("PIXKB_DISABLE_DOMAIN_VOCAB"); v != "" {
				fmt.Fprintf(out, "PIXKB_DISABLE_DOMAIN_VOCAB is set (%q) — domain-vocabulary expansion is currently DISABLED for multi-query search.\n\n", v)
			}
			for _, e := range query.Vocabulary() {
				status := "enabled"
				if !e.Enabled {
					status = "disabled"
				}
				fmt.Fprintf(out, "[%-8s] stems=%v -> %q\n", status, e.Stems, e.Subquery)
				if showReasons {
					fmt.Fprintf(out, "           %s\n", strings.TrimSpace(e.Reason))
				}
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&showReasons, "reasons", false, "also print each entry's documented reason")
	return cmd
}
