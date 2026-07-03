// Package agents is the pixkb agent host: the provider-agnostic core that runs
// a roster of declarative agents (gather, scraper, normalization, quality,
// governance, research, judge, control) through a pluggable Provider backend,
// keeping backends warm via a SessionPool.
//
// Backends are subscription coding agents, each in its own sibling package so
// the vendors stay cleanly separated:
//   - pkg/agents/agy    — Google Antigravity (ConPTY Driver)
//   - pkg/agents/codex  — OpenAI Codex (headless exec) + rate-limit usage tracking
//   - pkg/agents/claude — Anthropic Claude Code (headless print)
//
// Providers self-register here via RegisterProvider in their init(); this core
// never imports them, so the graph stays acyclic (the host-registry pattern).
// Import pkg/agents/all to populate the registry, then resolve with
// ProviderByName. pkg/agents/host packages the roster + an MCP manifest into an
// installable plugin tree per host.
//
// Documentation + provenance convention ported 2026-06-23 from
// inovacc/lensr/pkg/aihost (BSD-3): minimal interface + optional capabilities by
// type assertion, lazy-factory registry, doc.go provenance line, and a package
// README/MAINTAINERS. See pkg/agents/README.md.
package agents
