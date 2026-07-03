package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"pixkb/pkg/agents"
	_ "pixkb/pkg/agents/all" // registers codex/claude/agy providers
	"pixkb/pkg/agents/codex"
	"pixkb/pkg/agents/host"
)

// newAgentsCmd exposes the agy agent host: list the roster, run health checks,
// and execute an agent through a coding-agent backend (codex|code|agy).
func newAgentsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "agents",
		Short: "Inspect and run the pixkb agent fleet (gather/scraper/normalization/quality/governance/research/judge/control)",
	}
	cmd.AddCommand(newAgentsListCmd(), newAgentsDoctorCmd(), newAgentsRunCmd(),
		newAgentsInstallCmd(), newAgentsHostsCmd(), newAgentsUsageCmd(), newAgentsUpstreamCmd())
	return cmd
}

func newAgentsUpstreamCmd() *cobra.Command {
	var check bool
	cmd := &cobra.Command{
		Use:   "upstream",
		Short: "Show the pinned openai/codex source the /status tracker derives from; --check detects drift",
		RunE: func(cmd *cobra.Command, _ []string) error {
			w := cmd.OutOrStdout()
			if !check {
				_, _ = fmt.Fprintln(w, "Pinned openai/codex sources (the /status rate-limit contract):")
				for _, r := range codex.StatusRefs {
					_, _ = fmt.Fprintf(w, "  %s@%.7s  %s\n      %s — %s\n", r.Repo, r.BlobSHA, r.Path, r.Acquired, r.Purpose)
				}
				if names, err := codex.UpstreamMirrors(); err == nil {
					_, _ = fmt.Fprintf(w, "  local mirrors: %v\n", names)
				}
				return nil
			}
			cur, err := codex.FetchCurrentSHAs(cmd.Context())
			if err != nil {
				return err
			}
			drifts := codex.CheckDrift(cur)
			changed := 0
			for _, d := range drifts {
				status := "ok"
				if d.Changed {
					status = "CHANGED"
					changed++
				}
				_, _ = fmt.Fprintf(w, "  [%-7s] %s (pinned %.7s, now %.7s)\n", status, d.Path, d.Pinned, firstN(d.Current, 7))
			}
			if changed > 0 {
				_, _ = fmt.Fprintf(w, "\n%d file(s) drifted — re-verify the session parsing in codexusage.go\n"+
					"against a fresh ~/.codex session (see upstream/SESSION-SHAPE.md).\n", changed)
				return fmt.Errorf("upstream drift: %d file(s) changed", changed)
			}
			_, _ = fmt.Fprintln(w, "\nno drift — /status rate-limit contract unchanged.")
			return nil
		},
	}
	cmd.Flags().BoolVar(&check, "check", false, "fetch current openai/codex SHAs and report drift")
	return cmd
}

func firstN(s string, n int) string {
	if s == "" {
		return "(absent)"
	}
	if len(s) < n {
		return s
	}
	return s[:n]
}

func newAgentsUsageCmd() *cobra.Command {
	var provider string
	cmd := &cobra.Command{
		Use:   "usage",
		Short: "Show a provider's subscription rate-limit usage (5h + weekly), like its /usage view",
		Long: "Calls the provider's real usage API with its own credentials, like its /usage view:\n" +
			"  claude — GET api.anthropic.com/api/oauth/usage (OAuth bearer from ~/.claude/.credentials.json)\n" +
			"  codex  — GET chatgpt.com/backend-api/wham/usage (bearer + ChatGPT-Account-Id from ~/.codex/auth.json);\n" +
			"           falls back to the last /responses rate-limit headers in ~/.codex/sessions when offline\n" +
			"Only providers that expose a queryable limit report usage.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			w := cmd.OutOrStdout()
			p, err := agents.ProviderByName(provider)
			if err != nil {
				return err
			}
			s, supported, err := agents.ProviderUsage(p)
			if err != nil {
				return err
			}
			if !supported {
				_, _ = fmt.Fprintf(w, "provider %q exposes no queryable usage limit\n", p.Name())
				return nil
			}
			if s == nil {
				_, _ = fmt.Fprintf(w, "no usage snapshot for %q (not logged in?)\n", p.Name())
				return nil
			}
			_, _ = fmt.Fprintf(w, "%s usage", p.Name())
			if s.Plan != "" {
				_, _ = fmt.Fprintf(w, " (plan: %s)", s.Plan)
			}
			_, _ = fmt.Fprintln(w)
			for _, win := range s.Windows {
				reset := "unknown"
				if !win.ResetsAt.IsZero() {
					reset = win.ResetsAt.Format("Mon 02 Jan 15:04")
				}
				_, _ = fmt.Fprintf(w, "  %-12s %3.0f%% left (used %.0f%%, resets %s)\n",
					win.Name+":", win.LeftPercent(), win.UsedPercent, reset)
			}
			if s.Source != "" {
				_, _ = fmt.Fprintf(w, "  source: %s\n", s.Source)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&provider, "provider", "codex", "provider to query (codex|claude)")
	return cmd
}

func newAgentsInstallCmd() *cobra.Command {
	var hostName, target string
	var dryRun bool
	cmd := &cobra.Command{
		Use:   "install",
		Short: "Install the pixkb agent plugin into coding-agent hosts (claude|codex|agy|all)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			hosts := host.All()
			if hostName != "" && hostName != "all" {
				h, ok := host.ByName(hostName)
				if !ok {
					return fmt.Errorf("unknown host %q (want claude|codex|agy|all)", hostName)
				}
				hosts = []host.Host{h}
			}
			w := cmd.OutOrStdout()
			for _, h := range hosts {
				res, err := host.Install(h, target, dryRun)
				if err != nil {
					return err
				}
				if dryRun {
					_, _ = fmt.Fprintf(w, "[dry-run] %s -> %s (%d files)\n", res.Host, res.Target, len(res.Planned))
					for _, p := range res.Planned {
						_, _ = fmt.Fprintf(w, "    %s\n", p)
					}
					continue
				}
				_, _ = fmt.Fprintf(w, "%s: wrote %d files to %s\n", res.Host, res.Written, res.Target)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&hostName, "host", "all", "target host: claude|codex|agy|all")
	cmd.Flags().StringVar(&target, "target", "", "override base config dir (default: user home)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "show what would be written without writing")
	return cmd
}

func newAgentsHostsCmd() *cobra.Command {
	var doctor bool
	cmd := &cobra.Command{
		Use:   "hosts",
		Short: "List installable hosts, or health-check them with --doctor",
		RunE: func(cmd *cobra.Command, _ []string) error {
			w := cmd.OutOrStdout()
			for _, h := range host.All() {
				if !doctor {
					root, _ := h.Root("")
					_, _ = fmt.Fprintf(w, "%-8s %s\n", h.Name(), root)
					continue
				}
				r := h.Doctor("")
				_, _ = fmt.Fprintf(w, "%-8s [%s] %s\n", h.Name(), r.Verdict, r.Target)
				for _, c := range r.Checks {
					_, _ = fmt.Fprintf(w, "    [%-4s] %-12s %s\n", c.Verdict, c.Name, c.Detail)
				}
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&doctor, "doctor", false, "run per-host health checks")
	return cmd
}

func newAgentsListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List the registered agents",
		RunE: func(cmd *cobra.Command, _ []string) error {
			w := cmd.OutOrStdout()
			for _, a := range agents.All() {
				_, _ = fmt.Fprintf(w, "%-14s %-13s %s\n", a.Name, a.Kind, a.Description)
			}
			return nil
		},
	}
}

func newAgentsDoctorCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Health-check the agent stack (CLIs on PATH, embedding key, roster)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			r := agents.Doctor()
			w := cmd.OutOrStdout()
			if asJSON {
				b, _ := json.MarshalIndent(r, "", "  ")
				_, _ = fmt.Fprintln(w, string(b))
				return nil
			}
			_, _ = fmt.Fprintf(w, "verdict: %s\n", r.Verdict)
			for _, c := range r.Checks {
				_, _ = fmt.Fprintf(w, "  [%-4s] %-18s %s\n", c.Verdict, c.Name, c.Detail)
			}
			if r.Verdict == "FAILED" {
				return fmt.Errorf("agent stack FAILED")
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "emit the report as JSON")
	return cmd
}

func newAgentsRunCmd() *cobra.Command {
	var provider string
	cmd := &cobra.Command{
		Use:   "run <agent> [input]",
		Short: "Run an agent through a coding-agent backend (codex|code|agy)",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir, err := os.Getwd()
			if err != nil {
				return err
			}
			ag, err := agents.NewAgency(provider, dir)
			if err != nil {
				return err
			}
			defer func() { _ = ag.Close() }()
			input := strings.Join(args[1:], " ")
			res, err := ag.Run(cmd.Context(), args[0], input)
			if err != nil {
				return err
			}
			_, _ = fmt.Fprintln(cmd.OutOrStdout(), res.Text)
			return nil
		},
	}
	cmd.Flags().StringVar(&provider, "provider", "codex", "coding-agent backend: codex|code|agy")
	return cmd
}
