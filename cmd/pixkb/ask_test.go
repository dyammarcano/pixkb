package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"pixkb/internal/rag"
)

func TestRenderAnswer_HumanWithCitations(t *testing.T) {
	a := rag.Answer{Text: "A devolução usa PUT /pix/{e2eid}/devolucao.", Citations: []string{"api/openapi/put-pix-e2eid-devolucao-id.md"}}
	g := rag.Grounding{Chunks: []rag.Chunk{{ID: "api/openapi/put-pix-e2eid-devolucao-id.md", SourceURI: "doc:bacen"}}}
	var buf bytes.Buffer
	if err := renderAnswer(&buf, a, g, false); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	for _, want := range []string{"A devolução usa PUT", "Citations:", "put-pix-e2eid-devolucao-id.md", "doc:bacen"} {
		if !strings.Contains(out, want) {
			t.Fatalf("human output missing %q in:\n%s", want, out)
		}
	}
}

func TestRenderAnswer_JSON(t *testing.T) {
	a := rag.Answer{Text: "x", Citations: []string{"a.md"}}
	g := rag.Grounding{Chunks: []rag.Chunk{{ID: "a.md", SourceURI: "doc:a"}}}
	var buf bytes.Buffer
	if err := renderAnswer(&buf, a, g, true); err != nil {
		t.Fatal(err)
	}
	var got askJSON
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, buf.String())
	}
	if got.Answer != "x" || len(got.Citations) != 1 || got.Citations[0].ID != "a.md" || got.Citations[0].Source != "doc:a" {
		t.Fatalf("json = %+v", got)
	}
}

func TestRenderAnswer_RefusalNoCitations(t *testing.T) {
	a := rag.Answer{Text: "não consta na base de conhecimento", Refused: true}
	var buf bytes.Buffer
	if err := renderAnswer(&buf, a, rag.Grounding{}, false); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, "não consta") || strings.Contains(out, "Citations:") {
		t.Fatalf("refusal should print the note with no Citations block, got:\n%s", out)
	}
}
