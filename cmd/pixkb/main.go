package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	// Cancel the command context on SIGINT/SIGTERM so long-running commands
	// (mcp serve, watch, the read-only search server) unwind and run their
	// deferred cleanup — closing the pgx pool and the agent Agency — instead of
	// being killed mid-flight. Commands read this via cmd.Context().
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := NewRootCmd().ExecuteContext(ctx); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "pixkb:", err)
		os.Exit(1)
	}
}
