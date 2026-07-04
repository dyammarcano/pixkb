package similar

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"pixkb/internal/embed"
	"pixkb/internal/store/postgres"
)

// queryAwareStore differs its FTS result by the exact query string it
// receives — needed to prove MoreLikeThis builds its synthetic query from
// the concept's own fields rather than passing the concept id through as-is.
type queryAwareStore struct {
	fts map[string][]postgres.Hit
}

func (q *queryAwareStore) FTS(_ context.Context, query string, _ postgres.Filter) ([]postgres.Hit, error) {
	return q.fts[query], nil
}
func (q *queryAwareStore) Vector(_ context.Context, _ []float32, _ postgres.Filter) ([]postgres.Hit, error) {
	return nil, nil
}
func (q *queryAwareStore) GetEmbedding(_ context.Context, _ string) ([]float32, error) {
	return nil, nil
}
func (q *queryAwareStore) Related(_ context.Context, _ string) ([]postgres.RelatedConcept, error) {
	return nil, nil
}

func writeTestConcept(t *testing.T, dir, id, title, intentTerms, body string) {
	t.Helper()
	full := filepath.Join(dir, id)
	require.NoError(t, os.MkdirAll(filepath.Dir(full), 0o755))
	fm := "---\ntype: Reference\ntitle: " + title + "\n"
	if intentTerms != "" {
		fm += "intent_terms: " + intentTerms + "\n"
	}
	fm += "---\n"
	require.NoError(t, os.WriteFile(full, []byte(fm+body), 0o644))
}

func TestMoreLikeThis_BuildsSyntheticQueryFromConceptFields(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeTestConcept(t, dir, "a.md", "Refund Concept", "estorno devolucao", "Explains how Pix refunds work end to end.")

	wantQuery := "Refund Concept estorno devolucao Explains how Pix refunds work end to end."
	s := &queryAwareStore{fts: map[string][]postgres.Hit{
		wantQuery: {
			{ID: "a.md", Title: "Refund Concept"}, // self — must be excluded
			{ID: "b.md", Title: "Related Endpoint", Arm: "fts"},
		},
	}}
	got, err := MoreLikeThis(context.Background(), s, embed.NewHashing(8), dir, "a.md", postgres.Filter{})
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "b.md", got[0].ID)
	assert.Equal(t, []string{SignalLexical}, got[0].Why, "Arm=fts must map to lexical")
}

func TestMoreLikeThis_MapsArmToWhy(t *testing.T) {
	t.Parallel()
	cases := []struct {
		arm  string
		want []string
	}{
		{"fts", []string{SignalLexical}},
		{"vector", []string{SignalSemantic}},
		{"both", []string{SignalSemantic, SignalLexical}},
		{"", nil},
	}
	for _, c := range cases {
		assert.Equal(t, c.want, whyFromArm(c.arm), "arm=%q", c.arm)
	}
}

func TestMoreLikeThis_ReadConceptErrorPropagates(t *testing.T) {
	t.Parallel()
	s := &queryAwareStore{}
	_, err := MoreLikeThis(context.Background(), s, embed.NewHashing(8), t.TempDir(), "does-not-exist.md", postgres.Filter{})
	require.Error(t, err)
}

func TestTruncate_DoesNotSplitMultiByteRune(t *testing.T) {
	t.Parallel()
	// Construct a body where naive byte-500 truncation would land mid-rune:
	// one ASCII byte, then enough 2-byte "ç" runes to push the boundary
	// into the middle of one. With 1 ASCII byte + 250 "ç" runes = 1 + 500 bytes = 501.
	// The truncate at 500 bytes would cut right in the middle of the 250th "ç".
	body := "x" + strings.Repeat("ç", 250)
	got := truncate(body, mltBodyChars)
	assert.True(t, utf8.ValidString(got), "truncated body must be valid UTF-8, got %q", got)
	assert.LessOrEqual(t, len(got), mltBodyChars)
}
