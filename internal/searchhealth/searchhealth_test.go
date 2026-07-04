package searchhealth

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRecommend_EvalRegressionOutranksSingleWeakSignal(t *testing.T) {
	signals := []Signal{
		{ConceptID: "weak.md", Kind: KindSparseTerms, Detail: "no intent_terms"},
		{ConceptID: "broken.md", Kind: KindEvalRegression, Detail: "query found no acceptable hit"},
	}
	recs := Recommend(signals)
	require.Len(t, recs, 2)
	assert.Equal(t, "broken.md", recs[0].ConceptID, "an eval regression must outrank a single weak signal")
	assert.Equal(t, "weak.md", recs[1].ConceptID)
}

func TestRecommend_MultipleWeakSignalsOutrankSingleWeakSignal(t *testing.T) {
	signals := []Signal{
		{ConceptID: "single.md", Kind: KindSparseTerms, Detail: "no intent_terms"},
		{ConceptID: "double.md", Kind: KindSparseTerms, Detail: "no intent_terms"},
		{ConceptID: "double.md", Kind: KindNoisyTitle, Detail: "junk title"},
	}
	recs := Recommend(signals)
	require.Len(t, recs, 2)
	assert.Equal(t, "double.md", recs[0].ConceptID, "two weak signals must outrank one")
	assert.Equal(t, 2, recs[0].Score)
	assert.Equal(t, "single.md", recs[1].ConceptID)
	assert.Equal(t, 1, recs[1].Score)
}

func TestRecommend_TiesBrokenByConceptIDForDeterminism(t *testing.T) {
	signals := []Signal{
		{ConceptID: "z.md", Kind: KindSparseGraph, Detail: "no graph edges"},
		{ConceptID: "a.md", Kind: KindSparseGraph, Detail: "no graph edges"},
	}
	recs := Recommend(signals)
	require.Len(t, recs, 2)
	assert.Equal(t, "a.md", recs[0].ConceptID, "equal-score ties must break by concept id, not input order")
	assert.Equal(t, "z.md", recs[1].ConceptID)
}

func TestRecommend_EmptyInputReturnsEmpty(t *testing.T) {
	assert.Empty(t, Recommend(nil))
}

func TestRecommend_GroupsAllSignalsForSameConcept(t *testing.T) {
	signals := []Signal{
		{ConceptID: "x.md", Kind: KindSparseTerms, Detail: "a"},
		{ConceptID: "x.md", Kind: KindNoisyTitle, Detail: "b"},
		{ConceptID: "x.md", Kind: KindSparseGraph, Detail: "c"},
	}
	recs := Recommend(signals)
	require.Len(t, recs, 1)
	assert.Len(t, recs[0].Signals, 3, "all signals for the same concept must be grouped into one recommendation")
}
