package ingest

import (
	"archive/zip"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// writeTestDocx builds a minimal .docx (zip with word/document.xml) from
// (style, text) paragraphs. An empty style means a body paragraph.
func writeTestDocx(t *testing.T, paras [][2]string) string {
	t.Helper()
	var b strings.Builder
	b.WriteString(`<?xml version="1.0"?><w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body>`)
	for _, p := range paras {
		b.WriteString("<w:p>")
		if p[0] != "" {
			b.WriteString(`<w:pPr><w:pStyle w:val="` + p[0] + `"/></w:pPr>`)
		}
		b.WriteString(`<w:r><w:t>` + p[1] + `</w:t></w:r></w:p>`)
	}
	b.WriteString(`</w:body></w:document>`)

	path := filepath.Join(t.TempDir(), "test.docx")
	f, err := os.Create(path)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()
	zw := zip.NewWriter(f)
	ct, err := zw.Create("[Content_Types].xml")
	require.NoError(t, err)
	_, _ = ct.Write([]byte(`<?xml version="1.0"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"/>`))
	dw, err := zw.Create("word/document.xml")
	require.NoError(t, err)
	_, _ = dw.Write([]byte(b.String()))
	require.NoError(t, zw.Close())
	return path
}

func TestDocxSource_HeadingSections(t *testing.T) {
	path := writeTestDocx(t, [][2]string{
		{"Heading1", "Visão Geral do Pix"},
		{"", "O Pix é o meio de pagamento instantâneo brasileiro."},
		{"Heading1", "Segurança"},
		{"", "A MED trata fraudes e devoluções."},
	})
	cs, err := NewDocxSource([]string{path}).Fetch(context.Background())
	require.NoError(t, err)
	require.Len(t, cs, 2)
	require.Equal(t, "Reference", cs[0].Type)
	require.Equal(t, "Visão Geral do Pix", cs[0].Title)
	require.Contains(t, cs[0].Body, "meio de pagamento instantâneo")
	require.Subset(t, cs[0].Tags, []string{"docx", "test"})
	require.Contains(t, cs[0].ID, "reference/test/")
	require.NotEmpty(t, cs[0].ContentSHA)
	require.Equal(t, "Segurança", cs[1].Title)
}

func TestDocxSource_NoHeadings(t *testing.T) {
	path := writeTestDocx(t, [][2]string{
		{"", "Um documento sem títulos, só um parágrafo."},
	})
	cs, err := NewDocxSource([]string{path}).Fetch(context.Background())
	require.NoError(t, err)
	require.Len(t, cs, 1)
	require.Equal(t, "Overview", cs[0].Title)
	require.Contains(t, cs[0].Body, "sem títulos")
}

func TestDocxSource_MissingFile(t *testing.T) {
	_, err := NewDocxSource([]string{"does-not-exist.docx"}).Fetch(context.Background())
	require.Error(t, err)
}

func TestDocxSource_Name(t *testing.T) {
	require.Equal(t, "docx", NewDocxSource(nil).Name())
}

// TestDocxSource_EmptyHeadingSplits confirms an empty-text heading between two
// bodies starts a new section rather than merging them (item 2).
func TestDocxSource_EmptyHeadingSplits(t *testing.T) {
	path := writeTestDocx(t, [][2]string{
		{"Heading1", "Introdução"},
		{"", "Primeiro parágrafo."},
		{"Heading1", ""}, // empty-text heading — must still split
		{"", "Segundo parágrafo isolado."},
	})
	cs, err := NewDocxSource([]string{path}).Fetch(context.Background())
	require.NoError(t, err)
	require.Len(t, cs, 2)
	require.Equal(t, "Introdução", cs[0].Title)
	require.Contains(t, cs[0].Body, "Primeiro parágrafo")
	require.NotContains(t, cs[0].Body, "Segundo parágrafo")
	require.Contains(t, cs[1].Body, "Segundo parágrafo isolado")
}

// TestDocxSource_TableText confirms text inside w:tbl cells is extracted in
// document order rather than silently dropped (item 3).
func TestDocxSource_TableText(t *testing.T) {
	xml := `<?xml version="1.0"?><w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body>` +
		`<w:p><w:pPr><w:pStyle w:val="Heading1"/></w:pPr><w:r><w:t>Participantes</w:t></w:r></w:p>` +
		`<w:p><w:r><w:t>Lista de participantes SPI:</w:t></w:r></w:p>` +
		`<w:tbl>` +
		`<w:tr><w:tc><w:p><w:r><w:t>ISPB</w:t></w:r></w:p></w:tc><w:tc><w:p><w:r><w:t>Nome</w:t></w:r></w:p></w:tc></w:tr>` +
		`<w:tr><w:tc><w:p><w:r><w:t>00000000</w:t></w:r></w:p></w:tc><w:tc><w:p><w:r><w:t>Banco do Brasil</w:t></w:r></w:p></w:tc></w:tr>` +
		`</w:tbl>` +
		`</w:body></w:document>`
	path := filepath.Join(t.TempDir(), "tbl.docx")
	f, err := os.Create(path)
	require.NoError(t, err)
	zw := zip.NewWriter(f)
	dw, err := zw.Create("word/document.xml")
	require.NoError(t, err)
	_, _ = dw.Write([]byte(xml))
	require.NoError(t, zw.Close())
	require.NoError(t, f.Close())

	cs, err := NewDocxSource([]string{path}).Fetch(context.Background())
	require.NoError(t, err)
	require.Len(t, cs, 1)
	require.Contains(t, cs[0].Body, "Lista de participantes SPI")
	require.Contains(t, cs[0].Body, "ISPB | Nome")
	require.Contains(t, cs[0].Body, "00000000 | Banco do Brasil")
}
