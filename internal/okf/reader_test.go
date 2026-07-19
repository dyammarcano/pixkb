package okf

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWriteReadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	body := "# pacs.008\n\nSee [pacs.002](pacs.002.md) for the status.\n"
	in := Concept{
		ID:          "messages/pacs.008.md",
		Type:        "PacsMessage",
		Title:       "pacs.008 CustomerCreditTransfer",
		Description: "Iniciação de Pix",
		Resource:    "iso:pacs.008.001.08",
		Tags:        []string{"pix", "pacs008"},
		Language:    "pt",
		Timestamp:   time.Date(2026, 6, 22, 10, 0, 0, 0, time.UTC),
		Epoch:       2,
		ContentSHA:  ComputeSHA(body),
		SourceURI:   "iso-msg:pacs.008",
		EmbeddedAt:  time.Date(2026, 6, 22, 11, 0, 0, 0, time.UTC),
		EmbedModel:  "hashing",
		Body:        body,
	}
	require.NoError(t, WriteConcept(dir, in))

	path := filepath.Join(dir, "messages", "pacs.008.md")
	out, err := ReadConcept(path, dir)
	require.NoError(t, err)

	assert.Equal(t, "messages/pacs.008.md", out.ID)
	assert.Equal(t, in.Type, out.Type)
	assert.Equal(t, in.Title, out.Title)
	assert.Equal(t, in.Description, out.Description)
	assert.Equal(t, in.Resource, out.Resource)
	assert.Equal(t, in.Tags, out.Tags)
	assert.Equal(t, in.Language, out.Language)
	assert.True(t, out.Timestamp.Equal(in.Timestamp))
	assert.Equal(t, in.Epoch, out.Epoch)
	assert.Equal(t, in.ContentSHA, out.ContentSHA)
	assert.Equal(t, in.SourceURI, out.SourceURI)
	assert.True(t, out.EmbeddedAt.Equal(in.EmbeddedAt))
	assert.Equal(t, in.EmbedModel, out.EmbedModel)
	assert.Equal(t, in.Body, out.Body)
	assert.Equal(t, []string{"messages/pacs.002.md"}, out.Links)
	assert.Equal(t, ComputeSHA(body), out.ContentSHA)
}

func TestWriteReadRoundTripDomainNormRef(t *testing.T) {
	dir := t.TempDir()
	in := Concept{
		ID:      "norms/res-bcb-1.md",
		Type:    "NormativeConcept",
		Domain:  "bacen-normative",
		NormRef: "RES-BCB-1-2020",
		Body:    "norm body\n",
	}
	require.NoError(t, WriteConcept(dir, in))

	out, err := ReadConcept(filepath.Join(dir, "norms", "res-bcb-1.md"), dir)
	require.NoError(t, err)
	assert.Equal(t, "bacen-normative", out.Domain)
	assert.Equal(t, "RES-BCB-1-2020", out.NormRef)

	// A concept whose front-matter omits domain reads back empty.
	require.NoError(t, WriteConcept(dir, Concept{ID: "plain.md", Type: "Repo", Body: "x\n"}))
	plain, err := ReadConcept(filepath.Join(dir, "plain.md"), dir)
	require.NoError(t, err)
	assert.Equal(t, "", plain.Domain)
	assert.Equal(t, "", plain.NormRef)
}

func TestReadConceptIDIsBundleRelativeSlash(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, WriteConcept(dir, Concept{
		ID:   "apis/dict-api/lookup.md",
		Type: "ApiEndpoint",
		Body: "lookup body\n",
	}))
	path := filepath.Join(dir, "apis", "dict-api", "lookup.md")
	out, err := ReadConcept(path, dir)
	require.NoError(t, err)
	assert.Equal(t, "apis/dict-api/lookup.md", out.ID)
}

func TestReadConceptMissingFile(t *testing.T) {
	dir := t.TempDir()
	_, err := ReadConcept(filepath.Join(dir, "nope.md"), dir)
	require.Error(t, err)
}
