package rag

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/inovacc/corral"
)

// errGen is a Generator that always fails with a fixed error.
type errGen struct{ err error }

func (g errGen) Generate(context.Context, string) (string, error) { return "", g.err }

// TestMapGenErr confirms a corral rate-limit is mapped to the typed
// ErrRateLimited (errors.Is-matchable) and other errors pass through unchanged.
func TestMapGenErr(t *testing.T) {
	if got := mapGenErr(nil); got != nil {
		t.Fatalf("nil must map to nil, got %v", got)
	}

	rl := mapGenErr(fmt.Errorf("run withheld: %w", corral.ErrRateLimited))
	if !errors.Is(rl, ErrRateLimited) {
		t.Fatalf("a corral rate-limit must map to rag.ErrRateLimited, got %v", rl)
	}
	// The corral sentinel is still wrapped for context.
	if !errors.Is(rl, corral.ErrRateLimited) {
		t.Fatalf("mapped error should still wrap the corral cause, got %v", rl)
	}

	other := errors.New("some other failure")
	if got := mapGenErr(other); !errors.Is(got, other) || errors.Is(got, ErrRateLimited) {
		t.Fatalf("an unrelated error must pass through unchanged, got %v", got)
	}
}

// TestAsk_PropagatesRateLimited confirms a rate-limit surfaces to the caller as
// ErrRateLimited (so the CLI/MCP can present a friendly "try again later").
func TestAsk_PropagatesRateLimited(t *testing.T) {
	r := &fakeRetriever{hits: []Hit{{ID: "a.md", Score: 1}}}
	cs := fakeSource{"a.md": concept("a.md", "A", "body", "doc:a")}
	gen := errGen{err: ErrRateLimited}

	_, _, err := Ask(context.Background(), r, cs, gen, "q", Options{})
	if !errors.Is(err, ErrRateLimited) {
		t.Fatalf("Ask must propagate ErrRateLimited to the caller, got %v", err)
	}
}
