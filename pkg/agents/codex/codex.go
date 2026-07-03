// Package codex is the OpenAI Codex provider for the agent host: a headless
// `codex exec` preset plus rate-limit usage tracking (the /status data) and
// upstream source-drift pinning for that logic.
package codex

import (
	"context"
	"time"

	"pixkb/pkg/agents"
)

func init() {
	agents.RegisterProvider(func() agents.Provider { return NewProvider() }, "codex")
}

// New returns the OpenAI Codex provider preset — the headless `codex exec`
// invocation with native --output-schema structured output.
func New() *agents.CLIProvider {
	return &agents.CLIProvider{
		ProviderName: "codex",
		Bin:          "codex",
		BaseArgs:     []string{"exec", "--dangerously-bypass-approvals-and-sandbox"},
		DirFlag:      "-C",
		SchemaFlag:   "--output-schema",
		OutputFlag:   "-o",
	}
}

// Provider is the registered Codex provider: the CLIProvider preset plus the
// agents.UsageReporter capability, so the Agency can monitor the Codex session
// limit window (the /status 5h + weekly snapshot) and pause the fleet before it
// hits a hard wall. Embedding promotes Name/Run/Open, so it is also a
// SessionOpener like the bare preset.
type Provider struct{ *agents.CLIProvider }

// NewProvider returns the Codex provider with usage monitoring wired in.
func NewProvider() *Provider { return &Provider{CLIProvider: New()} }

// Usage satisfies agents.UsageReporter via the real Codex account endpoint:
// GET {chatgpt_base_url}/wham/usage with the credentials in ~/.codex/auth.json
// (the same call the CLI's /usage and /status make). If that call fails (offline
// or not logged in), it falls back to the rate-limit headers the last /responses
// turn persisted to ~/.codex/sessions — so monitoring still has data air-gapped.
func (p *Provider) Usage() (*agents.LimitStatus, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 13*time.Second)
	defer cancel()
	if s, err := FetchUsage(ctx); err == nil && s != nil {
		return s, nil
	}
	if u, err := ReadUsage(); err == nil && u != nil {
		return u.Status(), nil
	}
	return FetchUsage(ctx)
}
