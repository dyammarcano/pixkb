package econindex

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const sgsFixture = `[{"data":"02/07/2026","valor":"0.052531"},{"data":"03/07/2026","valor":"0.052531"},{"data":"not-a-date","valor":"1.0"},{"data":"04/07/2026","valor":"not-a-number"}]`

func TestParseSeries(t *testing.T) {
	synced := time.Date(2026, 7, 4, 0, 0, 0, 0, time.UTC)
	points, err := ParseSeries([]byte(sgsFixture), "11", synced)
	require.NoError(t, err)
	require.Len(t, points, 2, "4 rows minus 1 bad date minus 1 bad value")

	first := points[0]
	assert.Equal(t, "11", first.SeriesCode)
	assert.Equal(t, time.Date(2026, 7, 2, 0, 0, 0, 0, time.UTC), first.Date)
	assert.Equal(t, "0.052531", first.Value)
	assert.Equal(t, synced, first.SyncedAt)
}

func TestParseSeries_EmptyArray(t *testing.T) {
	_, err := ParseSeries([]byte(`[]`), "11", time.Now())
	assert.Error(t, err)
}

func TestParseSeries_AllRowsInvalid(t *testing.T) {
	_, err := ParseSeries([]byte(`[{"data":"bad","valor":"bad"}]`), "11", time.Now())
	assert.Error(t, err)
}

func TestParseSeries_MalformedJSON(t *testing.T) {
	_, err := ParseSeries([]byte(`not json`), "11", time.Now())
	assert.Error(t, err)
}
