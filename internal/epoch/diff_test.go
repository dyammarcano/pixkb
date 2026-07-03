package epoch

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDiffSets(t *testing.T) {
	t.Parallel()
	old := map[string]string{"a": "1", "b": "1", "c": "1"}
	cur := map[string]string{"a": "1", "b": "2", "d": "1"}

	d := diffSets(old, cur)
	assert.Equal(t, []string{"d"}, d.Added)     // d is new
	assert.Equal(t, []string{"b"}, d.Changed)   // b sha changed
	assert.Equal(t, []string{"c"}, d.Removed)   // c gone
}

func TestDiffSets_Empty(t *testing.T) {
	t.Parallel()
	d := diffSets(map[string]string{}, map[string]string{})
	assert.Empty(t, d.Added)
	assert.Empty(t, d.Changed)
	assert.Empty(t, d.Removed)
}

func TestDiffSets_AllAdded(t *testing.T) {
	t.Parallel()
	d := diffSets(nil, map[string]string{"x": "1", "y": "1"})
	assert.Equal(t, []string{"x", "y"}, d.Added)
	assert.Empty(t, d.Removed)
}
