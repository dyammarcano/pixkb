package ingest

import (
	"context"
	"errors"
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
