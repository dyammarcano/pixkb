package postgres

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"pixkb/internal/okf"
)

// TestQueryConcepts_FiltersOrdersAndLimits proves QueryConcepts composes the
// WHERE/ORDER BY/LIMIT clauses correctly from already-parameterized HQL
// fragments, and omits WHERE entirely when it is empty. This package's
// integration tests share one Postgres database with no truncation between
// tests, so unique-per-run ids/types isolate this test's rows from any other
// seeded data (see TestFTS_IncludeTypesRestrictsResults in search_test.go).
func TestQueryConcepts_FiltersOrdersAndLimits(t *testing.T) {
	dsn := testDSN(t)
	ctx := context.Background()
	applyTestSchema(t, dsn)
	s, err := Open(ctx, dsn)
	require.NoError(t, err)
	defer s.Close()

	suffix := time.Now().UnixNano()
	articleID := fmt.Sprintf("query-article-%d.md", suffix)
	endpointID := fmt.Sprintf("query-endpoint-%d.md", suffix)
	manualID := fmt.Sprintf("query-manual-%d.md", suffix)

	ts := time.Date(2026, 6, 22, 10, 0, 0, 0, time.UTC)
	require.NoError(t, s.UpsertConcept(ctx, okf.Concept{
		ID: articleID, Type: "LegalArticle", Title: "Article",
		Body: "legal article body", Language: "pt", ContentSHA: "sha-a",
		Tags:  []string{"domain:tax", "lei:lc-214-2025", "titulo:ii"},
		Epoch: 1, Timestamp: ts,
	}))
	require.NoError(t, s.UpsertConcept(ctx, okf.Concept{
		ID: endpointID, Type: "ApiEndpoint", Title: "Endpoint",
		Body: "api endpoint body", Language: "pt", ContentSHA: "sha-e",
		Tags: []string{"domain:tax", "api"}, Epoch: 1, Timestamp: ts,
	}))
	require.NoError(t, s.UpsertConcept(ctx, okf.Concept{
		ID: manualID, Type: "ManualSection", Title: "Manual",
		Body: "manual section body", Language: "pt", ContentSHA: "sha-m",
		Tags: []string{"domain:pix", "manual"}, Epoch: 1, Timestamp: ts,
	}))

	// Sort the three ids to know the expected ASC/DESC ordering, since ids
	// carry a nanosecond suffix so alphabetical order is not guessable a priori.
	taxIDs := []string{articleID, endpointID}
	if taxIDs[0] > taxIDs[1] {
		taxIDs[0], taxIDs[1] = taxIDs[1], taxIDs[0]
	}

	// tags @> ARRAY[$1]::text[] with "domain:tax" returns exactly the two tax
	// concepts, in id ASC order.
	got, err := s.QueryConcepts(ctx, "tags @> ARRAY[$1]::text[] AND id LIKE $2", []any{"domain:tax", "query-%"}, "id ASC", 0)
	require.NoError(t, err)
	require.Len(t, got, 2)
	require.Equal(t, taxIDs[0], got[0].ID)
	require.Equal(t, taxIDs[1], got[1].ID)

	// type = $1 with "LegalArticle" returns only the article.
	got, err = s.QueryConcepts(ctx, "type = $1 AND id LIKE $2", []any{"LegalArticle", "query-%"}, "id ASC", 0)
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, articleID, got[0].ID)

	// order = "id DESC" reverses the tax-concept ordering.
	got, err = s.QueryConcepts(ctx, "tags @> ARRAY[$1]::text[] AND id LIKE $2", []any{"domain:tax", "query-%"}, "id DESC", 0)
	require.NoError(t, err)
	require.Len(t, got, 2)
	require.Equal(t, taxIDs[1], got[0].ID)
	require.Equal(t, taxIDs[0], got[1].ID)

	// limit = 1 truncates to a single row.
	got, err = s.QueryConcepts(ctx, "tags @> ARRAY[$1]::text[] AND id LIKE $2", []any{"domain:tax", "query-%"}, "id ASC", 1)
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, taxIDs[0], got[0].ID)

	// Empty where ("") emits no WHERE clause at all and returns every row in
	// the table (this package's tests share one database, so we only assert
	// our three seeded ids are present among the unfiltered result rather
	// than asserting an exact count).
	all, err := s.QueryConcepts(ctx, "", nil, "", 0)
	require.NoError(t, err)
	found := map[string]bool{}
	for _, c := range all {
		found[c.ID] = true
	}
	require.True(t, found[articleID])
	require.True(t, found[endpointID])
	require.True(t, found[manualID])
}

// TestQueryConcepts_ScansNullableTextColumns proves scanConcept tolerates a
// genuine SQL NULL in the nullable TEXT columns (title, description,
// source_uri, intent_terms) rather than failing the scan, matching the
// coalesce(...) pattern already used in search.go. UpsertConcept always
// writes non-NULL values for these columns, so this seeds the row with a
// direct INSERT leaving description, source_uri, and intent_terms as NULL.
func TestQueryConcepts_ScansNullableTextColumns(t *testing.T) {
	dsn := testDSN(t)
	ctx := context.Background()
	applyTestSchema(t, dsn)
	s, err := Open(ctx, dsn)
	require.NoError(t, err)
	defer s.Close()

	nullID := fmt.Sprintf("query-null-%d.md", time.Now().UnixNano())
	ts := time.Date(2026, 6, 22, 10, 0, 0, 0, time.UTC)

	// title is left NULL too by omitting it; description and source_uri are
	// explicitly NULL; intent_terms is omitted (also NULL) since 0002 adds it
	// as a nullable column with no default.
	_, err = s.pool.Exec(ctx, `
		INSERT INTO concept (id, type, description, resource, tags, language, body, content_sha, source_uri, first_epoch, last_epoch, updated_at)
		VALUES ($1, $2, NULL, $3, $4, $5, $6, $7, NULL, $8, $9, $10)`,
		nullID, "ManualSection", "", []string{"domain:null-test"}, "pt", "null concept body", "sha-null", 1, 1, ts,
	)
	require.NoError(t, err)

	got, err := s.QueryConcepts(ctx, "id = $1", []any{nullID}, "", 0)
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, nullID, got[0].ID)
	require.Equal(t, "", got[0].Title)
	require.Equal(t, "", got[0].Description)
	require.Equal(t, "", got[0].SourceURI)
	require.Equal(t, "", got[0].IntentTerms)
}
