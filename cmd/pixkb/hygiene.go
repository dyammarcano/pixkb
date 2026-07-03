package main

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"

	"pixkb/internal/hygiene"
	"pixkb/internal/okf"
)

// newHygieneCmd is the deterministic KB health report: it scans the canonical
// bundle for BACEN-charter deviations and mechanical issues. No DB, no LLM, no
// network — air-gap-pure, and the trigger the curate loop builds on.
func newHygieneCmd() *cobra.Command {
	var asJSON bool
	var checkFilter, sevFilter string
	var errorsOnly bool
	cmd := &cobra.Command{
		Use:   "hygiene",
		Short: "Scan the KB for BACEN-charter deviations and mechanical issues (deterministic, offline)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg := loadConfig()
			concepts, err := okf.ReadBundle(cfg.BundleDir)
			if err != nil {
				return fmt.Errorf("read bundle %q: %w", cfg.BundleDir, err)
			}
			rep := hygiene.Scan(concepts)
			w := cmd.OutOrStdout()

			var findings []hygiene.Finding
			errs, warns := 0, 0
			for _, f := range rep.Findings {
				if checkFilter != "" && string(f.Check) != checkFilter {
					continue
				}
				if sevFilter != "" && string(f.Severity) != sevFilter {
					continue
				}
				if errorsOnly && f.Severity != hygiene.SeverityError {
					continue
				}
				if f.Severity == hygiene.SeverityError {
					errs++
				} else {
					warns++
				}
				findings = append(findings, f)
			}

			if asJSON {
				enc := json.NewEncoder(w)
				enc.SetIndent("", "  ")
				return enc.Encode(map[string]any{"concepts": rep.Concepts, "errors": errs, "warnings": warns, "findings": findings})
			}

			_, _ = fmt.Fprintf(w, "scanned %d concepts: %d errors, %d warnings\n", rep.Concepts, errs, warns)
			for _, f := range findings {
				_, _ = fmt.Fprintf(w, "  [%-5s] %-12s %s\n            %s\n", f.Severity, f.Check, f.ConceptID, f.Detail)
			}
			if errs > 0 {
				return fmt.Errorf("%d error-severity findings", errs)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "emit findings as JSON")
	cmd.Flags().StringVar(&checkFilter, "check", "", "filter by check name")
	cmd.Flags().StringVar(&sevFilter, "severity", "", "filter by severity (error|warn)")
	cmd.Flags().BoolVar(&errorsOnly, "errors", false, "show only error-severity findings")
	return cmd
}
