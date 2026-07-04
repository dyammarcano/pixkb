package econindex

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestSeriesPoint_Validate(t *testing.T) {
	valid := time.Date(2026, 7, 3, 0, 0, 0, 0, time.UTC)
	tests := []struct {
		name    string
		point   SeriesPoint
		wantErr bool
	}{
		{name: "valid decimal", point: SeriesPoint{SeriesCode: "11", Date: valid, Value: "0.052531"}, wantErr: false},
		{name: "valid integer value", point: SeriesPoint{SeriesCode: "1", Date: valid, Value: "5"}, wantErr: false},
		{name: "non-numeric code", point: SeriesPoint{SeriesCode: "abc", Date: valid, Value: "1.0"}, wantErr: true},
		{name: "zero date", point: SeriesPoint{SeriesCode: "11", Value: "1.0"}, wantErr: true},
		{name: "comma decimal rejected", point: SeriesPoint{SeriesCode: "11", Date: valid, Value: "1,25"}, wantErr: true},
		{name: "empty value", point: SeriesPoint{SeriesCode: "11", Date: valid, Value: ""}, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.point.Validate()
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestDefaultSeriesConfigs(t *testing.T) {
	cfgs := DefaultSeriesConfigs()
	assert.Len(t, cfgs, 3)
	assert.Equal(t, "11", cfgs["selic-diaria"].Code)
	assert.Equal(t, "432", cfgs["selic-meta"].Code)
	assert.Equal(t, "1", cfgs["usd-ptax-venda"].Code)
}

func TestFindSeriesConfig(t *testing.T) {
	byName, err := FindSeriesConfig("selic-diaria")
	assert.NoError(t, err)
	assert.Equal(t, "11", byName.Code)

	byCode, err := FindSeriesConfig("432")
	assert.NoError(t, err)
	assert.Equal(t, "selic-meta", byCode.Name)

	_, err = FindSeriesConfig("nope")
	assert.Error(t, err)
}
