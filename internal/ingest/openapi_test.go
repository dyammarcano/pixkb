package ingest

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOpenAPISource_EmitsEndpoints(t *testing.T) {
	t.Parallel()
	spec := `
openapi: 3.0.0
info:
  title: Pix API
  version: "2.4"
paths:
  /pix:
    post:
      summary: Criar Pix
      operationId: criarPix
      tags: [pix]
    parameters:
      - name: x
  /pix/{id}:
    get:
      summary: Consultar Pix
      parameters:
        - name: id
          in: path
          required: true
          description: Identificador
          schema:
            type: string
      responses:
        "200":
          description: OK
          content:
            application/json:
              schema:
                $ref: '#/components/schemas/Pix'
        "404":
          description: Nao encontrado
`
	dir := t.TempDir()
	f := filepath.Join(dir, "openapi.yaml")
	require.NoError(t, os.WriteFile(f, []byte(spec), 0o644))

	cs, err := NewOpenAPISource([]string{f}).Fetch(context.Background())
	require.NoError(t, err)
	require.Len(t, cs, 2, "POST /pix and GET /pix/{id}; non-method 'parameters' skipped")

	byTitle := map[string]bool{}
	for _, c := range cs {
		assert.Equal(t, "ApiEndpoint", c.Type)
		assert.NotEmpty(t, c.ContentSHA)
		assert.Contains(t, c.ID, "api/openapi/")
		byTitle[c.Title] = true
	}
	assert.True(t, byTitle["POST /pix"])
	assert.True(t, byTitle["GET /pix/{id}"])

	// The GET endpoint body must carry the enriched params + responses + schema.
	var get string
	for _, c := range cs {
		if c.Title == "GET /pix/{id}" {
			get = c.Body
		}
	}
	assert.Contains(t, get, "## Parameters")
	assert.Contains(t, get, "`id` (path, required)")
	assert.Contains(t, get, "## Responses")
	assert.Contains(t, get, "200")
	assert.Contains(t, get, "Pix") // resolved $ref schema name
	assert.Contains(t, get, "404")
}

func TestOpenAPISource_BadSpecSkipped(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	f := filepath.Join(dir, "bad.yaml")
	require.NoError(t, os.WriteFile(f, []byte("\t::not yaml::"), 0o644))
	cs, err := NewOpenAPISource([]string{f}).Fetch(context.Background())
	require.NoError(t, err)
	assert.Empty(t, cs)
}

func TestOpenAPISource_WithDomainTagsEndpoints(t *testing.T) {
	dir := t.TempDir()
	spec := `{"openapi":"3.0.0","info":{"title":"Tributos","version":"1"},` +
		`"paths":{"/calcular":{"post":{"summary":"Calcula CBS/IBS"}}}}`
	path := filepath.Join(dir, "tributos-consumo.json")
	require.NoError(t, os.WriteFile(path, []byte(spec), 0o644))

	cs, err := NewOpenAPISourceWithDomain([]string{path}, "tax").Fetch(context.Background())
	require.NoError(t, err)
	require.NotEmpty(t, cs)
	assert.Equal(t, "ApiEndpoint", cs[0].Type)
	assert.Contains(t, cs[0].Tags, "domain:tax")
	assert.Contains(t, cs[0].Tags, "api")
}
