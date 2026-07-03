package rag

import (
	"context"
	"strings"
	"testing"

	"pixkb/internal/okf"
)

type fakeRetriever struct {
	hits    []Hit
	related map[string][]string
	err     error
}

func (f *fakeRetriever) Retrieve(_ context.Context, _ string, _ int) ([]Hit, error) {
	return f.hits, f.err
}
func (f *fakeRetriever) Related(_ context.Context, id string) ([]string, error) {
	return f.related[id], nil
}

type fakeSource map[string]okf.Concept

func (f fakeSource) Concept(_ context.Context, id string) (okf.Concept, error) {
	c, ok := f[id]
	if !ok {
		return okf.Concept{}, context.Canceled // any error; BuildGrounding skips it
	}
	return c, nil
}

func concept(id, title, body, src string) okf.Concept {
	return okf.Concept{ID: id, Title: title, Body: body, SourceURI: src}
}

func TestBuildGrounding_RanksAndTags(t *testing.T) {
	r := &fakeRetriever{hits: []Hit{{ID: "a.md", Title: "A", Score: 2}, {ID: "b.md", Title: "B", Score: 1}}}
	cs := fakeSource{
		"a.md": concept("a.md", "A", "body of A", "doc:a"),
		"b.md": concept("b.md", "B", "body of B", "doc:b"),
	}
	g, err := BuildGrounding(context.Background(), r, cs, "q", Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(g.Chunks) != 2 || g.Chunks[0].ID != "a.md" || g.Chunks[1].ID != "b.md" {
		t.Fatalf("chunks = %+v", g.Chunks)
	}
	out := g.Render()
	for _, want := range []string{"concept: a.md", "source: doc:a", "body of A", "concept: b.md"} {
		if !strings.Contains(out, want) {
			t.Fatalf("render missing %q in:\n%s", want, out)
		}
	}
}

func TestBuildGrounding_EmptyOnNoHits(t *testing.T) {
	r := &fakeRetriever{hits: nil}
	g, err := BuildGrounding(context.Background(), r, fakeSource{}, "weather tomorrow", Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(g.Chunks) != 0 || g.Render() != "" {
		t.Fatalf("OOD must yield empty grounding, got %+v", g)
	}
}

func TestBuildGrounding_BudgetKeepsFirst(t *testing.T) {
	big := strings.Repeat("x", 500)
	r := &fakeRetriever{hits: []Hit{{ID: "a.md"}, {ID: "b.md"}, {ID: "c.md"}}}
	cs := fakeSource{
		"a.md": concept("a.md", "A", big, "doc:a"),
		"b.md": concept("b.md", "B", big, "doc:b"),
		"c.md": concept("c.md", "C", big, "doc:c"),
	}
	g, err := BuildGrounding(context.Background(), r, cs, "q", Options{MaxChars: 600})
	if err != nil {
		t.Fatal(err)
	}
	// First always included; the 600-char budget admits only one more 500-char body.
	if len(g.Chunks) != 1 {
		t.Fatalf("budget should admit only the first chunk, got %d", len(g.Chunks))
	}
}

func TestBuildGrounding_SkipsMissingAndEmpty(t *testing.T) {
	r := &fakeRetriever{hits: []Hit{{ID: "gone.md"}, {ID: "empty.md"}, {ID: "ok.md"}}}
	cs := fakeSource{
		"empty.md": concept("empty.md", "E", "   ", "doc:e"),
		"ok.md":    concept("ok.md", "OK", "real body", "doc:ok"),
	}
	g, err := BuildGrounding(context.Background(), r, cs, "q", Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(g.Chunks) != 1 || g.Chunks[0].ID != "ok.md" {
		t.Fatalf("should skip missing + empty, got %+v", g.Chunks)
	}
}

func TestBuildGrounding_ExpandRelated(t *testing.T) {
	r := &fakeRetriever{
		hits:    []Hit{{ID: "a.md"}},
		related: map[string][]string{"a.md": {"n1.md", "a.md"}}, // includes a dup of a.md
	}
	cs := fakeSource{
		"a.md":  concept("a.md", "A", "body A", "doc:a"),
		"n1.md": concept("n1.md", "N1", "body N1", "doc:n1"),
	}
	g, err := BuildGrounding(context.Background(), r, cs, "q", Options{ExpandRelated: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(g.Chunks) != 2 || g.Chunks[1].ID != "n1.md" {
		t.Fatalf("expand should append the neighbour once, got %+v", g.Chunks)
	}
}
