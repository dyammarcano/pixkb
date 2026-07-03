//go:build windows

package agy

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/UserExistsError/conpty"

	"pixkb/pkg/agents"
)

// run executes one agy prompt inside a ConPTY and returns the cleaned output.
// This is the one-shot path (a fresh process per call) used when warm sessions
// are not in play.
func (d *Driver) run(ctx context.Context, prompt string) (string, error) {
	parts := []string{
		winQuote(d.cfg.Bin), "--print", winQuote(prompt),
		"--dangerously-skip-permissions",
		"--print-timeout", fmt.Sprintf("%ds", int(d.cfg.Timeout.Seconds())),
	}
	if d.cfg.Model != "" {
		parts = append(parts, "--model", winQuote(d.cfg.Model))
	}
	cmdline := strings.Join(parts, " ")

	cpty, err := conpty.Start(cmdline)
	if err != nil {
		return "", fmt.Errorf("agy: start conpty: %w", err)
	}

	var buf bytes.Buffer
	done := make(chan struct{})
	go func() { _, _ = io.Copy(&buf, cpty); close(done) }()

	runCtx, cancel := context.WithTimeout(ctx, d.cfg.Timeout+15*time.Second)
	defer cancel()
	exit, werr := cpty.Wait(runCtx)
	// Close BEFORE draining: the ConPTY output handle does not EOF on its own
	// when agy exits, so io.Copy (and <-done) would otherwise block forever.
	_ = cpty.Close()
	<-done
	if werr != nil {
		return clean(buf.String()), fmt.Errorf("agy: wait (exit=%d): %w", exit, werr)
	}
	out := clean(buf.String())
	if out == "" {
		return "", fmt.Errorf("agy: empty response (is agy logged in?)")
	}
	return out, nil
}

// agySession is a warm, long-running agy process attached to a ConPTY. The
// model is warmed up once at Open; each Send writes a prompt into the live TUI
// and reads the response back, so repeated turns skip the cold-start cost.
//
// agy's TUI emits no turn-complete marker, so a turn is considered done when the
// rendered output stops growing for an idle window (bounded by the per-call
// timeout). This is a heuristic — fine for batch curation, not hard real-time.
type agySession struct {
	cpty    *conpty.ConPty
	buf     *syncBuffer
	mu      sync.Mutex  // serializes turns on this process
	closed  atomic.Bool // read by Alive without contending with an in-flight Send
	timeout time.Duration
}

// openSession starts agy interactive and waits for it to warm up once.
func (d *Driver) openSession(ctx context.Context) (agents.Session, error) {
	parts := []string{winQuote(d.cfg.Bin), "--dangerously-skip-permissions"}
	if d.cfg.Model != "" {
		parts = append(parts, "--model", winQuote(d.cfg.Model))
	}
	cpty, err := conpty.Start(strings.Join(parts, " "))
	if err != nil {
		return nil, fmt.Errorf("agy: start conpty session: %w", err)
	}
	s := &agySession{cpty: cpty, buf: newSyncBuffer(), timeout: d.cfg.Timeout}
	go func() { _, _ = io.Copy(s.buf, cpty) }()

	// Warm up once: wait for the TUI banner/prompt to render, then discard it.
	if !s.settle(ctx, 15*time.Second, 1500*time.Millisecond) {
		_ = s.Close()
		return nil, fmt.Errorf("agy: session never became ready (is agy logged in?)")
	}
	s.buf.Reset()
	return s, nil
}

// Send runs one turn on the warm session.
func (s *agySession) Send(ctx context.Context, req agents.RunRequest) (agents.RunResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed.Load() {
		return agents.RunResult{}, errors.New("agy: session closed")
	}
	prompt := req.ComposePrompt(true)
	offset := s.buf.Len()
	if _, err := s.cpty.Write([]byte(prompt + "\r")); err != nil {
		s.closed.Store(true)
		return agents.RunResult{}, fmt.Errorf("agy: write prompt: %w", err)
	}
	s.settle(ctx, s.timeout, 1500*time.Millisecond)
	out := clean(stripEcho(s.buf.From(offset), prompt))
	if out == "" {
		return agents.RunResult{}, errors.New("agy: empty response")
	}
	return agents.RunResult{Text: out, Provider: "agy"}, nil
}

// settle blocks until the output buffer stops growing for idle, or maxWait
// elapses, or ctx is cancelled. Returns true if any output was produced.
func (s *agySession) settle(ctx context.Context, maxWait, idle time.Duration) bool {
	deadline := time.Now().Add(maxWait)
	last := s.buf.Len()
	produced := last > 0
	stableSince := time.Now()
	for {
		if ctx != nil {
			select {
			case <-ctx.Done():
				return produced
			default:
			}
		}
		time.Sleep(100 * time.Millisecond)
		if n := s.buf.Len(); n != last {
			last, produced, stableSince = n, true, time.Now()
		} else if time.Since(stableSince) >= idle {
			return produced
		}
		if time.Now().After(deadline) {
			return produced
		}
	}
}

func (s *agySession) Alive() bool { return !s.closed.Load() }

func (s *agySession) Close() error {
	if s.closed.Swap(true) {
		return nil
	}
	return s.cpty.Close()
}
