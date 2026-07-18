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

func TestParseStatuteAnexoStrict(t *testing.T) {
	text := `Art. 1º Ficam instituídos o IBS e a CBS.

Anexo I é parte integrante desta Lei.

Art. 2º Considera-se operação onerosa.

ANEXO II
TABELA DE ALÍQUOTAS
Produto A - 10%
`
	secs := parseStatute(text)

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

	// The title-case "Anexo I ..." body line must not trigger anexo mode.
	a1, ok := arts["1º"]
	require.True(t, ok, "Art. 1º must be parsed")
	require.Contains(t, a1.Body, "Anexo I é parte integrante desta Lei.",
		"the false-positive anexo line must be folded into Art. 1º body, not swallow it")

	// Art. 2º must still be parsed as a distinct article, not folded into an anexo.
	a2, ok := arts["2º"]
	require.True(t, ok, "Art. 2º must not be swallowed by the false-positive anexo line")
	require.Contains(t, a2.Body, "Considera-se operação onerosa")

	// Only the real uppercase ANEXO II heading produces an anexo section.
	require.Len(t, anexos, 1, "only the uppercase ANEXO II heading must produce an anexo section")
	require.Contains(t, anexos[0].Title, "ANEXO II")
	require.Contains(t, anexos[0].Body, "TABELA DE ALÍQUOTAS")
}

func TestParseStatuteEmpty(t *testing.T) {
	require.Empty(t, parseStatute("just some prose with no articles at all"),
		"text with no Art. markers and no anexo yields no article/anexo sections")
}

// TestParseStatuteWrappedArt confirms a "Art." marker whose number wrapped onto
// the next line is still recognised as an article (item 6).
func TestParseStatuteWrappedArt(t *testing.T) {
	text := "Art.\n1º Esta Lei institui o tributo.\nArt.\n2º Considera-se fato gerador.\n"
	secs := parseStatute(text)
	var nums []string
	for _, s := range secs {
		if s.Kind == "article" {
			nums = append(nums, s.Number)
		}
	}
	require.Equal(t, []string{"1º", "2º"}, nums, "both wrapped Art. markers parsed")
	require.Contains(t, secs[0].Body, "institui o tributo")
}

// TestJoinWrappedArtLeavesProseAlone confirms the join only fires on a bare
// "Art." line followed by a number, never on ordinary prose.
func TestJoinWrappedArtLeavesProseAlone(t *testing.T) {
	in := "Art. 5º já completo\numa frase qualquer\n42 não é um artigo\n"
	require.Equal(t, in, joinWrappedArt(in), "no bare Art. line -> unchanged")
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

// TestLegislationConceptsSeenAcrossFiles confirms that the same statute split
// across two files (shared seen-id map) disambiguates colliding article IDs
// rather than emitting duplicates that would trip GatherAll's dup-id abort
// (item 5).
func TestLegislationConceptsSeenAcrossFiles(t *testing.T) {
	secs := parseStatute(sampleStatute)
	seen := map[string]bool{}
	a := legislationConceptsSeen(secs, "part1.pdf", "lc-214-2025", "tax", seen)
	b := legislationConceptsSeen(secs, "part2.pdf", "lc-214-2025", "tax", seen)

	ids := map[string]bool{}
	for _, c := range a {
		ids[c.ID] = true
	}
	for _, c := range b {
		require.False(t, ids[c.ID], "duplicate id emitted across files: %s", c.ID)
		ids[c.ID] = true
	}
	require.Equal(t, len(a)+len(b), len(ids), "every id across both files is unique")
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
