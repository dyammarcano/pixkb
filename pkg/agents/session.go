package agents

import (
	"context"
	"sync"
)

// Session is a warm, long-running agent backend. Reusing a Session across turns
// avoids the cold-start + model-warmup cost of spawning a coding-agent CLI per
// call — the process stays alive between Sends.
type Session interface {
	// Send runs one turn on the warm session.
	Send(ctx context.Context, req RunRequest) (RunResult, error)
	// Alive reports whether the underlying process is still usable.
	Alive() bool
	// Close terminates the session and frees its process.
	Close() error
}

// SessionOpener is a Provider that can open warm sessions. Providers that
// implement it are driven through the SessionPool (long-running); providers
// that do not fall back to one-shot Run.
type SessionOpener interface {
	Open(ctx context.Context, agent Agent) (Session, error)
}

// SessionPool keeps one warm Session per agent, reusing it across turns and
// transparently reopening it when it dies. Safe for concurrent use.
type SessionPool struct {
	opener SessionOpener
	mu     sync.Mutex
	live   map[string]Session
}

// NewSessionPool builds a pool over an opener.
func NewSessionPool(o SessionOpener) *SessionPool {
	return &SessionPool{opener: o, live: make(map[string]Session)}
}

// get returns a warm session for the agent, opening one (or replacing a dead
// one) as needed.
func (p *SessionPool) get(ctx context.Context, agent Agent) (Session, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if s, ok := p.live[agent.Name]; ok {
		if s.Alive() {
			return s, nil
		}
		_ = s.Close()
		delete(p.live, agent.Name)
	}
	s, err := p.opener.Open(ctx, agent)
	if err != nil {
		return nil, err
	}
	p.live[agent.Name] = s
	return s, nil
}

// Run executes a turn on a warm session, reopening on failure so a dead process
// never wedges the pool.
func (p *SessionPool) Run(ctx context.Context, req RunRequest) (RunResult, error) {
	s, err := p.get(ctx, req.Agent)
	if err != nil {
		return RunResult{}, err
	}
	res, err := s.Send(ctx, req)
	if err != nil {
		p.drop(req.Agent.Name, s)
	}
	return res, err
}

// drop removes a specific session instance from the pool (only if it is still
// the live one) and closes it.
func (p *SessionPool) drop(name string, s Session) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if cur, ok := p.live[name]; ok && cur == s {
		_ = cur.Close()
		delete(p.live, name)
	}
}

// Close tears down every warm session.
func (p *SessionPool) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	var firstErr error
	for k, s := range p.live {
		if e := s.Close(); e != nil && firstErr == nil {
			firstErr = e
		}
		delete(p.live, k)
	}
	return firstErr
}

// oneShotSession adapts a one-shot Provider to the Session interface: each Send
// re-execs the provider. It does NOT stay warm — it exists so the pool presents
// a uniform interface for providers without a persistent mode (Codex exec).
// Providers with a real persistent mode return their own warm Session instead.
type oneShotSession struct {
	p Provider
}

func (s oneShotSession) Send(ctx context.Context, req RunRequest) (RunResult, error) {
	return s.p.Run(ctx, req)
}
func (s oneShotSession) Alive() bool  { return true }
func (s oneShotSession) Close() error { return nil }

// OneShot wraps a Provider as a non-warm Session, for provider packages whose
// backend has no persistent mode (e.g. Antigravity off Windows).
func OneShot(p Provider) Session { return oneShotSession{p: p} }
