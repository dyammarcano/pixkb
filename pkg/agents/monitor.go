package agents

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// LimitWindow is one subscription rate-limit window a provider exposes for
// monitoring — vendor-neutral so the core can reason about Codex, Claude, or
// Antigravity limits uniformly. Mirrors what a coding agent's `/status` shows.
type LimitWindow struct {
	Name        string    // "5h" | "weekly" | provider-specific label
	UsedPercent float64   // 0..100
	ResetsAt    time.Time // when the window rolls over (zero if unknown)
}

// LeftPercent is the remaining headroom in the window.
func (w LimitWindow) LeftPercent() float64 { return 100 - w.UsedPercent }

// LimitStatus is a provider's current rate-limit snapshot across its windows.
type LimitStatus struct {
	Plan    string
	Windows []LimitWindow
	Source  string // where the snapshot came from (e.g. a session rollout file)
}

// Worst returns the highest used-percent across all windows — the binding
// constraint for deciding whether to keep dispatching work.
func (s *LimitStatus) Worst() float64 {
	var w float64
	for _, win := range s.Windows {
		if win.UsedPercent > w {
			w = win.UsedPercent
		}
	}
	return w
}

// Exhausted reports whether any window is fully spent.
func (s *LimitStatus) Exhausted() bool { return s.Worst() >= 100 }

// OK reports whether the worst window is still under threshold (a percent in
// 0..100). A threshold <= 0 disables the check (always OK).
func (s *LimitStatus) OK(threshold float64) bool {
	return threshold <= 0 || s.Worst() < threshold
}

// String renders the snapshot compactly for logs and the monitor error.
func (s *LimitStatus) String() string {
	var b strings.Builder
	if s.Plan != "" {
		fmt.Fprintf(&b, "plan=%s ", s.Plan)
	}
	for i, w := range s.Windows {
		if i > 0 {
			b.WriteString(" ")
		}
		fmt.Fprintf(&b, "%s=%.0f%%used", w.Name, w.UsedPercent)
		if !w.ResetsAt.IsZero() {
			fmt.Fprintf(&b, "(resets %s)", w.ResetsAt.Format("Mon 02 Jan 15:04"))
		}
	}
	return strings.TrimSpace(b.String())
}

// UsageReporter is an OPTIONAL Provider capability: report current subscription
// rate-limit usage so the Agency can pause a long-running fleet before it walks
// into a hard limit mid-run (rather than failing the turn). Providers without a
// queryable limit (Antigravity, Claude Code) simply do not implement it.
type UsageReporter interface {
	Usage() (*LimitStatus, error)
}

// ErrRateLimited is returned by Agency.RunAgent when the provider's usage has
// reached the configured limit threshold and the run was withheld.
var ErrRateLimited = errors.New("agents: rate-limit threshold reached")

// ProviderUsage reads a provider's limit snapshot when it supports monitoring,
// returning (nil, false, nil) when the provider has no queryable limit.
func ProviderUsage(p Provider) (status *LimitStatus, supported bool, err error) {
	r, ok := p.(UsageReporter)
	if !ok {
		return nil, false, nil
	}
	s, err := r.Usage()
	return s, true, err
}

// checkLimit is the Agency's pre-run gate: it withholds a turn when the
// provider reports usage at/over threshold. A missing snapshot or a provider
// without UsageReporter is never blocking — monitoring only ever stops work on
// a positive over-limit signal, never on absence of data.
func checkLimit(p Provider, threshold float64) error {
	if threshold <= 0 {
		return nil
	}
	s, supported, err := ProviderUsage(p)
	if !supported || err != nil || s == nil {
		return nil
	}
	if !s.OK(threshold) {
		return fmt.Errorf("%w: %s (threshold %.0f%%)", ErrRateLimited, s.String(), threshold)
	}
	return nil
}
