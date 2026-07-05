package postgres

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"pixkb/internal/embed"
	"pixkb/internal/okf"
)

func seedConcept(t *testing.T, s *Store, id, typ, title, body string, tags []string, epoch int) {
	t.Helper()
	require.NoError(t, s.UpsertConcept(context.Background(), okf.Concept{
		ID: id, Type: typ, Title: title, Body: body, Tags: tags,
		Language: "pt", ContentSHA: "sha-" + id, Epoch: epoch,
		Timestamp: time.Date(2026, 6, 22, 10, 0, 0, 0, time.UTC),
	}))
}

func TestReplaceEdges_DeletesThenInserts(t *testing.T) {
	dsn := testDSN(t)
	ctx := context.Background()
	applyTestSchema(t, dsn)
	s, err := Open(ctx, dsn)
	require.NoError(t, err)
	defer s.Close()
	truncateAll(t, s)

	src := "messages/pacs.008.md"
	require.NoError(t, s.ReplaceEdges(ctx, src, []string{"a.md", "b.md"}))

	var dsts []string
	rows, err := s.pool.Query(ctx, "SELECT dst FROM edge WHERE src=$1 ORDER BY dst", src)
	require.NoError(t, err)
	for rows.Next() {
		var d string
		require.NoError(t, rows.Scan(&d))
		dsts = append(dsts, d)
	}
	require.NoError(t, rows.Err())
	require.Equal(t, []string{"a.md", "b.md"}, dsts)

	// Replace must remove the old set entirely.
	require.NoError(t, s.ReplaceEdges(ctx, src, []string{"c.md"}))
	var cnt int
	require.NoError(t, s.pool.QueryRow(ctx, "SELECT count(*) FROM edge WHERE src=$1", src).Scan(&cnt))
	require.Equal(t, 1, cnt)
	require.NoError(t, s.pool.QueryRow(ctx, "SELECT count(*) FROM edge WHERE src=$1 AND dst='c.md'", src).Scan(&cnt))
	require.Equal(t, 1, cnt)
}

func TestFTS_RanksAndFilters(t *testing.T) {
	dsn := testDSN(t)
	ctx := context.Background()
	applyTestSchema(t, dsn)
	s, err := Open(ctx, dsn)
	require.NoError(t, err)
	defer s.Close()
	truncateAll(t, s)

	seedConcept(t, s, "messages/pacs.008.md", "message", "Credit Transfer",
		"pacs008 credit transfer credit transfer message", []string{"pix"}, 1)
	seedConcept(t, s, "messages/pacs.004.md", "message", "Payment Return",
		"payment return reversal message", []string{"pix"}, 1)
	seedConcept(t, s, "api/endpoint.md", "api", "Credit Endpoint",
		"credit api endpoint", []string{"api"}, 1)

	// Plain FTS: only docs matching "credit" come back, ranked.
	hits, err := s.FTS(ctx, "credit", Filter{Limit: 10})
	require.NoError(t, err)
	require.Len(t, hits, 2)
	ids := []string{hits[0].ID, hits[1].ID}
	require.Contains(t, ids, "messages/pacs.008.md")
	require.Contains(t, ids, "api/endpoint.md")
	require.Equal(t, 1, hits[0].Rank)
	require.Equal(t, 2, hits[1].Rank)
	require.GreaterOrEqual(t, hits[0].Score, hits[1].Score)

	// Type filter narrows to message rows only.
	hits, err = s.FTS(ctx, "credit", Filter{Type: "message", Limit: 10})
	require.NoError(t, err)
	require.Len(t, hits, 1)
	require.Equal(t, "messages/pacs.008.md", hits[0].ID)

	// Tag filter.
	hits, err = s.FTS(ctx, "message", Filter{Tag: "pix", Limit: 10})
	require.NoError(t, err)
	require.Len(t, hits, 2)
	for _, h := range hits {
		require.Contains(t, []string{"messages/pacs.008.md", "messages/pacs.004.md"}, h.ID)
	}

	// Limit caps the result set.
	hits, err = s.FTS(ctx, "message", Filter{Limit: 1})
	require.NoError(t, err)
	require.Len(t, hits, 1)
}

func TestFTS_BilingualRanksPortuguese(t *testing.T) {
	dsn := testDSN(t)
	ctx := context.Background()
	applyTestSchema(t, dsn)
	s, err := Open(ctx, dsn)
	require.NoError(t, err)
	defer s.Close()
	truncateAll(t, s)

	// A pt concept whose stemmed term ("devolução"→"devoluç") only matches under
	// the portuguese config, plus an en concept that should not outrank it.
	require.NoError(t, s.UpsertConcept(ctx, okf.Concept{
		ID: "messages/pacs.004.md", Type: "message", Title: "Devolução de Pix",
		Body: "devoluções de pix e estorno", Language: "pt",
		ContentSHA: "sha-pt", Epoch: 1,
		Timestamp: time.Date(2026, 6, 22, 10, 0, 0, 0, time.UTC),
	}))
	require.NoError(t, s.UpsertConcept(ctx, okf.Concept{
		ID: "messages/pacs.008.md", Type: "message", Title: "Credit Transfer",
		Body: "english body without the term", Language: "en",
		ContentSHA: "sha-en", Epoch: 1,
		Timestamp: time.Date(2026, 6, 22, 10, 0, 0, 0, time.UTC),
	}))

	// A Portuguese query stems to the same root; the pt concept must rank first.
	hits, err := s.FTS(ctx, "devolução", Filter{Limit: 10})
	require.NoError(t, err)
	require.NotEmpty(t, hits)
	require.Equal(t, "messages/pacs.004.md", hits[0].ID)
	require.Greater(t, hits[0].Score, 0.0)
}

func TestFTS_AsOfNarrowsResults(t *testing.T) {
	dsn := testDSN(t)
	ctx := context.Background()
	applyTestSchema(t, dsn)
	s, err := Open(ctx, dsn)
	require.NoError(t, err)
	defer s.Close()
	truncateAll(t, s)

	seedConcept(t, s, "messages/pacs.008.md", "message", "Credit Transfer",
		"credit transfer message", []string{"pix"}, 1)
	seedConcept(t, s, "messages/pacs.004.md", "message", "Payment Return",
		"credit return message", []string{"pix"}, 5)

	// Seed concept_fact rows directly (RecordFact is a later task).
	// Each fact has: id, type, title, content_sha, epoch, valid tstzrange, tx tstzrange.
	// tx is open-ended (upper_inf) to simulate the current open transaction.
	const insertFact = `
INSERT INTO concept_fact (id, type, title, content_sha, epoch, valid, tx)
VALUES ($1, $2, $3, $4, $5,
        tstzrange($6::timestamptz, 'infinity'),
        tstzrange($7::timestamptz, 'infinity'))`

	t1 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	_, err = s.pool.Exec(ctx, insertFact,
		"messages/pacs.008.md", "message", "Credit Transfer", "sha-008", 1, t1, t1)
	require.NoError(t, err)
	_, err = s.pool.Exec(ctx, insertFact,
		"messages/pacs.004.md", "message", "Payment Return", "sha-004", 5, t1, t1)
	require.NoError(t, err)

	// Without an as-of bound both match "credit".
	all, err := s.FTS(ctx, "credit", Filter{Limit: 10})
	require.NoError(t, err)
	require.Len(t, all, 2)

	// As-of epoch 1 excludes the concept whose fact epoch is 5.
	epoch1 := 1
	narrowed, err := s.FTS(ctx, "credit", Filter{AsOfEpoch: &epoch1, Limit: 10})
	require.NoError(t, err)
	require.Len(t, narrowed, 1)
	require.Equal(t, "messages/pacs.008.md", narrowed[0].ID)
}

// TestFTS_IncludeTypesRestrictsResults proves the new IncludeTypes filter
// narrows FTS to only the listed types. This package's integration tests
// share one Postgres database with no truncation between tests (see
// TestEmbeddingCoverage_CountsAndDetectsModelMix in health_test.go), so this
// test uses a unique-per-run search term — never indexed by any pre-existing
// row — plus unique-per-run concept ids/types, instead of truncating.
func TestFTS_IncludeTypesRestrictsResults(t *testing.T) {
	dsn := testDSN(t)
	ctx := context.Background()
	applyTestSchema(t, dsn)
	s, err := Open(ctx, dsn)
	require.NoError(t, err)
	defer s.Close()

	suffix := time.Now().UnixNano()
	term := fmt.Sprintf("zzzincludetypesterm%d", suffix)
	typeA := fmt.Sprintf("TypeA%d", suffix)
	typeB := fmt.Sprintf("TypeB%d", suffix)
	idA := fmt.Sprintf("include-types-a-%d.md", suffix)
	idB := fmt.Sprintf("include-types-b-%d.md", suffix)

	seedConcept(t, s, idA, typeA, "Include Types A", term, nil, 1)
	seedConcept(t, s, idB, typeB, "Include Types B", term, nil, 1)

	// Without the filter both concepts match the unique term.
	all, err := s.FTS(ctx, term, Filter{Limit: 10})
	require.NoError(t, err)
	require.Len(t, all, 2)

	// IncludeTypes restricts to typeA only.
	narrowed, err := s.FTS(ctx, term, Filter{IncludeTypes: []string{typeA}, Limit: 10})
	require.NoError(t, err)
	require.Len(t, narrowed, 1)
	assert.Equal(t, idA, narrowed[0].ID)
}

// TestFTS_ExcludeIDsExcludesSpecificID proves the new ExcludeIDs filter drops
// a specific concept id from FTS results while leaving another concept
// matching the same term untouched. Same unique-per-run, no-truncate
// discipline as TestFTS_IncludeTypesRestrictsResults.
func TestFTS_ExcludeIDsExcludesSpecificID(t *testing.T) {
	dsn := testDSN(t)
	ctx := context.Background()
	applyTestSchema(t, dsn)
	s, err := Open(ctx, dsn)
	require.NoError(t, err)
	defer s.Close()

	suffix := time.Now().UnixNano()
	term := fmt.Sprintf("zzzexcludeidsterm%d", suffix)
	typ := fmt.Sprintf("ExcludeType%d", suffix)
	idA := fmt.Sprintf("exclude-ids-a-%d.md", suffix)
	idB := fmt.Sprintf("exclude-ids-b-%d.md", suffix)

	seedConcept(t, s, idA, typ, "Exclude Ids A", term, nil, 1)
	seedConcept(t, s, idB, typ, "Exclude Ids B", term, nil, 1)

	all, err := s.FTS(ctx, term, Filter{Limit: 10})
	require.NoError(t, err)
	require.Len(t, all, 2)

	narrowed, err := s.FTS(ctx, term, Filter{ExcludeIDs: []string{idA}, Limit: 10})
	require.NoError(t, err)
	require.Len(t, narrowed, 1)
	assert.Equal(t, idB, narrowed[0].ID)
}

// TestVector_MinVecScoreDropsLowScoringHit proves the new MinVecScore filter
// drops a vector hit whose cosine score falls below the threshold while
// keeping a hit at/above it. IncludeTypes isolates this test's own two
// concepts (a unique-per-run synthetic type) from every other embedding row
// in this package's shared-uncleaned database, so pollution cannot crowd the
// pair out of the exact-kNN scan.
func TestVector_MinVecScoreDropsLowScoringHit(t *testing.T) {
	dsn := testDSN(t)
	ctx := context.Background()
	applyTestSchema(t, dsn)
	s, err := Open(ctx, dsn)
	require.NoError(t, err)
	defer s.Close()
	emb := embed.NewHashing(256)

	suffix := time.Now().UnixNano()
	typ := fmt.Sprintf("MinVecScoreType%d", suffix)
	highID := fmt.Sprintf("min-vec-score-high-%d.md", suffix)
	lowID := fmt.Sprintf("min-vec-score-low-%d.md", suffix)

	seed := func(id, body string) []float32 {
		c := okf.Concept{
			ID: id, Type: typ, Title: id, Body: body,
			ContentSHA: okf.ComputeSHA(body), Language: "en", Epoch: 0,
		}
		require.NoError(t, s.UpsertConcept(ctx, c))
		vs, err := emb.Embed(ctx, []string{body})
		require.NoError(t, err)
		require.NoError(t, s.UpsertEmbedding(ctx, id, 0, emb.Name(), vs[0], time.Now().UTC()))
		return vs[0]
	}

	queryVec := seed(highID, fmt.Sprintf("alpha bravo charlie %d", suffix))
	seed(lowID, fmt.Sprintf("delta echo foxtrot golf hotel %d", suffix+1))

	filt := Filter{IncludeTypes: []string{typ}, Limit: 10}
	both, err := s.Vector(ctx, queryVec, filt)
	require.NoError(t, err)
	require.Len(t, both, 2, "both concepts of the isolated type must be visible before any score filter")

	var highScore, lowScore float64
	for _, h := range both {
		switch h.ID {
		case highID:
			highScore = h.Score
		case lowID:
			lowScore = h.Score
		}
	}
	require.Greater(t, highScore, lowScore, "the self-vocabulary hit must score strictly higher than the disjoint-vocabulary hit")

	filt.MinVecScore = (highScore + lowScore) / 2
	narrowed, err := s.Vector(ctx, queryVec, filt)
	require.NoError(t, err)
	require.Len(t, narrowed, 1)
	assert.Equal(t, highID, narrowed[0].ID)
	assert.Equal(t, 1, narrowed[0].Rank)
}
