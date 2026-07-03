package ingest

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"pixkb/internal/okf"
)

func TestCrossLink_RelatesSharedTerms(t *testing.T) {
	t.Parallel()
	in := []okf.Concept{
		{ID: "api/o/post-cob.md", Type: "ApiEndpoint", Title: "POST /cob",
			Description: "Criar cobrança imediata", Body: "# POST /cob"},
		{ID: "api/o/put-cob-txid.md", Type: "ApiEndpoint", Title: "PUT /cob/{txid}",
			Description: "Revisar cobrança", Body: "# PUT /cob/{txid}"},
		{ID: "manuals/m/secao-3.md", Type: "ManualSection", Title: "Cobrança imediata",
			Description: "Fluxo de cobrança", Body: "# Cobrança"},
		{ID: "messages/pacs.008.md", Type: "PacsMessage", Title: "Credit Transfer",
			Description: "Interbank settlement", Body: "# pacs.008"},
	}
	out := CrossLink(in)

	// The two /cob endpoints relate (path-family + shared "cobranca" term) and
	// both relate to the manual cobrança section.
	post := find(t, out, "api/o/post-cob.md")
	assert.Contains(t, post.Body, "## Related")
	assert.Contains(t, post.Links, "api/o/put-cob-txid.md")
	assert.Contains(t, post.Links, "manuals/m/secao-3.md")
	// Links are rendered as markdown so a disk read re-derives the same edges.
	assert.Contains(t, post.Body, "(manuals/m/secao-3.md)")

	// The unrelated ISO message shares no salient term -> no Related section.
	iso := find(t, out, "messages/pacs.008.md")
	assert.NotContains(t, iso.Body, "## Related")
	assert.Empty(t, iso.Links)

	// ContentSHA reflects the appended section (deterministic).
	require.NotEmpty(t, post.ContentSHA)
	assert.Equal(t, okf.ComputeSHA(post.Body), post.ContentSHA)
}

func TestCrossLink_Deterministic(t *testing.T) {
	t.Parallel()
	mk := func() []okf.Concept {
		return []okf.Concept{
			{ID: "a/cob.md", Type: "ApiEndpoint", Title: "POST /cob", Description: "cobrança", Body: "x"},
			{ID: "b/cob.md", Type: "ApiEndpoint", Title: "GET /cob", Description: "cobrança", Body: "y"},
		}
	}
	a := CrossLink(mk())
	b := CrossLink(mk())
	assert.Equal(t, find(t, a, "a/cob.md").Body, find(t, b, "a/cob.md").Body)
}

func find(t *testing.T, cs []okf.Concept, id string) okf.Concept {
	t.Helper()
	for _, c := range cs {
		if c.ID == id {
			return c
		}
	}
	t.Fatalf("concept %q not found", id)
	return okf.Concept{}
}

func TestNormalizeTerms_StripsDiacritics(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "cobranca devolucao", strings.TrimSpace(normalizeTerms("Cobrança devolução")))
}
