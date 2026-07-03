package ingest

import (
	"context"
	"os"
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
