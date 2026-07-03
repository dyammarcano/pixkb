// Package claude is the Anthropic Claude Code provider for the agent host: a
// headless `claude -p` preset. Claude Code has no native output-schema flag, so
// schemas are embedded in the prompt and the JSON is parsed from stdout.
//
// Usage monitoring (agents.UsageReporter) is implemented as the real call the
// CLI's `/usage` and `/status` make: GET https://api.anthropic.com/api/oauth/usage
// with the subscription OAuth bearer read from the CLI's own
// ~/.claude/.credentials.json (see usage.go). The handler is recovered from the
// published npm bundle (the GitHub repo ships no source); see
// docs/agents-usage-signals.md.
package claude

import (
	"context"
	"time"

	"pixkb/pkg/agents"
)

func init() {
	agents.RegisterProvider(func() agents.Provider { return NewProvider() }, "claude", "code", "claude-code")
}

// New returns the Claude Code provider preset (headless print mode).
func New() *agents.CLIProvider {
	return &agents.CLIProvider{
		ProviderName: "claude",
		Bin:          "claude",
		BaseArgs:     []string{"-p"},
		ModelFlag:    "--model",
		Model:        "claude-sonnet-4-6", // default; an Agent.Model hint overrides
	}
}

// Provider is the registered Claude provider: the CLIProvider preset plus the
// agents.UsageReporter capability (the real /api/oauth/usage call). Embedding
// promotes Name/Run/Open, so it is also a SessionOpener like the bare preset.
type Provider struct{ *agents.CLIProvider }

// NewProvider returns the Claude provider with subscription-usage monitoring.
func NewProvider() *Provider { return &Provider{CLIProvider: New()} }

// Usage satisfies agents.UsageReporter via the real Claude Code subscription
// endpoint. A short timeout bounds the monitor; absence (not logged in) is
// reported as (nil, nil) so it never blocks the fleet.
func (p *Provider) Usage() (*agents.LimitStatus, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	return ReadUsage(ctx)
}
