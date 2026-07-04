package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"pixkb/internal/curate"
	"pixkb/pkg/agents"
	_ "pixkb/pkg/agents/all" // registers codex/claude/agy providers
)

// newCurateCmd is the Curator: the orchestrator that closes the KB control loop.
// It scans the bundle with the deterministic hygiene engine, routes each fixable
// problem to the agent that repairs it (deviation -> deviation, junk/link/dup ->
// hygiene, stub -> research), re-scans every PROPOSED concept through the SAME
// detector as a governance gate, and — only with --apply — writes the gated
// fixes back as an epoch + reindex. Dry-run is the default; --plan is offline.
func newCurateCmd() *cobra.Command {
	var provider string
	var apply, planOnly, asJSON, enrich, reenrich bool
	var limit int
	var ids []string
	cmd := &cobra.Command{
		Use:   "curate",
		Short: "Run the curation loop: scan -> route to fix agents -> gate -> (--apply) upsert+reindex",
		Long: "The Curator closes the KB control loop. It scans the canonical bundle for BACEN-charter\n" +
			"deviations and mechanical issues, hands each flagged concept to its repair agent, and re-scans\n" +
			"the proposed fix with the SAME deterministic detector (the governance gate) so an agent can\n" +
			"never write a new deviation.\n\n" +
			"  --plan   offline preview: scan + routing only, no agent turns, no database.\n" +
			"  (default) dry-run: run the fix agents and gate their output, but DO NOT write.\n" +
			"  --apply  write every gated-clean fix back as an epoch, then reindex.\n" +
			"  --enrich recall pass: the enrich agent generates intent_terms for concepts that have\n" +
			"           none; terms are merged onto the concept (body untouched) and gated for charter\n" +
			"           purity. Combine with --plan/--apply/--limit like the hygiene pass.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg := loadConfig()
			w := cmd.OutOrStdout()

			if planOnly {
				var out curate.Outcome
				var err error
				if enrich {
					out, err = curate.EnrichPlan(cfg.BundleDir, reenrich, ids)
				} else {
					out, err = curate.Plan(cfg.BundleDir, ids)
				}
				if err != nil {
					return err
				}
				return report(w, out, asJSON)
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

			fixer := &curate.AgencyFixer{Agency: ag}
			c := &curate.Curator{
				Bundle:   cfg.BundleDir,
				Fixer:    fixer,
				Enricher: fixer, // AgencyFixer satisfies both Fixer and IntentFixer
				Apply:    apply,
				Limit:    limit,
				Reenrich: reenrich,
				IDs:      ids,
			}
			if apply {
				r, st, err := newRunner(cmd.Context(), cfg)
				if err != nil {
					return err
				}
				defer st.Close()
				c.Runner = r
			}

			run := c.Run
			if enrich {
				run = c.Enrich
			}
			out, err := run(cmd.Context())
			if err != nil {
				return err
			}
			return report(w, out, asJSON)
		},
	}
	cmd.Flags().StringVar(&provider, "provider", "claude", "coding-agent backend for the fix agents: claude|codex|agy")
	cmd.Flags().BoolVar(&apply, "apply", false, "write gated-clean fixes back (default is dry-run)")
	cmd.Flags().BoolVar(&planOnly, "plan", false, "offline routing preview only (no agents, no DB)")
	cmd.Flags().BoolVar(&enrich, "enrich", false, "intent_terms recall pass: enrich agent fills concepts with no intent_terms")
	cmd.Flags().BoolVar(&reenrich, "reenrich", false, "with --enrich: route ALL concepts (re-tune existing intent_terms), not only empty ones")
	cmd.Flags().BoolVar(&asJSON, "json", false, "emit the outcome as JSON")
	cmd.Flags().IntVar(&limit, "limit", 0, "cap routed concepts this pass (0 = all); spares subscription quota")
	cmd.Flags().StringSliceVar(&ids, "ids", nil, "restrict this pass to these concept ids (comma-separated; empty = every routed/candidate concept)")
	return cmd
}

func report(w interface{ Write([]byte) (int, error) }, out curate.Outcome, asJSON bool) error {
	if asJSON {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(out)
	}
	_, _ = fmt.Fprintf(w, "scanned %d concepts: %d fixable findings, %d routed\n",
		out.Concepts, out.Findings, out.Routed)
	_, _ = fmt.Fprintf(w, "  applied=%d proposed=%d rejected=%d errors=%d\n",
		out.Applied, out.Proposed, out.Rejected, out.Errors)
	if out.Commit != "" {
		_, _ = fmt.Fprintf(w, "  commit %.7s\n", out.Commit)
	}
	for _, it := range out.Items {
		_, _ = fmt.Fprintf(w, "  [%-13s] %-9s %s %v\n", it.Status, it.Agent, it.ConceptID, it.Checks)
		if it.Detail != "" {
			_, _ = fmt.Fprintf(w, "                %s\n", it.Detail)
		}
	}
	return nil
}
