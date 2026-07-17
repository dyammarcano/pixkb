package ingest

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestISOSpecSource_DefaultDefs(t *testing.T) {
	t.Parallel()
	src := NewISOSpecSource(DefaultMsgDefs())
	assert.Equal(t, "iso-spec", src.Name())

	got, err := src.Fetch(context.Background())
	require.NoError(t, err)
	require.Len(t, got, 12)

	byID := map[string]int{}
	for i, c := range got {
		byID[c.ID] = i
	}

	pacs008, ok := byID["messages/pacs.008.md"]
	require.True(t, ok, "pacs.008 concept present")
	c := got[pacs008]
	assert.Equal(t, "PacsMessage", c.Type)
	assert.Equal(t, "en", c.Language)
	assert.NotEmpty(t, c.ContentSHA)
	assert.Contains(t, c.Body, "EndToEndId")
	// cross-links resolved to bundle-relative ids via ParseLinks
	assert.Contains(t, c.Links, "messages/pacs.002.md")
	assert.Contains(t, c.Links, "messages/pacs.004.md")
	// body "Related messages" links must be bundle-relative too, so hygiene can
	// resolve them against the messages/-prefixed concept ids (not bare filenames).
	assert.Contains(t, c.Body, "[pacs.002](messages/pacs.002.md)")
	assert.NotContains(t, c.Body, "](pacs.002.md)")

	camt, ok := byID["messages/camt.056.md"]
	require.True(t, ok)
	assert.Equal(t, "CamtMessage", got[camt].Type)
	assert.Contains(t, got[camt].Links, "messages/camt.029.md")

	// Statement messages (camt.052/.053/.054) present and intent-enriched so
	// "account statement" surfaces them.
	stmt, ok := byID["messages/camt.053.md"]
	require.True(t, ok, "camt.053 statement concept present")
	assert.Equal(t, "CamtMessage", got[stmt].Type)
	assert.Contains(t, got[stmt].Body, "Termos / Terms:")
	assert.Contains(t, got[stmt].Body, "account statement")

	// deterministic sorted-by-id order
	for i := 1; i < len(got); i++ {
		assert.True(t, got[i-1].ID < got[i].ID, "sorted by id")
	}
}

func TestISOSpecSource_InvalidDef(t *testing.T) {
	t.Parallel()
	src := NewISOSpecSource([]MsgDef{{ID: "", Family: "pacs"}})
	_, err := src.Fetch(context.Background())
	require.Error(t, err)
	assert.True(t, strings.Contains(err.Error(), "invalid msgdef"))
}
