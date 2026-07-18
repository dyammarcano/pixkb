package evalkit

import (
	"testing"

	"github.com/stretchr/testify/require"

	"pixkb/internal/query"
	"pixkb/internal/store/postgres"
)

func TestCheckExplainConsistency(t *testing.T) {
	t.Run("consistent -> no issues", func(t *testing.T) {
		hits := []postgres.Hit{{ID: "a", Score: 0.9}, {ID: "b", Score: 0.4}}
		ex := []query.Explain{{FinalScore: 0.9}, {FinalScore: 0.4}}
		require.Empty(t, checkExplainConsistency("q", hits, ex))
	})

	t.Run("length mismatch -> one issue", func(t *testing.T) {
		got := checkExplainConsistency("q", []postgres.Hit{{ID: "a"}}, nil)
		require.Len(t, got, 1)
		require.Contains(t, got[0].Detail, "len(hits)=1")
	})

	t.Run("score disagreement -> issue", func(t *testing.T) {
		hits := []postgres.Hit{{ID: "a", Score: 0.9}}
		ex := []query.Explain{{FinalScore: 0.8}}
		got := checkExplainConsistency("q", hits, ex)
		require.Len(t, got, 1)
		require.Contains(t, got[0].Detail, "!=")
	})

	t.Run("rank order violated -> issue", func(t *testing.T) {
		// hit scores agree, but FinalScore rises at rank 2 (0.4 -> 0.7).
		hits := []postgres.Hit{{ID: "a", Score: 0.4}, {ID: "b", Score: 0.7}}
		ex := []query.Explain{{FinalScore: 0.4}, {FinalScore: 0.7}}
		got := checkExplainConsistency("q", hits, ex)
		require.NotEmpty(t, got)
		require.Contains(t, got[len(got)-1].Detail, "rank order violated")
	})
}

func TestCheckAsOfInvariant(t *testing.T) {
	same := []postgres.Hit{{ID: "a"}, {ID: "b"}}

	require.Empty(t, checkAsOfInvariant("q", same, []postgres.Hit{{ID: "a"}, {ID: "b"}}))

	got := checkAsOfInvariant("q", same, []postgres.Hit{{ID: "a"}})
	require.Len(t, got, 1)
	require.Contains(t, got[0].Detail, "len(unfiltered)=2")

	got = checkAsOfInvariant("q", same, []postgres.Hit{{ID: "a"}, {ID: "X"}})
	require.Len(t, got, 1) // at most one issue per case (first divergence)
	require.Contains(t, got[0].Detail, "position 1")
}
