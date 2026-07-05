package query

import "testing"

func TestComputeMatchedFields(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name        string
		q           string
		title       string
		intentTerms string
		body        string
		wantTokens  []string
		wantFields  []string
	}{
		{
			name:       "token found in title only",
			q:          "cobranca",
			title:      "Criar Cobranca Imediata",
			body:       "corpo sem o termo",
			wantTokens: []string{"cobranca"},
			wantFields: []string{"title"},
		},
		{
			name:       "token found in body only",
			q:          "webhook",
			title:      "Notificacoes",
			body:       "Configuracao de webhook para redelivery.",
			wantTokens: []string{"webhook"},
			wantFields: []string{"body"},
		},
		{
			name:       "token found in multiple fields",
			q:          "pix",
			title:      "Chave Pix",
			body:       "Todo pagamento Pix segue o fluxo SPI.",
			wantTokens: []string{"pix"},
			wantFields: []string{"title", "body"},
		},
		{
			name:       "no match at all",
			q:          "inexistente",
			title:      "Outro assunto qualquer",
			body:       "Nada relacionado por aqui.",
			wantTokens: nil,
			wantFields: nil,
		},
		{
			name:       "case-insensitivity",
			q:          "COBRANÇA",
			title:      "criar cobrança imediata",
			body:       "",
			wantTokens: []string{"cobrança"},
			wantFields: []string{"title"},
		},
		{
			name:       "punctuation is stripped during tokenization",
			q:          "cobrança?",
			title:      "Criar cobrança imediata",
			body:       "",
			wantTokens: []string{"cobrança"},
			wantFields: []string{"title"},
		},
		{
			name:        "token appears in intent_terms but not title/body",
			q:           "estorno",
			title:       "Devolucao de Pix",
			intentTerms: "estorno reembolso cancelamento",
			body:        "Fluxo de devolucao via API.",
			wantTokens:  []string{"estorno"},
			wantFields:  []string{"intent_terms"},
		},
		{
			name:       "multiple distinct tokens deduped and ordered by first occurrence in query",
			q:          "pix pix cobranca",
			title:      "Cobranca Pix",
			body:       "",
			wantTokens: []string{"pix", "cobranca"},
			wantFields: []string{"title"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := ComputeMatchedFields(tc.q, tc.title, tc.intentTerms, tc.body)
			if !equalStrSlices(got.Tokens, tc.wantTokens) {
				t.Errorf("Tokens = %v, want %v", got.Tokens, tc.wantTokens)
			}
			if !equalStrSlices(got.Fields, tc.wantFields) {
				t.Errorf("Fields = %v, want %v", got.Fields, tc.wantFields)
			}
		})
	}
}

func equalStrSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
