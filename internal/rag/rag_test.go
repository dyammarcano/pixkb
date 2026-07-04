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

type fakeMultiRetriever struct {
	fakeRetriever
	multiHits []Hit
}

func (f *fakeMultiRetriever) RetrieveMulti(_ context.Context, _ string, _ int) ([]Hit, error) {
	return f.multiHits, nil
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

func TestBuildGrounding_MultiQueryUsesMultiRetriever(t *testing.T) {
	r := &fakeMultiRetriever{
		fakeRetriever: fakeRetriever{hits: []Hit{{ID: "single.md", Title: "Single"}}},
		multiHits:     []Hit{{ID: "multi.md", Title: "Multi"}},
	}
	cs := fakeSource{
		"single.md": concept("single.md", "Single", "body", "doc:single"),
		"multi.md":  concept("multi.md", "Multi", "body", "doc:multi"),
	}
	g, err := BuildGrounding(context.Background(), r, cs, "q", Options{MultiQuery: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(g.Chunks) != 1 || g.Chunks[0].ID != "multi.md" {
		t.Fatalf("MultiQuery should use RetrieveMulti's hits, got %+v", g.Chunks)
	}
}

func TestBuildGrounding_MultiQueryFallsBackWithoutMultiRetriever(t *testing.T) {
	r := &fakeRetriever{hits: []Hit{{ID: "a.md", Title: "A"}}}
	cs := fakeSource{"a.md": concept("a.md", "A", "body", "doc:a")}
	g, err := BuildGrounding(context.Background(), r, cs, "q", Options{MultiQuery: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(g.Chunks) != 1 || g.Chunks[0].ID != "a.md" {
		t.Fatalf("MultiQuery without a MultiRetriever must fall back to Retrieve, got %+v", g.Chunks)
	}
}

func TestBuildGrounding_DiversifyOrdersByTypeFirst(t *testing.T) {
	r := &fakeRetriever{hits: []Hit{
		{ID: "ref1.md", Type: "Reference"},
		{ID: "ref2.md", Type: "Reference"},
		{ID: "api1.md", Type: "ApiEndpoint"},
	}}
	cs := fakeSource{
		"ref1.md": concept("ref1.md", "R1", "body", "doc:r1"),
		"ref2.md": concept("ref2.md", "R2", "body", "doc:r2"),
		"api1.md": concept("api1.md", "A1", "body", "doc:a1"),
	}
	g, err := BuildGrounding(context.Background(), r, cs, "q", Options{Diversify: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(g.Chunks) != 3 {
		t.Fatalf("expected all 3 chunks, got %+v", g.Chunks)
	}
	if g.Chunks[0].ID != "ref1.md" || g.Chunks[1].ID != "api1.md" || g.Chunks[2].ID != "ref2.md" {
		t.Fatalf("diversify should promote the first ApiEndpoint ahead of the second Reference, got order %v",
			[]string{g.Chunks[0].ID, g.Chunks[1].ID, g.Chunks[2].ID})
	}
}

func TestBuildGrounding_ExpandRelatedMultiSeed(t *testing.T) {
	r := &fakeRetriever{
		hits: []Hit{{ID: "a.md"}, {ID: "b.md"}},
		related: map[string][]string{
			"a.md": {"n1.md"},
			"b.md": {"n2.md"},
		},
	}
	cs := fakeSource{
		"a.md":  concept("a.md", "A", "body A", "doc:a"),
		"b.md":  concept("b.md", "B", "body B", "doc:b"),
		"n1.md": concept("n1.md", "N1", "body N1", "doc:n1"),
		"n2.md": concept("n2.md", "N2", "body N2", "doc:n2"),
	}
	g, err := BuildGrounding(context.Background(), r, cs, "q", Options{ExpandRelated: true, ExpandSeeds: 2})
	if err != nil {
		t.Fatal(err)
	}
	ids := make([]string, len(g.Chunks))
	for i, c := range g.Chunks {
		ids[i] = c.ID
	}
	want := []string{"a.md", "b.md", "n1.md", "n2.md"}
	if len(ids) != len(want) {
		t.Fatalf("ExpandSeeds:2 should pull neighbours of both seeds, got %v", ids)
	}
	for i := range want {
		if ids[i] != want[i] {
			t.Fatalf("ExpandSeeds:2 order = %v, want %v", ids, want)
		}
	}
}

func TestBuildGrounding_MinScoreRefuses(t *testing.T) {
	r := &fakeRetriever{hits: []Hit{{ID: "weak.md", Score: 0.1}}}
	cs := fakeSource{"weak.md": concept("weak.md", "Weak", "body", "doc:weak")}
	g, err := BuildGrounding(context.Background(), r, cs, "q", Options{MinScore: 0.5})
	if err != nil {
		t.Fatal(err)
	}
	if len(g.Chunks) != 0 {
		t.Fatalf("a top hit below MinScore must refuse (empty grounding), got %+v", g.Chunks)
	}
}

func TestBuildGrounding_MinScorePassesStrongEvidence(t *testing.T) {
	r := &fakeRetriever{hits: []Hit{{ID: "strong.md", Score: 0.9}}}
	cs := fakeSource{"strong.md": concept("strong.md", "Strong", "body", "doc:strong")}
	g, err := BuildGrounding(context.Background(), r, cs, "q", Options{MinScore: 0.5})
	if err != nil {
		t.Fatal(err)
	}
	if len(g.Chunks) != 1 || g.Chunks[0].ID != "strong.md" {
		t.Fatalf("a top hit at/above MinScore must proceed normally, got %+v", g.Chunks)
	}
}
