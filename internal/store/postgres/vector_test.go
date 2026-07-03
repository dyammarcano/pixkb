package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"pixkb/internal/embed"
	"pixkb/internal/okf"
)

func TestVector_NearestAndFilters(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	emb := embed.NewHashing(256)

	seed := func(id, typ, title, body string) []float32 {
		c := okf.Concept{
			ID: id, Type: typ, Title: title, Body: body,
			ContentSHA: okf.ComputeSHA(body), Language: "en", Epoch: 0,
		}
		require.NoError(t, s.UpsertConcept(ctx, c))
		vs, err := emb.Embed(ctx, []string{title + " " + body})
		require.NoError(t, err)
		require.NoError(t, s.UpsertEmbedding(ctx, id, 0, emb.Name(), vs[0], time.Now().UTC()))
		return vs[0]
	}

	vPacs := seed("messages/pacs.008.md", "PacsMessage", "Customer Credit Transfer", "pacs008 credit transfer payment")
	seed("messages/camt.056.md", "CamtMessage", "Cancellation Request", "camt056 cancellation devolucao request")

	// Querying with the pacs vector ranks the pacs concept first (self-match).
	hits, err := s.Vector(ctx, vPacs, Filter{Limit: 10})
	require.NoError(t, err)
	require.NotEmpty(t, hits)
	assert.Equal(t, "messages/pacs.008.md", hits[0].ID)
	assert.Equal(t, 1, hits[0].Rank)
	assert.InDelta(t, 1.0, hits[0].Score, 1e-4)

	// Type filter narrows to the camt concept only.
	camtHits, err := s.Vector(ctx, vPacs, Filter{Type: "CamtMessage", Limit: 10})
	require.NoError(t, err)
	require.Len(t, camtHits, 1)
	assert.Equal(t, "messages/camt.056.md", camtHits[0].ID)

	// Limit caps results.
	one, err := s.Vector(ctx, vPacs, Filter{Limit: 1})
	require.NoError(t, err)
	assert.Len(t, one, 1)
}
