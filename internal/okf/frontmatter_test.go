package okf

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMarshalFrontmatter(t *testing.T) {
	ts := time.Date(2026, 6, 22, 10, 30, 0, 0, time.UTC)
	c := Concept{
		ID:          "messages/pacs.008.md",
		Type:        "PacsMessage",
		Title:       "pacs.008 CustomerCreditTransfer",
		Description: "Iniciação de Pix",
		Resource:    "iso:pacs.008.001.08",
		Tags:        []string{"pix", "pacs008"},
		Language:    "pt",
		Timestamp:   ts,
		Epoch:       0,
		ContentSHA:  "abc123",
		SourceURI:   "iso-msg:pacs.008",
		EmbeddedAt:  ts,
		EmbedModel:  "hashing",
		Body:        "ignored by marshalFrontmatter",
	}
	out, err := marshalFrontmatter(c)
	require.NoError(t, err)
	s := string(out)
	assert.Contains(t, s, "type: PacsMessage")
	assert.Contains(t, s, "title: pacs.008 CustomerCreditTransfer")
	assert.Contains(t, s, "language: pt")
	assert.Contains(t, s, "epoch: 0")
	assert.Contains(t, s, "content_sha: abc123")
	assert.Contains(t, s, "embed_model: hashing")
	assert.NotContains(t, s, "ignored by marshalFrontmatter")
}

func TestMarshalUnmarshalDomainNormRef(t *testing.T) {
	ts := time.Date(2026, 7, 19, 10, 30, 0, 0, time.UTC)
	c := Concept{
		Type:       "NormativeConcept",
		Title:      "Resolução BCB",
		Timestamp:  ts,
		Epoch:      1,
		ContentSHA: "cafe",
		Domain:     "bacen-normative",
		NormRef:    "RES-BCB-1-2020",
		EmbeddedAt: ts,
		EmbedModel: "hashing",
	}
	out, err := marshalFrontmatter(c)
	require.NoError(t, err)
	s := string(out)
	assert.Contains(t, s, "domain: bacen-normative")
	assert.Contains(t, s, "norm_ref: RES-BCB-1-2020")

	fm, err := unmarshalFrontmatter(out)
	require.NoError(t, err)
	assert.Equal(t, "bacen-normative", fm.Domain)
	assert.Equal(t, "RES-BCB-1-2020", fm.NormRef)
}

func TestUnmarshalFrontmatterDomainOmittedDefaultsEmpty(t *testing.T) {
	fm, err := unmarshalFrontmatter([]byte("type: Repo\ntitle: pix-api\n"))
	require.NoError(t, err)
	assert.Equal(t, "", fm.Domain)
	assert.Equal(t, "", fm.NormRef)

	// omitempty: absent fields must not be serialized.
	out, err := marshalFrontmatter(Concept{Type: "Repo"})
	require.NoError(t, err)
	assert.NotContains(t, string(out), "domain:")
	assert.NotContains(t, string(out), "norm_ref:")
}

func TestSplitDocument(t *testing.T) {
	raw := []byte("---\ntype: Repo\ntitle: pix-api\n---\n# pix-api\n\nbody line\n")
	front, body, err := splitDocument(raw)
	require.NoError(t, err)
	assert.Contains(t, string(front), "type: Repo")
	assert.Equal(t, "# pix-api\n\nbody line\n", body)
}

func TestSplitDocumentMissingFences(t *testing.T) {
	_, _, err := splitDocument([]byte("no frontmatter here\n"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "frontmatter")
}

func TestSplitDocumentBodyWithDashes(t *testing.T) {
	// Test that body containing --- (e.g. markdown horizontal rule) is preserved
	// Splits on FIRST closing fence only
	raw := []byte("---\ntype: Article\n---\n# Title\n\nintro\n\n---\n\nmore content\n")
	front, body, err := splitDocument(raw)
	require.NoError(t, err)
	assert.Contains(t, string(front), "type: Article")
	assert.Equal(t, "# Title\n\nintro\n\n---\n\nmore content\n", body)
	assert.Contains(t, body, "---")
}

func TestUnmarshalFrontmatterRoundTrip(t *testing.T) {
	ts := time.Date(2026, 6, 22, 10, 30, 0, 0, time.UTC)
	c := Concept{
		Type:       "CamtMessage",
		Title:      "camt.056",
		Tags:       []string{"pix", "devolucao"},
		Language:   "en",
		Timestamp:  ts,
		Epoch:      3,
		ContentSHA: "deadbeef",
		EmbeddedAt: ts,
		EmbedModel: "hashing",
	}
	out, err := marshalFrontmatter(c)
	require.NoError(t, err)
	fm, err := unmarshalFrontmatter(out)
	require.NoError(t, err)
	assert.Equal(t, "CamtMessage", fm.Type)
	assert.Equal(t, "camt.056", fm.Title)
	assert.Equal(t, []string{"pix", "devolucao"}, fm.Tags)
	assert.Equal(t, "en", fm.Language)
	assert.Equal(t, 3, fm.Epoch)
	assert.Equal(t, "deadbeef", fm.ContentSHA)
	assert.Equal(t, "hashing", fm.EmbedModel)
	assert.True(t, fm.Timestamp.Equal(ts))
	assert.True(t, strings.HasPrefix(fm.Timestamp.Format(time.RFC3339), "2026-06-22"))
}
