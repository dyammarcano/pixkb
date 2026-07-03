package agents

import (
	"context"
	"fmt"
)

// Agency is the agent host: it pairs a Provider with the registered agent
// roster and runs agents by name. It is the single entry point the rest of
// pixkb uses to invoke agents (gather, scraper, normalization, quality,
// governance, research, judge, control).
//
// When the provider supports warm sessions (implements SessionOpener), the
// Agency keeps a long-running session per agent via a SessionPool so repeated
// turns skip the coding-agent CLI's cold-start + model-warmup cost. Callers
// MUST Close the Agency to tear those sessions down.
type Agency struct {
	Provider Provider
	Dir      string // repo root the agents operate in
	pool     *SessionPool

	// LimitThreshold is the used-percent (0..100) at or above which RunAgent
	// withholds a turn and returns ErrRateLimited, so a long-running fleet pauses
	// before hitting a hard subscription wall instead of failing mid-turn. 0
	// disables the gate. Only enforced when the provider implements UsageReporter.
	LimitThreshold float64
}

// DefaultLimitThreshold is the used-percent at which the Agency stops
// dispatching: pause with a little headroom rather than at a dead 100%.
const DefaultLimitThreshold = 98

// NewAgency builds an Agency over the named provider backend (codex|code|agy)
// rooted at dir, enabling warm sessions when the provider supports them.
func NewAgency(provider, dir string) (*Agency, error) {
	p, err := ProviderByName(provider)
	if err != nil {
		return nil, err
	}
	a := &Agency{Provider: p, Dir: dir, LimitThreshold: DefaultLimitThreshold}
	if o, ok := p.(SessionOpener); ok {
		a.pool = NewSessionPool(o)
	}
	return a, nil
}

// LimitStatus reports the provider's current rate-limit snapshot, or
// (nil, false, nil) when the provider exposes no queryable limit. Callers (the
// control loop, the MCP server) use it to monitor the session window while the
// fleet runs.
func (a *Agency) LimitStatus() (status *LimitStatus, supported bool, err error) {
	return ProviderUsage(a.Provider)
}

// Run looks up the named agent and executes one turn with the given input,
// reusing a warm session when available.
func (a *Agency) Run(ctx context.Context, agentName, input string) (RunResult, error) {
	ag, ok := ByName(agentName)
	if !ok {
		return RunResult{}, fmt.Errorf("agency: unknown agent %q", agentName)
	}
	return a.RunAgent(ctx, ag, input)
}

// RunAgent executes a turn for an explicit Agent (not necessarily in the
// roster), reusing a warm session when available.
func (a *Agency) RunAgent(ctx context.Context, ag Agent, input string) (RunResult, error) {
	if err := checkLimit(a.Provider, a.LimitThreshold); err != nil {
		return RunResult{}, err
	}
	req := RunRequest{Agent: ag, Input: input, Dir: a.Dir, Schema: ag.Schema}
	if a.pool != nil {
		return a.pool.Run(ctx, req)
	}
	return a.Provider.Run(ctx, req)
}

// Close tears down any warm sessions. Safe to call when no pool is active.
func (a *Agency) Close() error {
	if a.pool != nil {
		return a.pool.Close()
	}
	return nil
}
