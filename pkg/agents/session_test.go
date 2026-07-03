package agents

import (
	"context"
	"errors"
	"sync"
	"testing"
)

// fakeSession is a controllable Session for pool tests.
type fakeSession struct {
	mu     sync.Mutex
	sends  int
	alive  bool
	closed int
	failOn int // Send number (1-based) that returns an error; 0 = never
}

func (f *fakeSession) Send(_ context.Context, _ RunRequest) (RunResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sends++
	if f.failOn != 0 && f.sends == f.failOn {
		return RunResult{}, errors.New("boom")
	}
	return RunResult{Text: "ok", Provider: "fake"}, nil
}
func (f *fakeSession) Alive() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.alive
}
func (f *fakeSession) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closed++
	f.alive = false
	return nil
}

// fakeOpener hands out preconfigured sessions in order and counts opens.
type fakeOpener struct {
	mu    sync.Mutex
	queue []*fakeSession
	opens int
}

func (o *fakeOpener) Open(_ context.Context, _ Agent) (Session, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.opens++
	if len(o.queue) == 0 {
		return &fakeSession{alive: true}, nil
	}
	s := o.queue[0]
	o.queue = o.queue[1:]
	return s, nil
}

func TestSessionPoolReusesWarmSession(t *testing.T) {
	s := &fakeSession{alive: true}
	o := &fakeOpener{queue: []*fakeSession{s}}
	p := NewSessionPool(o)
	ag := Agent{Name: "judge"}

	for i := range 3 {
		if _, err := p.Run(context.Background(), RunRequest{Agent: ag}); err != nil {
			t.Fatalf("run %d: %v", i, err)
		}
	}
	if o.opens != 1 {
		t.Errorf("opened %d times, want 1 (session should stay warm)", o.opens)
	}
	if s.sends != 3 {
		t.Errorf("sends = %d, want 3", s.sends)
	}
}

func TestSessionPoolReopensDeadSession(t *testing.T) {
	dead := &fakeSession{alive: false} // dies immediately after first open
	fresh := &fakeSession{alive: true}
	o := &fakeOpener{queue: []*fakeSession{dead, fresh}}
	p := NewSessionPool(o)
	ag := Agent{Name: "gather"}

	// First Run opens `dead`; it is not Alive on the next get, so reopen `fresh`.
	_, _ = p.Run(context.Background(), RunRequest{Agent: ag})
	_, _ = p.Run(context.Background(), RunRequest{Agent: ag})
	if o.opens != 2 {
		t.Errorf("opens = %d, want 2 (dead session reopened)", o.opens)
	}
	if dead.closed == 0 {
		t.Error("dead session should have been closed")
	}
}

func TestSessionPoolDropsOnSendError(t *testing.T) {
	failing := &fakeSession{alive: true, failOn: 1}
	o := &fakeOpener{queue: []*fakeSession{failing}}
	p := NewSessionPool(o)
	ag := Agent{Name: "scraper"}

	if _, err := p.Run(context.Background(), RunRequest{Agent: ag}); err == nil {
		t.Fatal("want error from failing send")
	}
	if failing.closed == 0 {
		t.Error("failed session should be dropped + closed")
	}
}

func TestSessionPoolClose(t *testing.T) {
	s := &fakeSession{alive: true}
	o := &fakeOpener{queue: []*fakeSession{s}}
	p := NewSessionPool(o)
	_, _ = p.Run(context.Background(), RunRequest{Agent: Agent{Name: "quality"}})
	if err := p.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if s.closed == 0 {
		t.Error("pool Close should close live sessions")
	}
}

func TestOneShotSession(t *testing.T) {
	var ran bool
	p := providerFunc(func(_ context.Context, _ RunRequest) (RunResult, error) {
		ran = true
		return RunResult{Text: "x"}, nil
	})
	s := oneShotSession{p: p}
	if !s.Alive() {
		t.Error("one-shot session always alive")
	}
	if _, err := s.Send(context.Background(), RunRequest{}); err != nil || !ran {
		t.Errorf("send should delegate to provider: ran=%v err=%v", ran, err)
	}
	if err := s.Close(); err != nil {
		t.Errorf("close: %v", err)
	}
}

// providerFunc adapts a func to Provider for tests.
type providerFunc func(context.Context, RunRequest) (RunResult, error)

func (f providerFunc) Name() string { return "func" }
func (f providerFunc) Run(ctx context.Context, req RunRequest) (RunResult, error) {
	return f(ctx, req)
}
