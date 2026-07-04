// Package econindex fetches and validates BACEN economic-indicator time
// series (SELIC rates, USD/BRL PTAX) from the public SGS API.
package econindex

import (
	"fmt"
	"regexp"
	"time"
)

var (
	seriesCodePattern = regexp.MustCompile(`^\d+$`)
	valuePattern      = regexp.MustCompile(`^-?\d+(\.\d+)?$`)
)

// SeriesConfig identifies one BACEN SGS series and how to fetch it.
type SeriesConfig struct {
	Name          string        // short key, e.g. "selic-diaria"
	Code          string        // SGS numeric series code, e.g. "11"
	Description   string        // human label, e.g. "Taxa Selic diária"
	BaseURL       string        // "%s"-templated with Code, e.g. "https://api.bcb.gov.br/dados/serie/bcdata.sgs.%s"
	HTTPTimeout   time.Duration
	MaxRangeYears int // pagination window for FetchHistory; BCB caps daily series at 10y/request
}

// DefaultSeriesConfigs returns the three known BACEN indicator series, keyed
// by short name. Entries are also matched by numeric Code via FindSeriesConfig.
func DefaultSeriesConfigs() map[string]SeriesConfig {
	const base = "https://api.bcb.gov.br/dados/serie/bcdata.sgs.%s"
	mk := func(name, code, desc string) SeriesConfig {
		return SeriesConfig{
			Name: name, Code: code, Description: desc,
			BaseURL: base, HTTPTimeout: 30 * time.Second, MaxRangeYears: 10,
		}
	}
	return map[string]SeriesConfig{
		"selic-diaria":   mk("selic-diaria", "11", "Taxa Selic diária"),
		"selic-meta":     mk("selic-meta", "432", "Taxa Selic meta (Copom)"),
		"usd-ptax-venda": mk("usd-ptax-venda", "1", "Dólar comercial, PTAX venda"),
	}
}

// FindSeriesConfig looks up a SeriesConfig by short Name or numeric Code.
func FindSeriesConfig(key string) (SeriesConfig, error) {
	for _, cfg := range DefaultSeriesConfigs() {
		if cfg.Name == key || cfg.Code == key {
			return cfg, nil
		}
	}
	return SeriesConfig{}, fmt.Errorf("unknown econindex series %q", key)
}

// SeriesPoint is one dated value of a BACEN SGS series.
type SeriesPoint struct {
	SeriesCode string    // SGS numeric code, e.g. "11"
	Date       time.Time // UTC, truncated to day
	Value      string    // decimal value, verbatim from BCB (dot-separated), e.g. "0.052531"
	SyncedAt   time.Time
}

// Validate checks that a SeriesPoint has a numeric SeriesCode, a non-zero
// Date, and a Value shaped like a decimal number.
func (p SeriesPoint) Validate() error {
	if !seriesCodePattern.MatchString(p.SeriesCode) {
		return fmt.Errorf("invalid series code %q: must be numeric", p.SeriesCode)
	}
	if p.Date.IsZero() {
		return fmt.Errorf("series point for code %s: missing date", p.SeriesCode)
	}
	if !valuePattern.MatchString(p.Value) {
		return fmt.Errorf("invalid value %q for series %s: not a decimal number", p.Value, p.SeriesCode)
	}
	return nil
}
