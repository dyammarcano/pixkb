package postgres

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"pixkb/internal/okf"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping postgres integration test in -short mode")
	}
	dsn := os.Getenv("PIXKB_TEST_DSN")
	if dsn == "" {
		t.Skip("PIXKB_TEST_DSN not set; skipping postgres integration test")
	}
	guardNotProdDSN(t, dsn)
	ctx := context.Background()
	s, err := Open(ctx, dsn)
	require.NoError(t, err)
	require.NoError(t, s.Truncate(ctx))
	t.Cleanup(s.Close)
	return s
}

func TestUpsertEmbedding(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	c := okf.Concept{
		ID:         "messages/pacs.008.md",
		Type:       "PacsMessage",
		Title:      "FI to FI Customer Credit Transfer",
		Body:       "pacs.008 credit transfer body",
		ContentSHA: okf.ComputeSHA("pacs.008 credit transfer body"),
		Language:   "pt",
		Epoch:      0,
	}
	require.NoError(t, s.UpsertConcept(ctx, c))

	vec := make([]float32, 256)
	for i := range vec {
		vec[i] = 0.01
	}
	at := time.Now().UTC()
	err := s.UpsertEmbedding(ctx, c.ID, 0, "hashing", vec, at)
	require.NoError(t, err)

	// Upsert again on same (id, epoch) must not error (conflict update).
	vec[0] = 0.5
	err = s.UpsertEmbedding(ctx, c.ID, 0, "hashing", vec, at.Add(time.Minute))
	require.NoError(t, err)

	var model string
	var dim int
	row := s.pool.QueryRow(ctx,
		`SELECT embed_model, dim FROM embedding WHERE id=$1 AND epoch=$2`, c.ID, 0)
	require.NoError(t, row.Scan(&model, &dim))
	assert.Equal(t, "hashing", model)
	assert.Equal(t, 256, dim)
}

func TestGetEmbedding_ReturnsLatestEpoch(t *testing.T) {
	dsn := testDSN(t)
	ctx := context.Background()
	applyTestSchema(t, dsn)
	s, err := Open(ctx, dsn)
	require.NoError(t, err)
	defer s.Close()

	_, err = s.pool.Exec(ctx, `
INSERT INTO concept (id, type, title, body, content_sha, first_epoch, last_epoch, updated_at)
VALUES ('x.md', 'Reference', 'X', 'body', 'sha', 1, 1, now())`)
	require.NoError(t, err)

	require.NoError(t, s.UpsertEmbedding(ctx, "x.md", 1, "hashing", []float32{1, 0, 0}, time.Now()))
	require.NoError(t, s.UpsertEmbedding(ctx, "x.md", 2, "hashing", []float32{0, 1, 0}, time.Now()))

	got, err := s.GetEmbedding(ctx, "x.md")
	require.NoError(t, err)
	assert.Equal(t, []float32{0, 1, 0}, got, "must return the LATEST epoch's vector, not the first")
}

func TestGetEmbedding_NotFound(t *testing.T) {
	dsn := testDSN(t)
	ctx := context.Background()
	applyTestSchema(t, dsn)
	s, err := Open(ctx, dsn)
	require.NoError(t, err)
	defer s.Close()

	_, err = s.GetEmbedding(ctx, "does-not-exist.md")
	require.Error(t, err)
}
