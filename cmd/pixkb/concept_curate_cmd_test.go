package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"pixkb/internal/okf"
)

// tempBundle writes the given concepts into a fresh bundle dir and points
// PIXKB_BUNDLE at it for the duration of the test.
func tempBundle(t *testing.T, concepts ...okf.Concept) string {
	t.Helper()
	dir := t.TempDir()
	for _, c := range concepts {
		if err := okf.WriteConcept(dir, c); err != nil {
			t.Fatalf("write concept %s: %v", c.ID, err)
		}
	}
	t.Setenv("PIXKB_BUNDLE", dir)
	t.Setenv("PIXKB_DSN", "") // these paths must not touch a DB
	return dir
}

func TestConceptGetCmd(t *testing.T) {
	tempBundle(t, okf.Concept{
		ID: "reference/x/keys.md", Type: "Reference", Title: "Tipos de Chave Pix",
		Body:      "# Tipos de Chave Pix\n\nCPF, CNPJ, e-mail, telefone, EVP.",
		SourceURI: "doc:bacen", ContentSHA: "sha1",
	})
	out, err := runCmd(t, "concept", "get", "reference/x/keys.md")
	require.NoError(t, err)
	assert.Contains(t, out, "Tipos de Chave Pix")
	assert.Contains(t, out, "EVP")
}

func TestCuratePlanCmd_Offline(t *testing.T) {
	// A junk all-caps fragment title is a junk-title finding routed to hygiene.
	tempBundle(t, okf.Concept{
		ID: "manuals/m/secao-x.md", Type: "ManualSection", Title: "ANEXO IV",
		Body:      "# ANEXO IV\n\nConteúdo normativo de iniciação do Pix com detalhe suficiente.",
		SourceURI: "pdf:m", ContentSHA: "sha2",
	})
	out, err := runCmd(t, "curate", "--plan")
	require.NoError(t, err)
	assert.Contains(t, out, "scanned 1 concepts")
	assert.Contains(t, out, "hygiene")
	assert.Contains(t, out, "manuals/m/secao-x.md")
}
