package agents

import (
	"context"
	"errors"
	"testing"
)

// stubProvider is a Provider that optionally reports usage.
type stubProvider struct {
	name   string
	status *LimitStatus
	err    error
	ran    bool
}

func (s *stubProvider) Name() string { return s.name }
func (s *stubProvider) Run(_ context.Context, _ RunRequest) (RunResult, error) {
	s.ran = true
	return RunResult{Text: "ok", Provider: s.name}, nil
}

// usageStub adds the UsageReporter capability.
type usageStub struct{ *stubProvider }

func (u usageStub) Usage() (*LimitStatus, error) { return u.status, u.err }

func TestLimitStatusWorstAndOK(t *testing.T) {
	s := &LimitStatus{Windows: []LimitWindow{{Name: "5h", UsedPercent: 40}, {Name: "weekly", UsedPercent: 96}}}
	if s.Worst() != 96 {
		t.Errorf("worst = %v, want 96", s.Worst())
	}
	if s.Exhausted() {
		t.Error("96%% is not exhausted")
	}
	if s.OK(95) {
		t.Error("96%% should fail a 95%% threshold")
	}
	if !s.OK(98) {
		t.Error("96%% should pass a 98%% threshold")
	}
	if !s.OK(0) {
		t.Error("threshold 0 disables the gate")
	}
}

func TestProviderUsageUnsupported(t *testing.T) {
	_, supported, err := ProviderUsage(&stubProvider{name: "bare"})
	if supported || err != nil {
		t.Errorf("bare provider: supported=%v err=%v", supported, err)
	}
}

func TestAgencyGatesOnLimit(t *testing.T) {
	over := usageStub{&stubProvider{name: "codex", status: &LimitStatus{
		Plan: "plus", Windows: []LimitWindow{{Name: "weekly", UsedPercent: 99}}},
	}}
	a := &Agency{Provider: over, Dir: ".", LimitThreshold: 98}
	_, err := a.RunAgent(context.Background(), Agent{Name: "probe"}, "")
	if !errors.Is(err, ErrRateLimited) {
		t.Fatalf("want ErrRateLimited, got %v", err)
	}
	if over.ran {
		t.Error("provider ran despite being over the limit")
	}
}

func TestAgencyRunsUnderLimit(t *testing.T) {
	under := usageStub{&stubProvider{name: "codex", status: &LimitStatus{
		Windows: []LimitWindow{{Name: "weekly", UsedPercent: 50}}},
	}}
	a := &Agency{Provider: under, Dir: ".", LimitThreshold: 98}
	res, err := a.RunAgent(context.Background(), Agent{Name: "probe"}, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Text != "ok" || !under.ran {
		t.Error("provider should have run under the limit")
	}
}

func TestAgencyMissingSnapshotNeverBlocks(t *testing.T) {
	// UsageReporter present but no data (nil status) must not block work.
	none := usageStub{&stubProvider{name: "codex", status: nil}}
	a := &Agency{Provider: none, Dir: ".", LimitThreshold: 98}
	if _, err := a.RunAgent(context.Background(), Agent{Name: "probe"}, ""); err != nil {
		t.Fatalf("missing snapshot must not block: %v", err)
	}
	if !none.ran {
		t.Error("provider should have run when snapshot absent")
	}
}
