package ingest

import (
	"os"
	"strings"
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
	require.Contains(t, a1.Body, "com serviços") // inciso accumulated
	require.Equal(t, "I", a1.Livro)
	require.Equal(t, "I", a1.Titulo)
	require.Equal(t, "I", a1.Capitulo)

	a2 := arts["2º"]
	require.Contains(t, a2.Body, "Parágrafo único") // parágrafo único folded in

	a31 := arts["31"]
	require.Equal(t, "II", a31.Capitulo) // capítulo context advanced
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

func TestLegislationConceptsFromText(t *testing.T) {
	// Exercise concept-building directly from parsed sections (no PDF needed).
	concepts := legislationConcepts(parseStatute(sampleStatute), "res.pdf", "lc-214-2025", "tax")

	byID := map[string]okfConceptView{}
	for _, c := range concepts {
		byID[c.ID] = okfConceptView{Type: c.Type, Title: c.Title, Tags: c.Tags, Body: c.Body}
	}

	// Article 1º → padded id, LegalArticle type, domain:tax + structural tags.
	a1, ok := byID["legislation/lc-214-2025/art-0001.md"]
	require.True(t, ok, "art 0001 concept must exist; got ids %v", keysOf(byID))
	require.Equal(t, "LegalArticle", a1.Type)
	require.Equal(t, "Art. 1º", a1.Title)
	require.Subset(t, a1.Tags, []string{"legislacao", "domain:tax", "lei:lc-214-2025", "livro:i", "titulo:i", "capitulo:i"})

	// Letter-suffixed article keeps its suffix in the id.
	_, ok = byID["legislation/lc-214-2025/art-0031-a.md"]
	require.True(t, ok, "art 0031-a concept must exist; got ids %v", keysOf(byID))

	// Ementa concept.
	_, ok = byID["legislation/lc-214-2025/art-0000-ementa.md"]
	require.True(t, ok, "ementa concept must exist")

	// Anexo → Reference type, anexo tag.
	var anexo *okfConceptView
	for id, c := range byID {
		if strings.Contains(id, "/anexo-") {
			cc := c
			anexo = &cc
		}
	}
	require.NotNil(t, anexo, "anexo Reference concept must exist")
	require.Equal(t, "Reference", anexo.Type)
	require.Subset(t, anexo.Tags, []string{"domain:tax", "lei:lc-214-2025", "anexo"})
}

func TestNewLegislationSourceName(t *testing.T) {
	require.Equal(t, "legislation", NewLegislationSource(nil, "lc-214-2025", "tax").Name())
}

type okfConceptView struct {
	Type, Title string
	Tags        []string
	Body        string
}

func keysOf(m map[string]okfConceptView) []string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	return ks
}

func TestLegislationFixtureEndToEnd(t *testing.T) {
	data, err := os.ReadFile("testdata/sample-statute.txt")
	require.NoError(t, err)

	concepts := legislationConcepts(parseStatute(string(data)), "LC214-2025.pdf", "lc-214-2025", "tax")
	require.NotEmpty(t, concepts)

	var articles, references int
	for _, c := range concepts {
		require.Contains(t, c.Tags, "domain:tax", "every legislation concept carries domain:tax")
		require.Contains(t, c.Tags, "lei:lc-214-2025")
		require.True(t, strings.HasPrefix(c.ID, "legislation/lc-214-2025/"))
		switch c.Type {
		case "LegalArticle":
			articles++
		case "Reference":
			references++
		}
	}
	require.GreaterOrEqual(t, articles, 5, "ementa + at least 5 articles")
	require.GreaterOrEqual(t, references, 1, "at least one anexo Reference")

	// No duplicate ids (the gather layer rejects them; catch it earlier here).
	ids := map[string]bool{}
	for _, c := range concepts {
		require.False(t, ids[c.ID], "duplicate concept id %q", c.ID)
		ids[c.ID] = true
	}
}
