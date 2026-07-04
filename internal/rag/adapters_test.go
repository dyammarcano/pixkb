package rag

import (
	"context"
	"strings"
	"testing"
)

func TestAsk_RedactsPIIInAnswerTextByDefault(t *testing.T) {
	r := &fakeRetriever{hits: []Hit{{ID: "a.md", Score: 1}}}
	cs := fakeSource{"a.md": concept("a.md", "A", "body", "doc:a")}
	gen := &fakeGen{reply: `{"answer":"Ligue (11) 98765-4321 ou envie CPF 123.456.789-01","citations":["a.md"],"refused":false}`}

	ans, _, err := Ask(context.Background(), r, cs, gen, "q", Options{})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(ans.Text, "98765-4321") || strings.Contains(ans.Text, "123.456.789-01") {
		t.Fatalf("PII must be redacted by default, got %q", ans.Text)
	}
	if len(ans.Citations) != 1 || ans.Citations[0] != "a.md" {
		t.Fatalf("citations must be untouched by redaction, got %v", ans.Citations)
	}
}

func TestAsk_NoPIIFilterOptOutSkipsRedaction(t *testing.T) {
	r := &fakeRetriever{hits: []Hit{{ID: "a.md", Score: 1}}}
	cs := fakeSource{"a.md": concept("a.md", "A", "body", "doc:a")}
	gen := &fakeGen{reply: `{"answer":"CPF 123.456.789-01","citations":["a.md"],"refused":false}`}

	ans, _, err := Ask(context.Background(), r, cs, gen, "q", Options{NoPIIFilter: true})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(ans.Text, "123.456.789-01") {
		t.Fatalf("NoPIIFilter must skip redaction, got %q", ans.Text)
	}
}
