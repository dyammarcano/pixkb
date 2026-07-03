package main

import (
	"log/slog"
	"os"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spf13/cobra"

	"pixkb/internal/kbmcp"
	"pixkb/pkg/agents"
	_ "pixkb/pkg/agents/all" // registers codex/claude/agy providers
	"pixkb/pkg/agents/host"
)

// newMCPCmd runs pixkb as an MCP server: the agent's self-contained tool
// surface. When wired into a coding agent (codex/claude/agy), pixkb's verbs
// become the agent's ONLY tools — it queries, verifies, and enriches the KB
// autonomously through them.
func newMCPCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "mcp",
		Short: "Run pixkb as an MCP server (the agent's self-contained tool surface)",
	}
	cmd.AddCommand(newMCPServeCmd(), newMCPManifestCmd())
	return cmd
}

// newMCPManifestCmd prints the .mcp.json that registers `pixkb mcp serve` as an
// MCP server for a coding agent (Codex / Claude Code share this format). Drop it
// into the agent's config so that loading the agent makes pixkb's verbs its
// self-contained tool surface — no other tools, no manual wiring.
func newMCPManifestCmd() *cobra.Command {
	var bin string
	cmd := &cobra.Command{
		Use:   "manifest",
		Short: "Print the .mcp.json registering pixkb as a coding agent's MCP server",
		RunE: func(cmd *cobra.Command, _ []string) error {
			_, _ = cmd.OutOrStdout().Write(host.MCPManifest(bin))
			return nil
		},
	}
	cmd.Flags().StringVar(&bin, "bin", "pixkb", "pixkb binary path to register")
	return cmd
}

func newMCPServeCmd() *cobra.Command {
	var dsn, answerer string
	var readOnly bool
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Serve pixkb verbs (search/related/stats/concept_get/concept_upsert/reindex) as MCP tools over stdio",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg := loadConfig()
			if dsn != "" {
				cfg.DSN = dsn
			}
			ctx := cmd.Context()
			// MCP speaks JSON-RPC over stdout; all logging MUST go to stderr.
			logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))

			r, st, err := newRunner(ctx, cfg)
			if err != nil {
				return err
			}
			defer st.Close()

			d := kbmcp.Deps{Store: st, Emb: r.Emb, Runner: r, Bundle: cfg.BundleDir}
			if readOnly {
				d.Runner = nil // omit write tools
			}
			if answerer != "" {
				dir, err := os.Getwd()
				if err != nil {
					return err
				}
				ag, err := agents.NewAgency(answerer, dir)
				if err != nil {
					return err
				}
				defer func() { _ = ag.Close() }()
				d.Agency = ag // enables the kb_ask (RAG) tool
			}
			srv := kbmcp.NewServer(d)
			logger.Info("pixkb mcp server starting", "read_only", readOnly, "bundle", cfg.BundleDir, "kb_ask", answerer != "")
			return srv.Run(ctx, &mcp.StdioTransport{})
		},
	}
	cmd.Flags().StringVar(&dsn, "dsn", "", "Postgres DSN (overrides PIXKB_DSN)")
	cmd.Flags().BoolVar(&readOnly, "read-only", false, "disable write tools (concept_upsert, reindex)")
	cmd.Flags().StringVar(&answerer, "answerer", "", "enable the kb_ask RAG tool, answered by this backend: claude|codex|agy")
	return cmd
}
