package rag

import (
	"context"
	"strings"
	"testing"
)

type fakeGen struct {
	reply  string
	called bool
}

func (f *fakeGen) Generate(_ context.Context, _ string) (string, error) {
	f.called = true
	return f.reply, nil
}

func groundingWith(ids ...string) Grounding {
	g := Grounding{Query: "q"}
	for _, id := range ids {
		g.Chunks = append(g.Chunks, Chunk{ID: id, Title: id, Body: "body " + id, SourceURI: "doc:" + id})
	}
	return g
}

func TestAnswer_RefusesEmptyWithoutAgent(t *testing.T) {
	gen := &fakeGen{reply: `{"answer":"x","citations":["a.md"],"refused":false}`}
	a, err := Synthesize(context.Background(), gen, Grounding{Query: "weather"})
	if err != nil {
		t.Fatal(err)
	}
	if !a.Refused || gen.called {
		t.Fatalf("empty grounding must refuse WITHOUT calling the agent: refused=%v called=%v", a.Refused, gen.called)
	}
}

func TestAnswer_ParsesAndValidatesCitations(t *testing.T) {
	// Agent cites one real id and one hallucinated id; the fake should be dropped.
	gen := &fakeGen{reply: "```json\n{\"answer\":\"A devolução usa PUT\",\"citations\":[\"a.md\",\"ghost.md\"],\"refused\":false}\n```"}
	a, err := Synthesize(context.Background(), gen, groundingWith("a.md", "b.md"))
	if err != nil {
		t.Fatal(err)
	}
	if a.Refused {
		t.Fatal("should not refuse with a valid citation")
	}
	if len(a.Citations) != 1 || a.Citations[0] != "a.md" {
		t.Fatalf("citations should keep only the real id, got %v", a.Citations)
	}
	if !strings.Contains(a.Text, "devolução") {
		t.Fatalf("answer text lost: %q", a.Text)
	}
}

func TestAnswer_DowngradesUncitedToRefusal(t *testing.T) {
	// Non-refused answer but every cited id is hallucinated -> forced refusal.
	gen := &fakeGen{reply: `{"answer":"algo","citations":["ghost.md"],"refused":false}`}
	a, err := Synthesize(context.Background(), gen, groundingWith("a.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !a.Refused || len(a.Citations) != 0 {
		t.Fatalf("uncited answer must be downgraded to refusal, got %+v", a)
	}
}

func TestAnswer_RefusesCitedButEmptyText(t *testing.T) {
	// Cited a real id but produced no prose -> refusal, not a blank answer.
	gen := &fakeGen{reply: `{"answer":"   ","citations":["a.md"],"refused":false}`}
	a, err := Synthesize(context.Background(), gen, groundingWith("a.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !a.Refused || len(a.Citations) != 0 || strings.TrimSpace(a.Text) == "" {
		t.Fatalf("empty-prose answer must become a refusal with a note, got %+v", a)
	}
}

func TestAnswer_RespectsAgentRefusal(t *testing.T) {
	gen := &fakeGen{reply: `{"answer":"não consta","citations":[],"refused":true}`}
	a, err := Synthesize(context.Background(), gen, groundingWith("a.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !a.Refused {
		t.Fatal("agent refusal must be honored")
	}
}
