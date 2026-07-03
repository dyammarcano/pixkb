package ingest

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAPIDocSource_ParsesHTML(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	htmlDoc := `<html><head><title>DICT API - Key</title></head>
<body><script>var x=1;</script>
<h1>Consulta de Chave</h1>
<p>Resolve uma chave Pix (CPF, e-mail, telefone) para os dados da conta.</p>
<p>GET /api/v2/keys/{key} &amp; retorna ISPB.</p></body></html>`
	path := filepath.Join(dir, "dict-key.html")
	require.NoError(t, os.WriteFile(path, []byte(htmlDoc), 0o644))

	cs, err := NewAPIDocSource([]string{path}).Fetch(context.Background())
	require.NoError(t, err)
	require.Len(t, cs, 1)
	c := cs[0]
	assert.Equal(t, "apis/dict-key.md", c.ID)
	assert.Equal(t, "ApiEndpoint", c.Type)
	assert.Equal(t, "DICT API - Key", c.Title)
	assert.Contains(t, c.Body, "Resolve uma chave Pix")
	assert.Contains(t, c.Body, "retorna ISPB") // entity unescaped, tags stripped
	assert.NotContains(t, c.Body, "<p>")
	assert.NotContains(t, c.Body, "var x=1") // script stripped
	assert.NotEmpty(t, c.ContentSHA)
}
