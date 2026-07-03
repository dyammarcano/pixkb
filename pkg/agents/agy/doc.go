// Package agy is the Google Antigravity provider for the pixkb agent host.
//
// Antigravity's CLI (`agy`) has no working headless mode — `agy --print` hangs
// or no-ops in a non-TTY (antigravity-cli issue #318) — so this package drives
// it through a pseudo-console (ConPTY on Windows): the Driver allocates a PTY,
// runs the prompt, and captures the rendered answer stripped of ANSI escapes.
// It also offers a warm, long-running ConPTY Session so the model is warmed up
// once instead of per turn.
//
// The Driver satisfies pkg/agents.Provider and registers itself ("agy",
// "antigravity") with the core host in init(); import pkg/agents/all (or this
// package) to make it available via agents.ProviderByName.
//
// Usage monitoring (agents.UsageReporter) is implemented as the real quota call
// the CLI makes: POST cloudcode-pa.googleapis.com/v1internal:retrieveUserQuotaSummary
// (Google Code Assist API — the gemini-cli backend) with the Google OAuth bearer
// from the shared ~/.gemini/oauth_creds.json. See usage.go. The response field
// mapping was recovered from the binary (not a live response), so it is decoded
// tolerantly and failures are non-blocking. See docs/agents-usage-signals.md.
//
// Provenance: ConPTY driver adapted 2026-06-23 from the maildrop project
// (same author); doc convention from inovacc/lensr/pkg/aihost. See
// pkg/agents/README.md.
package agy
