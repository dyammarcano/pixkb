package econindex

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// sgsPoint is the raw shape of one element of a BACEN SGS JSON response.
type sgsPoint struct {
	Date  string `json:"data"`
	Value string `json:"valor"`
}

// ParseSeries decodes a BACEN SGS JSON array response into SeriesPoints.
// seriesCode is stamped from the caller (the SGS response itself carries no
// series identifier). syncedAt is stamped on every returned record. Rows
// with a malformed date or value are skipped rather than failing the whole
// parse.
func ParseSeries(data []byte, seriesCode string, syncedAt time.Time) ([]SeriesPoint, error) {
	var raw []sgsPoint
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parse sgs json: %w", err)
	}
	if len(raw) == 0 {
		return nil, fmt.Errorf("sgs response has no data points")
	}

	out := make([]SeriesPoint, 0, len(raw))
	for _, r := range raw {
		d, err := time.Parse("02/01/2006", strings.TrimSpace(r.Date))
		if err != nil {
			continue
		}
		p := SeriesPoint{
			SeriesCode: seriesCode,
			Date:       d,
			Value:      strings.TrimSpace(r.Value),
			SyncedAt:   syncedAt,
		}
		if p.Validate() != nil {
			continue
		}
		out = append(out, p)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("sgs response had %d points, none valid", len(raw))
	}
	return out, nil
}
