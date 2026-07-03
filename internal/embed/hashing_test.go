package embed

import (
	"context"
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewHashing_NameAndDim(t *testing.T) {
	e := NewHashing(0) // 0 => default 256
	assert.Equal(t, "hashing", e.Name())
	assert.Equal(t, 256, e.Dim())

	e2 := NewHashing(128)
	assert.Equal(t, 128, e2.Dim())
}

func TestNewHashing_Deterministic(t *testing.T) {
	e := NewHashing(64)
	a, err := e.Embed(context.Background(), []string{"PACS.008 Devolução"})
	require.NoError(t, err)
	b, err := e.Embed(context.Background(), []string{"pacs.008 devolução"})
	require.NoError(t, err)
	require.Len(t, a, 1)
	require.Len(t, b, 1)
	require.Len(t, a[0], 64)
	// lowercase-insensitive => identical vectors
	assert.Equal(t, a[0], b[0])
}

func TestNewHashing_L2Normalized(t *testing.T) {
	e := NewHashing(64)
	out, err := e.Embed(context.Background(), []string{"camt.056 cancelamento"})
	require.NoError(t, err)
	require.Len(t, out, 1)
	var sum float64
	for _, v := range out[0] {
		sum += float64(v) * float64(v)
	}
	assert.InDelta(t, 1.0, math.Sqrt(sum), 1e-6)
}

func TestNewHashing_EmptyText(t *testing.T) {
	e := NewHashing(32)
	out, err := e.Embed(context.Background(), []string{""})
	require.NoError(t, err)
	require.Len(t, out, 1)
	require.Len(t, out[0], 32)
	for _, v := range out[0] {
		assert.Equal(t, float32(0), v)
	}
}

func TestNewHashing_Batch(t *testing.T) {
	e := NewHashing(32)
	out, err := e.Embed(context.Background(), []string{"pix", "spi", "dict"})
	require.NoError(t, err)
	assert.Len(t, out, 3)
}
