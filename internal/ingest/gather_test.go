package ingest

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"pixkb/internal/okf"
)

type stubSource struct {
	name string
	cs   []okf.Concept
	err  error
}

func (s stubSource) Name() string { return s.name }
func (s stubSource) Fetch(_ context.Context) ([]okf.Concept, error) {
	return s.cs, s.err
}

func TestGatherAll_MergesSorted(t *testing.T) {
	t.Parallel()
	out, err := GatherAll(context.Background(), []Source{
		stubSource{name: "b", cs: []okf.Concept{{ID: "z.md"}, {ID: "a.md"}}},
		stubSource{name: "c", cs: []okf.Concept{{ID: "m.md"}}},
	})
	require.NoError(t, err)
	require.Len(t, out, 3)
	assert.Equal(t, "a.md", out[0].ID)
	assert.Equal(t, "m.md", out[1].ID)
	assert.Equal(t, "z.md", out[2].ID)
}

func TestGatherAll_DuplicateIDErrors(t *testing.T) {
	t.Parallel()
	_, err := GatherAll(context.Background(), []Source{
		stubSource{name: "one", cs: []okf.Concept{{ID: "dup.md"}}},
		stubSource{name: "two", cs: []okf.Concept{{ID: "dup.md"}}},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "duplicate concept id")
}

func TestGatherAll_SourceErrorPropagates(t *testing.T) {
	t.Parallel()
	sentinel := errors.New("boom")
	_, err := GatherAll(context.Background(), []Source{
		stubSource{name: "bad", err: sentinel},
	})
	require.Error(t, err)
	assert.True(t, errors.Is(err, sentinel))
}

func TestTagDomain(t *testing.T) {
	in := []okf.Concept{
		{ID: "a", Tags: []string{"manual", "ii-manual"}},          // no domain -> default pix
		{ID: "b", Tags: []string{"api", "tributos", "domain:tax"}}, // already tagged -> kept
		{ID: "c", Tags: nil},                                       // nil tags -> pix
	}
	out := tagDomain(in)

	assert.Contains(t, out[0].Tags, "domain:pix")
	assert.Contains(t, out[1].Tags, "domain:tax")
	assert.NotContains(t, out[1].Tags, "domain:pix", "must not double-tag an already-domained concept")
	assert.Contains(t, out[2].Tags, "domain:pix")

	// The column must agree with the tag: derived from domain:* (prefix stripped),
	// defaulting to "pix" when a source tagged no domain.
	assert.Equal(t, "pix", out[0].Domain, "untagged concept -> pix column")
	assert.Equal(t, "tax", out[1].Domain, "domain:tax concept -> tax column")
	assert.Equal(t, "pix", out[2].Domain, "nil-tag concept -> pix column")

	// Idempotent: a second pass adds nothing and leaves the column stable.
	again := tagDomain(out)
	for i := range again {
		n := 0
		for _, tg := range again[i].Tags {
			if strings.HasPrefix(tg, "domain:") {
				n++
			}
		}
		assert.Equalf(t, 1, n, "concept %s must have exactly one domain tag", again[i].ID)
	}
	assert.Equal(t, "pix", again[0].Domain)
	assert.Equal(t, "tax", again[1].Domain)
	assert.Equal(t, "pix", again[2].Domain)
}
