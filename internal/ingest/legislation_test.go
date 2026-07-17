package ingest

import (
	"testing"

	"github.com/stretchr/testify/require"
)

const sampleStatute = `LEI COMPLEMENTAR Nº 214, DE 16 DE JANEIRO DE 2025

Institui o Imposto sobre Bens e Serviços (IBS) e a Contribuição Social sobre Bens e Serviços (CBS).

LIVRO I
NORMAS GERAIS

TÍTULO I
DISPOSIÇÕES PRELIMINARES

CAPÍTULO I
DO FATO GERADOR

Art. 1º Ficam instituídos o IBS e a CBS.
§ 1º O disposto neste artigo aplica-se às operações.
I - com bens materiais;
II - com serviços.

Art. 2º Considera-se operação onerosa.
Parágrafo único. Aplica-se às importações.

CAPÍTULO II
DO SPLIT PAYMENT

Art. 31. O recolhimento ocorre na liquidação financeira da transação.

Art. 31-A. O split payment aplica-se ao arranjo Pix.

ANEXO I
TABELA DE ALÍQUOTAS
Produto A - 10%
`

func TestParseStatute(t *testing.T) {
	secs := parseStatute(sampleStatute)

	// Ementa is the leading section before Art. 1º.
	require.GreaterOrEqual(t, len(secs), 1)
	require.Equal(t, "ementa", secs[0].Kind)
	require.Contains(t, secs[0].Body, "Institui o Imposto")

	arts := map[string]statuteSection{}
	var anexos []statuteSection
	for _, s := range secs {
		switch s.Kind {
		case "article":
			arts[s.Number] = s
		case "anexo":
			anexos = append(anexos, s)
		}
	}

	// Four articles: 1º, 2º, 31, 31-A.
	require.Len(t, arts, 4)

	a1 := arts["1º"]
	require.Equal(t, "article", a1.Kind)
	require.Contains(t, a1.Body, "Ficam instituídos")
	require.Contains(t, a1.Body, "com serviços")          // inciso accumulated
	require.Equal(t, "I", a1.Livro)
	require.Equal(t, "I", a1.Titulo)
	require.Equal(t, "I", a1.Capitulo)

	a2 := arts["2º"]
	require.Contains(t, a2.Body, "Parágrafo único")       // parágrafo único folded in

	a31 := arts["31"]
	require.Equal(t, "II", a31.Capitulo)                  // capítulo context advanced
	require.Contains(t, a31.Body, "liquidação financeira")

	_, ok := arts["31-A"]
	require.True(t, ok, "letter-suffixed article Art. 31-A must be captured")

	// One Anexo, emitted as an anexo section.
	require.Len(t, anexos, 1)
	require.Contains(t, anexos[0].Title, "ANEXO I")
	require.Contains(t, anexos[0].Body, "TABELA DE ALÍQUOTAS")
}

func TestParseStatuteEmpty(t *testing.T) {
	require.Empty(t, parseStatute("just some prose with no articles at all"),
		"text with no Art. markers and no anexo yields no article/anexo sections")
}
