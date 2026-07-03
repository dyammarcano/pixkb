package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"pixkb/internal/okf"
)

func TestUpsertConcept_InsertThenUpdate(t *testing.T) {
	dsn := testDSN(t)
	ctx := context.Background()
	applyTestSchema(t, dsn)
	s, err := Open(ctx, dsn)
	require.NoError(t, err)
	defer s.Close()
	truncateAll(t, s)

	c := okf.Concept{
		ID:          "messages/pacs.008.md",
		Type:        "message",
		Title:       "pacs.008 Credit Transfer",
		Description: "Customer credit transfer",
		Resource:    "pacs.008",
		Tags:        []string{"pix", "pacs008"},
		Language:    "pt",
		Body:        "FI to FI customer credit transfer message.",
		ContentSHA:  "abc123",
		SourceURI:   "https://example/pacs008",
		Epoch:       3,
		Timestamp:   time.Date(2026, 6, 22, 10, 0, 0, 0, time.UTC),
	}

	// Insert.
	require.NoError(t, s.UpsertConcept(ctx, c))

	var (
		typ, title, sha string
		tags            []string
		firstEp, lastEp int
	)
	row := s.pool.QueryRow(ctx,
		"SELECT type, title, content_sha, tags, first_epoch, last_epoch FROM concept WHERE id=$1", c.ID)
	require.NoError(t, row.Scan(&typ, &title, &sha, &tags, &firstEp, &lastEp))
	require.Equal(t, "message", typ)
	require.Equal(t, []string{"pix", "pacs008"}, tags)
	require.Equal(t, 3, firstEp)
	require.Equal(t, 3, lastEp)

	// Update at a later epoch: first_epoch preserved, last_epoch advances.
	c.Epoch = 5
	c.ContentSHA = "def456"
	c.Title = "pacs.008 v2"
	c.Tags = []string{"pix", "pacs008", "v2"}
	require.NoError(t, s.UpsertConcept(ctx, c))

	row = s.pool.QueryRow(ctx,
		"SELECT title, content_sha, tags, first_epoch, last_epoch FROM concept WHERE id=$1", c.ID)
	require.NoError(t, row.Scan(&title, &sha, &tags, &firstEp, &lastEp))
	require.Equal(t, "pacs.008 v2", title)
	require.Equal(t, "def456", sha)
	require.Equal(t, []string{"pix", "pacs008", "v2"}, tags)
	require.Equal(t, 3, firstEp, "first_epoch must be preserved across update")
	require.Equal(t, 5, lastEp, "last_epoch must advance to new epoch")

	// FTS queryable: to_tsquery should find the row.
	var ftsMatch bool
	require.NoError(t, s.pool.QueryRow(ctx,
		"SELECT fts @@ to_tsquery('simple', 'credit') FROM concept WHERE id=$1", c.ID,
	).Scan(&ftsMatch))
	require.True(t, ftsMatch, "fts column should match 'credit'")

	// Single row only.
	var cnt int
	require.NoError(t, s.pool.QueryRow(ctx, "SELECT count(*) FROM concept").Scan(&cnt))
	require.Equal(t, 1, cnt)
}
