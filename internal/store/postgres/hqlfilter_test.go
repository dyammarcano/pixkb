package postgres

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"pixkb/internal/embed"
	"pixkb/internal/okf"
)

// TestFTS_HQLWhereNarrowsResults proves Filter.HQLWhere composes an
// AND-ed predicate into FTS, placed after the existing predicates and
// numbered contiguously from len(args) — the exact symptom of a
// placeholder-numbering bug is a pgx "wrong number of parameters" (or
// "could not determine data type") error, so its absence is the key
// assertion here.
func TestFTS_HQLWhereNarrowsResults(t *testing.T) {
	dsn := testDSN(t)
	ctx := context.Background()
	applyTestSchema(t, dsn)
	s, err := Open(ctx, dsn)
	require.NoError(t, err)
	defer s.Close()

	suffix := time.Now().UnixNano()
	term := fmt.Sprintf("zzzhqlfilterterm%d", suffix)
	legalID := fmt.Sprintf("hql-legal-%d.md", suffix)
	apiID := fmt.Sprintf("hql-api-%d.md", suffix)
	manualID := fmt.Sprintf("hql-manual-%d.md", suffix)

	seedConcept(t, s, legalID, "LegalArticle", "Legal Article", term+" body text", []string{"domain:tax"}, 1)
	seedConcept(t, s, apiID, "ApiEndpoint", "Api Endpoint", term+" body text", []string{"domain:tax"}, 1)
	seedConcept(t, s, manualID, "ManualSection", "Manual Section", term+" body text", []string{"domain:pix"}, 1)

	// Without the HQL predicate, all three match the unique term.
	all, err := s.FTS(ctx, term, Filter{Limit: 10})
	require.NoError(t, err)
	require.Len(t, all, 3)

	// With an HQLWhere closure narrowing to LegalArticle only.
	f := Filter{Limit: 10, HQLWhere: func(start int) (string, []any, error) {
		return fmt.Sprintf("type = $%d", start+1), []any{"LegalArticle"}, nil
	}}
	hits, err := s.FTS(ctx, term, f)
	require.NoError(t, err, "must not be a pgx placeholder-numbering error")
	require.Len(t, hits, 1)
	assert.Equal(t, legalID, hits[0].ID)
}

// TestFTS_HQLWhereErrorPropagates proves a closure returning an error
// aborts FTS with that error rather than running an unfiltered search
// (fail closed).
func TestFTS_HQLWhereErrorPropagates(t *testing.T) {
	dsn := testDSN(t)
	ctx := context.Background()
	applyTestSchema(t, dsn)
	s, err := Open(ctx, dsn)
	require.NoError(t, err)
	defer s.Close()

	suffix := time.Now().UnixNano()
	term := fmt.Sprintf("zzzhqlerrterm%d", suffix)
	id := fmt.Sprintf("hql-err-%d.md", suffix)
	seedConcept(t, s, id, "LegalArticle", "Legal Article", term+" body text", nil, 1)

	wantErr := errors.New("bad hql predicate")
	f := Filter{Limit: 10, HQLWhere: func(start int) (string, []any, error) {
		return "", nil, wantErr
	}}
	hits, err := s.FTS(ctx, term, f)
	require.Error(t, err)
	assert.ErrorIs(t, err, wantErr)
	assert.Nil(t, hits)
}

// TestFTS_HQLWhereNilLeavesResultsUnchanged proves Filter{HQLWhere: nil}
// takes the same, unchanged path as before this feature existed.
func TestFTS_HQLWhereNilLeavesResultsUnchanged(t *testing.T) {
	dsn := testDSN(t)
	ctx := context.Background()
	applyTestSchema(t, dsn)
	s, err := Open(ctx, dsn)
	require.NoError(t, err)
	defer s.Close()

	suffix := time.Now().UnixNano()
	term := fmt.Sprintf("zzzhqlnilterm%d", suffix)
	id := fmt.Sprintf("hql-nil-%d.md", suffix)
	seedConcept(t, s, id, "LegalArticle", "Legal Article", term+" body text", nil, 1)

	withoutField, err := s.FTS(ctx, term, Filter{Limit: 10})
	require.NoError(t, err)
	withNilField, err := s.FTS(ctx, term, Filter{Limit: 10, HQLWhere: nil})
	require.NoError(t, err)
	assert.Equal(t, withoutField, withNilField)
}

// TestVector_HQLWhereNarrowsResults mirrors TestFTS_HQLWhereNarrowsResults
// for the Vector arm.
func TestVector_HQLWhereNarrowsResults(t *testing.T) {
	dsn := testDSN(t)
	ctx := context.Background()
	applyTestSchema(t, dsn)
	s, err := Open(ctx, dsn)
	require.NoError(t, err)
	defer s.Close()
	emb := embed.NewHashing(256)

	// Unique-per-run type names, same isolation discipline as
	// TestVector_MinVecScoreDropsLowScoringHit: this package's integration
	// tests share one uncleaned Postgres database, so a literal type like
	// "LegalArticle" would also match rows left behind by earlier runs.
	suffix := time.Now().UnixNano()
	body := fmt.Sprintf("pix common vocabulary term %d", suffix)
	typeLegal := fmt.Sprintf("HQLVecLegal%d", suffix)
	typeAPI := fmt.Sprintf("HQLVecAPI%d", suffix)
	typeManual := fmt.Sprintf("HQLVecManual%d", suffix)
	legalID := fmt.Sprintf("hql-vec-legal-%d.md", suffix)
	apiID := fmt.Sprintf("hql-vec-api-%d.md", suffix)
	manualID := fmt.Sprintf("hql-vec-manual-%d.md", suffix)

	seed := func(id, typ, tag string) []float32 {
		c := okf.Concept{
			ID: id, Type: typ, Title: id, Body: body, Tags: []string{tag},
			ContentSHA: okf.ComputeSHA(id), Language: "en", Epoch: 0,
		}
		require.NoError(t, s.UpsertConcept(ctx, c))
		vs, err := emb.Embed(ctx, []string{body})
		require.NoError(t, err)
		require.NoError(t, s.UpsertEmbedding(ctx, id, 0, emb.Name(), vs[0], time.Now().UTC()))
		return vs[0]
	}

	queryVec := seed(legalID, typeLegal, "domain:tax")
	seed(apiID, typeAPI, "domain:tax")
	seed(manualID, typeManual, "domain:pix")

	f := Filter{Limit: 10, IncludeTypes: []string{typeLegal, typeAPI, typeManual}, HQLWhere: func(start int) (string, []any, error) {
		return fmt.Sprintf("c.type = $%d", start+1), []any{typeLegal}, nil
	}}
	hits, err := s.Vector(ctx, queryVec, f)
	require.NoError(t, err, "must not be a pgx placeholder-numbering error")
	require.Len(t, hits, 1)
	assert.Equal(t, legalID, hits[0].ID)
}

// TestVector_HQLWhereErrorPropagates mirrors TestFTS_HQLWhereErrorPropagates
// for the Vector arm.
func TestVector_HQLWhereErrorPropagates(t *testing.T) {
	dsn := testDSN(t)
	ctx := context.Background()
	applyTestSchema(t, dsn)
	s, err := Open(ctx, dsn)
	require.NoError(t, err)
	defer s.Close()
	emb := embed.NewHashing(256)

	suffix := time.Now().UnixNano()
	body := fmt.Sprintf("pix common vocabulary term err %d", suffix)
	id := fmt.Sprintf("hql-vec-err-%d.md", suffix)
	c := okf.Concept{
		ID: id, Type: "LegalArticle", Title: id, Body: body,
		ContentSHA: okf.ComputeSHA(id), Language: "en", Epoch: 0,
	}
	require.NoError(t, s.UpsertConcept(ctx, c))
	vs, err := emb.Embed(ctx, []string{body})
	require.NoError(t, err)
	require.NoError(t, s.UpsertEmbedding(ctx, id, 0, emb.Name(), vs[0], time.Now().UTC()))

	wantErr := errors.New("bad hql predicate")
	f := Filter{Limit: 10, HQLWhere: func(start int) (string, []any, error) {
		return "", nil, wantErr
	}}
	hits, err := s.Vector(ctx, vs[0], f)
	require.Error(t, err)
	assert.ErrorIs(t, err, wantErr)
	assert.Nil(t, hits)
}
