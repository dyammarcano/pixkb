package ingest

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSplitSections(t *testing.T) {
	t.Parallel()
	text := "intro paragraph here\n1. Primeira Secao\nconteudo da primeira\n2. Segunda Secao\nconteudo da segunda\n"
	secs := splitSections(text)
	require.GreaterOrEqual(t, len(secs), 2)
	titles := map[string]bool{}
	for _, s := range secs {
		titles[s.title] = true
	}
	assert.True(t, titles["1. Primeira Secao"], "numbered heading detected")
	assert.True(t, titles["2. Segunda Secao"])
}

func TestSplitSections_NoHeadings(t *testing.T) {
	t.Parallel()
	secs := splitSections("just some flowing text with no headings at all")
	require.Len(t, secs, 1)
	assert.Equal(t, "Documento", secs[0].title)
}

func TestIsHeading(t *testing.T) {
	t.Parallel()
	headings := []string{
		"1. Primeira Secao",
		"3.2 Iniciação do Pagamento",
		"MAPEAMENTO DO PROCESSO",
	}
	for _, h := range headings {
		assert.Truef(t, isHeading(h), "should be heading: %q", h)
	}
	// OCR noise / table cells / vowel-less codes must NOT become titles.
	notHeadings := []string{
		"63 04",
		"EMV QRCPS",
		"00 01 02 03",
		"",
		"a normal sentence in lower case that just flows on and on",
		`O “ANEXO IV`, // leading stray article + caps fragment
		`A “SECAO X`,
		"2 e o",
		"CONCLUÍDA é",
	}
	for _, n := range notHeadings {
		assert.Falsef(t, isHeading(n), "should NOT be heading: %q", n)
	}
}

func TestCleanTitle(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "Iniciação do Pagamento", cleanTitle("  Iniciação   do    Pagamento  "))
}

func TestSlugify(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "foo-bar-baz", slugify("Foo_Bar Baz!"))
	assert.Equal(t, "ii-manual", slugify("II__Manual"))
}

func TestStripTOCRegion(t *testing.T) {
	// Mirrors the real manual: title/version front matter, then the Sumário TOC
	// block (number lines, dropcap fragments, dot-leader runs, page numbers),
	// then real body prose. Dot-leaders appear ONLY in the TOC.
	toc := strings.Join([]string{
		"Manual", "de Padrões", "Versão 2.8", // front matter (kept)
		"Sumário", // TOC start marker
		"1.", "INTRODUÇÃO", "................................", "6",
		"2.", "INICIAÇÃO POR QR CODE", "....................", "6",
		"2.6.", "I", "NICIAÇÃO VIA ", "QR", "C", "ODE ", "E", "STÁTICO",
		"........................", " ", "11",
		// --- body begins: no more dot-leaders ---
		"1.", "Introdução",
		"O Pix é o meio de pagamento instantâneo brasileiro que permite...",
	}, "\n")

	out := stripTOCRegion(toc)

	require.Contains(t, out, "Manual", "front matter before Sumário is kept")
	require.NotContains(t, out, "Sumário", "the Sumário marker is dropped")
	require.NotContains(t, out, "NICIAÇÃO VIA ", "TOC dropcap fragments are dropped")
	require.NotContains(t, out, "....", "no dot-leader lines survive")
	require.Contains(t, out, "O Pix é o meio de pagamento", "body prose is preserved")
	require.Contains(t, out, "Introdução", "body heading is preserved")
}

func TestStripTOCRegion_NoSumario(t *testing.T) {
	in := "3.2 Serviço\n\nUm texto qualquer sem sumário.\n"
	require.Equal(t, in, stripTOCRegion(in), "no Sumário -> unchanged")
}

func TestStripTOCRegion_SumarioNoDotLeaders(t *testing.T) {
	in := "Sumário\n1. Introdução\nTexto sem dot-leaders.\n"
	require.Equal(t, in, stripTOCRegion(in), "Sumário but no dot-leaders -> unchanged")
}

func TestLinePredicates(t *testing.T) {
	require.True(t, isDotLeader("........"))
	require.True(t, isDotLeader("  ....  "))
	require.False(t, isDotLeader("... x"))
	require.False(t, isDotLeader(""))
	require.True(t, isBarePageNumber("38"))
	require.True(t, isBarePageNumber("6"))
	require.False(t, isBarePageNumber("2.6."))
	require.False(t, isBarePageNumber("ANEXO"))
}

func TestPDFSource_RealFile(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping real-PDF parse in -short mode")
	}
	path := `C:\Users\dyamm\Downloads\II_ManualdePadroesparaIniciacaodoPix.pdf`
	if _, err := os.Stat(path); err != nil {
		t.Skip("real PDF not present: " + path)
	}
	src := NewPDFSource([]string{path})
	cs, err := src.Fetch(context.Background())
	require.NoError(t, err)
	require.NotEmpty(t, cs)
	assert.Equal(t, "ManualSection", cs[0].Type)
	assert.Equal(t, "pt", cs[0].Language)
	assert.NotEmpty(t, cs[0].ContentSHA)
	assert.Contains(t, cs[0].ID, "manuals/")
}

func manualPDFPath() string {
	base := os.Getenv("LOCALAPPDATA")
	if base == "" {
		return "" // non-Windows / unset: acceptance test skips
	}
	p := filepath.Join(base, "PixKB", "mirror", "pdfs", "II_ManualdePadroesparaIniciacaodoPix.pdf")
	if _, err := os.Stat(p); err != nil {
		return ""
	}
	return p
}

func TestPDFFetch_NoTOCJunk(t *testing.T) {
	p := manualPDFPath()
	if p == "" {
		t.Skip("manual PDF not present in mirror dir")
	}
	concepts, err := NewPDFSource([]string{p}).Fetch(context.Background())
	require.NoError(t, err)
	require.NotEmpty(t, concepts)

	seen := map[string]bool{}
	junk := regexp.MustCompile(`^\.+$`)
	dropcapArtifacts := []string{"ERVIÇO DE", "ODE ESTÁTICO PARA PACS", "ECOMENDAÇÕES DE SEGURANÇA"}
	for _, c := range concepts {
		title := strings.TrimSpace(c.Title)
		require.False(t, junk.MatchString(title), "dot-leader title leaked: %q", title)
		require.NotRegexp(t, `^\d{1,4}$`, title, "bare page-number title leaked: %q", title)
		for _, a := range dropcapArtifacts {
			require.NotEqual(t, a, title, "known dropcap artifact leaked: %q", title)
		}
		require.False(t, seen[title], "duplicate ManualSection title: %q", title)
		seen[title] = true
	}
	t.Logf("manual produced %d ManualSection concepts (clean)", len(concepts))
}
