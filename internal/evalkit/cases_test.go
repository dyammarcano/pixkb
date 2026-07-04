package evalkit

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeFixture(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "cases.tsv")
	require.NoError(t, os.WriteFile(p, []byte(content), 0o644))
	return p
}

func TestLoadPairCases_SkipsCommentsAndBlanks(t *testing.T) {
	p := writeFixture(t, "# comment\n\ncriar cobrança\tapi/openapi/post-cob.md\nconsultar\ta.md,b.md\n")
	cases, err := LoadPairCases(p)
	require.NoError(t, err)
	require.Len(t, cases, 2)
	assert.Equal(t, "criar cobrança", cases[0].Query)
	assert.Equal(t, []string{"api/openapi/post-cob.md"}, cases[0].WantIDs)
	assert.Equal(t, []string{"a.md", "b.md"}, cases[1].WantIDs)
}

func TestLoadSimilarCases_ParsesThreeColumns(t *testing.T) {
	p := writeFixture(t, "# comment\napi/openapi/post-cob.md\thybrid\tapi/openapi/get-cob-txid.md\n")
	cases, err := LoadSimilarCases(p)
	require.NoError(t, err)
	require.Len(t, cases, 1)
	assert.Equal(t, "api/openapi/post-cob.md", cases[0].ConceptID)
	assert.Equal(t, "hybrid", cases[0].Mode)
	assert.Equal(t, []string{"api/openapi/get-cob-txid.md"}, cases[0].WantIDs)
}

func TestLoadQueries_SkipsCommentsAndBlanks(t *testing.T) {
	p := writeFixture(t, "# ood\nqual a previsão do tempo?\n\nreceita de bolo\n")
	qs, err := LoadQueries(p)
	require.NoError(t, err)
	assert.Equal(t, []string{"qual a previsão do tempo?", "receita de bolo"}, qs)
}

func TestForbiddenIDs_UnionsMultipleSets(t *testing.T) {
	setA := []PairCase{{Query: "q1", WantIDs: []string{"a.md", "b.md"}}}
	setB := []PairCase{{Query: "q2", WantIDs: []string{"b.md", "c.md"}}}
	forbidden := ForbiddenIDs(setA, setB)
	assert.True(t, forbidden["a.md"])
	assert.True(t, forbidden["b.md"])
	assert.True(t, forbidden["c.md"])
	assert.Len(t, forbidden, 3)
}
