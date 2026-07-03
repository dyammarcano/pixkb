package okf

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIntentTermsRoundTrip(t *testing.T) {
	dir := t.TempDir()
	c := Concept{
		ID:          "reference/x/keys.md",
		Type:        "Reference",
		Title:       "Tipos de Chave Pix",
		Language:    "pt",
		SourceURI:   "doc:bacen",
		IntentTerms: "chave aleatória EVP sinônimos endereçamento DICT",
		Body:        "# Tipos de Chave Pix\n\nCPF, CNPJ, e-mail, telefone, EVP.",
		Timestamp:   time.Date(2026, 6, 24, 0, 0, 0, 0, time.UTC),
	}
	require.NoError(t, WriteConcept(dir, c))

	// intent_terms persists in frontmatter and round-trips on read.
	raw, err := os.ReadFile(filepath.Join(dir, "reference", "x", "keys.md"))
	require.NoError(t, err)
	assert.Contains(t, string(raw), "intent_terms:")

	got, err := ReadConcept(filepath.Join(dir, "reference", "x", "keys.md"), dir)
	require.NoError(t, err)
	assert.Equal(t, c.IntentTerms, got.IntentTerms)
	// The intent terms are metadata, not content — the rendered body excludes them.
	assert.NotContains(t, got.Body, "sinônimos")
}

func TestWriteConcept(t *testing.T) {
	dir := t.TempDir()
	c := Concept{
		ID:        "messages/pacs.008.md",
		Type:      "PacsMessage",
		Title:     "pacs.008",
		Language:  "pt",
		Timestamp: time.Date(2026, 6, 22, 0, 0, 0, 0, time.UTC),
		Epoch:     0,
		Body:      "# pacs.008\n\nCustomerCreditTransfer.\n",
	}
	require.NoError(t, WriteConcept(dir, c))

	path := filepath.Join(dir, "messages", "pacs.008.md")
	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	s := string(raw)
	assert.True(t, len(s) > 0)
	assert.Equal(t, "---\n", s[:4])
	assert.Contains(t, s, "type: PacsMessage")
	assert.Contains(t, s, "\n---\n# pacs.008")
	assert.Contains(t, s, "CustomerCreditTransfer.")
}

func TestWriteConceptCreatesNestedDirs(t *testing.T) {
	dir := t.TempDir()
	c := Concept{
		ID:   "manuals/iniciacao-pix/secao-1.md",
		Type: "ManualSection",
		Body: "section body\n",
	}
	require.NoError(t, WriteConcept(dir, c))
	_, err := os.Stat(filepath.Join(dir, "manuals", "iniciacao-pix", "secao-1.md"))
	require.NoError(t, err)
}

func TestReconcileBundlePrunesOrphanIndexDir(t *testing.T) {
	dir := t.TempDir()
	// A dropped source's dir left with only a generated index.md (no concepts).
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "reference", "gone"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "reference", "gone", "index.md"), []byte("idx"), 0o644))
	// A live dir with a kept concept must survive (index.md and all).
	require.NoError(t, WriteConcept(dir, Concept{ID: "reference/live/c.md", Type: "Reference", Body: "keep\n"}))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "reference", "live", "index.md"), []byte("idx"), 0o644))

	require.NoError(t, ReconcileBundle(dir, map[string]struct{}{"reference/live/c.md": {}}))

	_, err := os.Stat(filepath.Join(dir, "reference", "gone"))
	assert.True(t, os.IsNotExist(err), "orphan index-only dir pruned")
	_, err = os.Stat(filepath.Join(dir, "reference", "live", "c.md"))
	require.NoError(t, err, "live dir survives")
	_, err = os.Stat(filepath.Join(dir, "reference", "live", "index.md"))
	require.NoError(t, err, "live dir's index.md survives")
}

func TestReconcileBundle(t *testing.T) {
	dir := t.TempDir()
	mkmd := func(rel, body string) {
		require.NoError(t, WriteConcept(dir, Concept{ID: rel, Type: "ManualSection", Body: body}))
	}
	// One kept concept + one orphan in the same dir; a generated index.md;
	// and a whole orphaned source dir (dropped from config) that must be purged.
	mkmd("manuals/m/secao-1.md", "keep\n")
	mkmd("manuals/m/secao-2.md", "orphan junk\n")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "manuals", "m", "index.md"), []byte("idx"), 0o644))
	mkmd("manuals/dropped/secao-1.md", "whole source removed\n")

	keep := map[string]struct{}{"manuals/m/secao-1.md": {}}
	require.NoError(t, ReconcileBundle(dir, keep))

	_, err := os.Stat(filepath.Join(dir, "manuals", "m", "secao-1.md"))
	require.NoError(t, err, "kept concept survives")
	_, err = os.Stat(filepath.Join(dir, "manuals", "m", "secao-2.md"))
	assert.True(t, os.IsNotExist(err), "same-dir orphan deleted")
	_, err = os.Stat(filepath.Join(dir, "manuals", "m", "index.md"))
	require.NoError(t, err, "generated index.md preserved")
	_, err = os.Stat(filepath.Join(dir, "manuals", "dropped", "secao-1.md"))
	assert.True(t, os.IsNotExist(err), "fully-dropped source orphan deleted")
	_, err = os.Stat(filepath.Join(dir, "manuals", "dropped"))
	assert.True(t, os.IsNotExist(err), "emptied source dir pruned")
}

func TestWriteConceptRejectsEmptyID(t *testing.T) {
	dir := t.TempDir()
	err := WriteConcept(dir, Concept{Type: "Repo"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "id")
}
