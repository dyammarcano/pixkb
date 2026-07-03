package main

import (
	"github.com/spf13/cobra"

	"pixkb/internal/build"
)

// NewRootCmd builds the top-level pixkb command. This is the canonical,
// exported root constructor. Subcommands are attached when its full form lands
// in Phase 6 (Task "root command + search command wiring"): ingest, watch,
// epoch, reindex, search, diff, export-bundle, db, serve, doctor. That later
// task REPLACES this file's body in place — there is never a second (unexported)
// root constructor.
func NewRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:           "pixkb",
		Short:         "Air-gap OKF knowledge base for Pix / SPB",
		Version:       build.Version(),
		SilenceErrors: true,
		SilenceUsage:  true,
	}
	root.AddCommand(newDBCmd())
	attachCommands(root)
	attachOps(root)
	return root
}
