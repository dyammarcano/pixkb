package agents

import "context"

// RunRequest is a single agent turn submitted to a Provider.
type RunRequest struct {
	Agent  Agent  // the agent definition (System prompt, model hint, schema)
	Input  string // task input appended after the agent's System prompt
	Dir    string // working directory the agent operates in (repo root)
	Schema string // JSON Schema for structured output; overrides Agent.Schema when set
}

// RunResult is a Provider's reply.
type RunResult struct {
	Text     string // freeform text, or raw JSON when a schema was requested
	Provider string // provider name that produced it
}

// Provider executes an agent turn against a coding-agent backend. The backends
// are subscription coding agents (Codex, Claude Code, Antigravity) driven
// headlessly — not metered per-token APIs — so running the roster is bounded by
// the subscription, not per-call billing.
type Provider interface {
	Name() string
	Run(ctx context.Context, req RunRequest) (RunResult, error)
}

// EffectiveSchema returns the schema to enforce for a request: the per-request
// override when set, else the agent's declared schema. Exported so provider
// packages (agy/codex/claude) can drive it.
func (r RunRequest) EffectiveSchema() string {
	if r.Schema != "" {
		return r.Schema
	}
	return r.Agent.Schema
}

// ComposePrompt builds the full prompt: the agent's System role followed by the
// task Input. When the provider cannot enforce a schema natively, schemaHint is
// appended so the agent still returns valid JSON.
func (r RunRequest) ComposePrompt(schemaHint bool) string {
	p := r.Agent.System
	if r.Input != "" {
		p += "\n\n" + r.Input
	}
	if schemaHint {
		if s := r.EffectiveSchema(); s != "" {
			p += "\n\nReturn ONLY a single JSON object that validates against this JSON Schema (no prose, no code fences):\n" + s
		}
	}
	return p
}
