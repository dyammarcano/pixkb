package postgres

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"pixkb/internal/okf"
)

func TestStats_Counts(t *testing.T) {
	dsn := testDSN(t)
	ctx := context.Background()
	applyTestSchema(t, dsn)
	s, err := Open(ctx, dsn)
	require.NoError(t, err)
	defer s.Close()
	truncateAll(t, s)

	for _, c := range []okf.Concept{
		{ID: "messages/pacs.008.md", Type: "PacsMessage", Title: "Credit Transfer", ContentSHA: "a"},
		{ID: "messages/pacs.004.md", Type: "PacsMessage", Title: "Payment Return", ContentSHA: "b"},
		{ID: "manuals/m/secao-0.md", Type: "ManualSection", Title: "Intro", ContentSHA: "c"},
	} {
		require.NoError(t, s.UpsertConcept(ctx, c))
	}

	st, err := s.Stats(ctx)
	require.NoError(t, err)
	require.Equal(t, 3, st.Concepts)
	require.Equal(t, 2, st.ByType["PacsMessage"])
	require.Equal(t, 1, st.ByType["ManualSection"])
	// PacsMessage (2) must sort before ManualSection (1) by count desc.
	require.Equal(t, "PacsMessage", st.TypeOrder[0])
}
