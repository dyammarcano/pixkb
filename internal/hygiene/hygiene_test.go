package hygiene

import (
	"testing"

	"pixkb/internal/okf"
)

func has(fs []Finding, ch Check, id string) *Finding {
	for i := range fs {
		if fs[i].Check == ch && fs[i].ConceptID == id {
			return &fs[i]
		}
	}
	return nil
}

// a well-formed BACEN-canonical concept used as the clean baseline.
func cleanConcept() okf.Concept {
	return okf.Concept{
		ID: "reference/x/clean.md", Type: "Reference", Title: "Tipos de Chave Pix",
		Body:      "# Tipos de Chave Pix\n\nCPF, CNPJ, e-mail, telefone e chave aleatória (EVP) resolvidas via DICT.",
		SourceURI: "doc:bacen", ContentSHA: "sha-clean",
	}
}

func TestScan_CleanConceptNoErrors(t *testing.T) {
	rep := Scan([]okf.Concept{cleanConcept()})
	if !rep.Clean() {
		t.Fatalf("clean concept produced errors: %+v", rep.Errors())
	}
}

func TestMissingIntentTerms(t *testing.T) {
	empty := cleanConcept()
	empty.ID = "reference/x/empty.md"
	blank := cleanConcept()
	blank.ID = "reference/x/blank.md"
	blank.IntentTerms = "   " // whitespace-only counts as missing
	filled := cleanConcept()
	filled.ID = "reference/x/filled.md"
	filled.IntentTerms = "chave aleatória EVP sinônimos"

	out := MissingIntentTerms([]okf.Concept{filled, empty, blank})

	if has(out, CheckMissingIntentTerms, "reference/x/filled.md") != nil {
		t.Fatal("filled concept must not be flagged")
	}
	for _, id := range []string{"reference/x/blank.md", "reference/x/empty.md"} {
		f := has(out, CheckMissingIntentTerms, id)
		if f == nil {
			t.Fatalf("expected missing-intent-terms finding for %s", id)
		}
		if f.Severity != SeverityWarn || !f.Fixable {
			t.Fatalf("finding for %s must be a fixable WARN, got %+v", id, *f)
		}
	}
	// deterministic, sorted by id: blank < empty.
	if len(out) != 2 || out[0].ConceptID != "reference/x/blank.md" {
		t.Fatalf("expected 2 findings sorted by id, got %+v", out)
	}

	// Must NOT leak into the default Scan (enrichment is not a hygiene defect).
	rep := Scan([]okf.Concept{empty})
	if has(rep.Findings, CheckMissingIntentTerms, empty.ID) != nil {
		t.Fatal("default Scan must not emit missing-intent-terms")
	}
}

func TestScan_DeviationDetected(t *testing.T) {
	cases := []struct{ name, body string }{
		{"broker", "# T\n\nThe Recebedor PSP publishes to a Pulsar topic for pix-in."},
		{"infra", "# T\n\nDeployed via ArgoCD into the pix namespace on Kubernetes."},
		{"microservice", "# T\n\nHandled by orchestration-go-pix-in before settlement."},
		{"correlation", "# T\n\nThe correlationId threads through every hop."},
		{"sql", "# T\n\nStored via INSERT INTO pix_tx (e2eid, amount)."},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := cleanConcept()
			c.ID = "reference/x/" + tc.name + ".md"
			c.Body = tc.body
			c.ContentSHA = tc.name
			rep := Scan([]okf.Concept{c})
			f := has(rep.Findings, CheckDeviation, c.ID)
			if f == nil {
				t.Fatalf("no deviation finding for %s: %+v", tc.name, rep.Findings)
			}
			if f.Severity != SeverityError || !f.Fixable {
				t.Errorf("deviation should be fixable error, got %+v", f)
			}
			if rep.Clean() {
				t.Error("report with a deviation must not be Clean()")
			}
		})
	}
}

func TestScan_NoFalsePositiveOnNormativeProse(t *testing.T) {
	c := cleanConcept()
	c.Body = "# Liquidação no SPI\n\nA liquidação ocorre nas contas Reservas Bancárias " +
		"mantidas no Banco Central; o SPI processa a ordem pacs.008 e confirma via pacs.002."
	c.ContentSHA = "spi"
	rep := Scan([]okf.Concept{c})
	if f := has(rep.Findings, CheckDeviation, c.ID); f != nil {
		t.Fatalf("normative SPI prose flagged as deviation: %s", f.Detail)
	}
}

func TestScan_JunkTitles(t *testing.T) {
	mk := func(id, title string) okf.Concept {
		return okf.Concept{ID: id, Type: "ManualSection", Title: title,
			Body:      "# x\n\n" + "enough body content here to clear the stub threshold easily.",
			SourceURI: "pdf:m", ContentSHA: id}
	}
	concepts := []okf.Concept{
		mk("m/1.md", "ANEXO IV"),
		mk("m/2.md", "CONCLUÍDA é"),
		mk("m/3.md", "12.3"),
		mk("m/4.md", ""),
	}
	rep := Scan(concepts)
	for _, id := range []string{"m/1.md", "m/2.md", "m/3.md", "m/4.md"} {
		if has(rep.Findings, CheckJunkTitle, id) == nil {
			t.Errorf("expected junk-title finding for %s", id)
		}
	}
	// empty title is an error; fragments are warnings.
	if f := has(rep.Findings, CheckJunkTitle, "m/4.md"); f == nil || f.Severity != SeverityError {
		t.Errorf("empty title should be an error: %+v", f)
	}
}

func TestScan_StubBrokenLinkProvenanceDuplicate(t *testing.T) {
	a := okf.Concept{ID: "a.md", Type: "Reference", Title: "Concept A",
		Body: "# Concept A\n\nSee [B](b.md) and [Ghost](ghost.md) for more.", SourceURI: "", ContentSHA: "sA"}
	b := okf.Concept{ID: "b.md", Type: "Reference", Title: "Concept A", // identical title -> duplicate
		Body: "# Concept A\n\ntiny", SourceURI: "doc:b", ContentSHA: "sB"} // tiny body -> stub
	rep := Scan([]okf.Concept{a, b})

	if has(rep.Findings, CheckMissingProv, "a.md") == nil {
		t.Error("a.md missing-provenance not flagged")
	}
	if has(rep.Findings, CheckBrokenLink, "a.md") == nil {
		t.Error("a.md broken-link to ghost.md not flagged")
	}
	if has(rep.Findings, CheckStubBody, "b.md") == nil {
		t.Error("b.md stub-body not flagged")
	}
	if has(rep.Findings, CheckDuplicate, "b.md") == nil {
		t.Error("b.md duplicate-title not flagged")
	}
	// the live link [[b.md]] must NOT be flagged broken.
	for _, f := range rep.Findings {
		if f.Check == CheckBrokenLink && f.Detail == `link to missing concept "b.md"` {
			t.Error("live link b.md wrongly flagged broken")
		}
	}
}

func TestScan_DuplicateBodyBySHA(t *testing.T) {
	a := okf.Concept{ID: "a.md", Type: "Reference", Title: "A",
		Body: "# A\n\nidentical body content shared across two concepts here.", SourceURI: "x", ContentSHA: "dup"}
	b := a
	b.ID, b.Title = "b.md", "B"
	rep := Scan([]okf.Concept{a, b})
	if has(rep.Findings, CheckDuplicate, "b.md") == nil {
		t.Error("same content_sha duplicate not flagged on b.md")
	}
}

// Gate use: a proposed fix that still contains a deviation must not be Clean().
func TestScan_SampleDataFragment(t *testing.T) {
	// An OCR'd worked-example screen: placeholder merchant title + sample CNPJ in
	// the description. It would pass junk-title and stub checks, but must be
	// flagged as sample-data.
	c := okf.Concept{
		ID: "manuals/m/secao-73.md", Type: "ManualSection",
		Title:       "FULANO DE TAL EIRELI",
		Description: "CNPJ 00.123.456/7891",
		Body:        "# FULANO DE TAL EIRELI\n\nExpira em: 31/03/2020 17:00:00\nDetalhes do Pagamento\nConfirma?",
		SourceURI:   "pdf:manual", ContentSHA: "sha-sample",
	}
	rep := Scan([]okf.Concept{c})
	f := has(rep.Findings, CheckSampleData, c.ID)
	if f == nil {
		t.Fatalf("sample-data fragment not flagged: %+v", rep.Findings)
	}
	if f.Severity != SeverityWarn || !f.Fixable {
		t.Fatalf("sample-data finding should be a fixable warn: %+v", f)
	}
}

func TestScan_NoFalsePositiveOnRealConcept(t *testing.T) {
	// A normative concept that legitimately mentions CNPJ as a key TYPE must NOT
	// trip the sample-data check (no placeholder name, no sample taxid literal).
	c := okf.Concept{
		ID: "reference/x/keys.md", Type: "Reference",
		Title:       "Tipos de Chave Pix (CPF, CNPJ, e-mail, telefone, EVP)",
		Description: "Chaves de endereçamento Pix registradas no DICT",
		Body:        "# Tipos de Chave Pix\n\nO CNPJ identifica pessoa jurídica; o CPF, pessoa natural.",
		SourceURI:   "doc:bacen", ContentSHA: "sha-keys",
	}
	rep := Scan([]okf.Concept{c})
	if f := has(rep.Findings, CheckSampleData, c.ID); f != nil {
		t.Fatalf("false positive on normative concept: %+v", f)
	}
}

func TestReport_GatePredicate(t *testing.T) {
	bad := cleanConcept()
	bad.Body = "# T\n\nrouted through Kafka"
	bad.ContentSHA = "bad"
	if Scan([]okf.Concept{bad}).Clean() {
		t.Fatal("gate must reject a proposed concept that still deviates")
	}
	if !Scan([]okf.Concept{cleanConcept()}).Clean() {
		t.Fatal("gate must accept a clean proposed concept")
	}
}
