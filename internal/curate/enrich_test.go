package curate

import (
	"context"
	"strings"
	"testing"

	"pixkb/internal/okf"
)

// fakeIntentFixer returns canned intent_terms keyed by concept id.
type fakeIntentFixer struct {
	out  map[string]string
	seen []string
}

func (f *fakeIntentFixer) EnrichTerms(_ context.Context, c okf.Concept) (string, error) {
	f.seen = append(f.seen, c.ID)
	return f.out[c.ID], nil
}

func enrichBaseConcept() okf.Concept {
	return okf.Concept{
		ID: "reference/chave.md", Type: "Reference", Title: "Tipos de Chave Pix",
		SourceURI:  "doc:bacen",
		Body:       "# Tipos de Chave Pix\n\nCPF, CNPJ, e-mail, telefone e chave aleatória (EVP) resolvidas via DICT.",
		ContentSHA: "sha-chave",
	}
}

func TestEnrichPlanListsMissing(t *testing.T) {
	missing := enrichBaseConcept()
	filled := enrichBaseConcept()
	filled.ID = "reference/e2e.md"
	filled.Title = "EndToEndId"
	filled.ContentSHA = "sha-e2e"
	filled.IntentTerms = "e2eid identificador fim a fim"

	bundle := writeBundle(t, missing, filled)
	out, err := EnrichPlan(bundle, false)
	if err != nil {
		t.Fatal(err)
	}
	if out.Routed != 1 || len(out.Items) != 1 {
		t.Fatalf("plan routed=%d items=%d, want 1/1: %+v", out.Routed, len(out.Items), out.Items)
	}
	it := out.Items[0]
	if it.ConceptID != missing.ID || it.Agent != "enrich" || it.Status != StatusPlanned {
		t.Fatalf("item = %+v", it)
	}

	// --reenrich routes BOTH (the filled one too) so terms can be re-tuned.
	re, err := EnrichPlan(bundle, true)
	if err != nil {
		t.Fatal(err)
	}
	if re.Routed != 2 {
		t.Fatalf("reenrich plan routed=%d, want 2 (all concepts): %+v", re.Routed, re.Items)
	}
}

func TestEnrichReenrichRoutesFilled(t *testing.T) {
	filled := enrichBaseConcept()
	filled.IntentTerms = "evp chave aleatória dict" // already enriched
	cur := &Curator{
		Bundle:   writeBundle(t, filled),
		Reenrich: true,
		Enricher: &fakeIntentFixer{out: map[string]string{filled.ID: "evp chave aleatória endereçamento dict novo"}},
	}
	out, err := cur.Enrich(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if out.Routed != 1 || out.Proposed != 1 {
		t.Fatalf("reenrich should route the already-filled concept: routed=%d proposed=%d", out.Routed, out.Proposed)
	}
}

func TestEnrichProposesCleanTerms(t *testing.T) {
	c := enrichBaseConcept()
	cur := &Curator{
		Bundle:   writeBundle(t, c),
		Enricher: &fakeIntentFixer{out: map[string]string{c.ID: "evp chave aleatória endereçamento dict sinônimos"}},
	}
	out, err := cur.Enrich(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if out.Routed != 1 || out.Proposed != 1 || out.Rejected != 0 || out.Errors != 0 {
		t.Fatalf("routed=%d proposed=%d rejected=%d errors=%d, want 1/1/0/0", out.Routed, out.Proposed, out.Rejected, out.Errors)
	}
	if it := out.Items[0]; it.Status != StatusProposed || !strings.Contains(it.Detail, "terms") {
		t.Fatalf("item = %+v", it)
	}
}

func TestEnrichGateRejectsDeviationInTerms(t *testing.T) {
	c := enrichBaseConcept()
	cur := &Curator{
		Bundle:   writeBundle(t, c),
		Enricher: &fakeIntentFixer{out: map[string]string{c.ID: "chave evp kafka topic dict"}},
	}
	out, err := cur.Enrich(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if out.Rejected != 1 || out.Proposed != 0 {
		t.Fatalf("rejected=%d proposed=%d, want 1/0", out.Rejected, out.Proposed)
	}
	if d := out.Items[0].Detail; !strings.Contains(d, "deviation") {
		t.Fatalf("reject detail = %q, want deviation", d)
	}
}

func TestEnrichEmptyTermsNoChange(t *testing.T) {
	c := enrichBaseConcept()
	cur := &Curator{
		Bundle:   writeBundle(t, c),
		Enricher: &fakeIntentFixer{out: map[string]string{c.ID: "   "}},
	}
	out, err := cur.Enrich(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if out.Proposed != 0 || out.Items[0].Status != StatusNoChange {
		t.Fatalf("want no-change, got %+v", out.Items[0])
	}
}

func TestEnrichLimitCaps(t *testing.T) {
	a := enrichBaseConcept()
	b := enrichBaseConcept()
	b.ID = "reference/devolucao.md"
	b.Title = "Devolução"
	b.ContentSHA = "sha-dev"
	cur := &Curator{
		Bundle:   writeBundle(t, a, b),
		Limit:    1,
		Enricher: &fakeIntentFixer{out: map[string]string{a.ID: "termos um", b.ID: "termos dois"}},
	}
	out, err := cur.Enrich(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if out.Routed != 1 || out.Proposed != 1 {
		t.Fatalf("limit not honored: routed=%d proposed=%d", out.Routed, out.Proposed)
	}
}

func TestEnrichRequiresEnricher(t *testing.T) {
	cur := &Curator{Bundle: writeBundle(t, enrichBaseConcept())}
	if _, err := cur.Enrich(context.Background()); err == nil {
		t.Fatal("expected error when Enricher is nil")
	}
}

func TestParseIntentTerms(t *testing.T) {
	raw := "```json\n{\"concepts\":[{\"id\":\"a.md\",\"intent_terms\":\"foo bar baz\"}]}\n```"
	m, err := ParseIntentTerms(raw)
	if err != nil {
		t.Fatal(err)
	}
	if m["a.md"] != "foo bar baz" {
		t.Fatalf("parsed = %+v", m)
	}
}
