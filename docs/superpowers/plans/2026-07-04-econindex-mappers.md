# SELIC/Dólar (econindex) Mappers Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add an economic-indicator mapper to `pixkb`: a `pixkb econindex` CLI that downloads, parses, and stores three BACEN SGS time series (Selic daily rate, Selic target/meta rate, USD/BRL PTAX venda), then looks them up by series key/date.

**Architecture:** A pure `internal/econindex` library (no DB import) holds one BACEN SGS client (`client.go`, JSON over HTTP, no auth, no date-probing) and one parser (`parse.go`), shared by all three series since they're one API shape — unlike ISPB's two structurally different sources. `internal/store/postgres/econindex.go` adds upsert/get methods on the existing `*Store`. `cmd/pixkb/econindex.go` wires it into a flat Cobra command group honoring the project's air-gap convention (online fetch → staged file → offline load).

**Tech Stack:** Go 1.25.0, `github.com/spf13/cobra`, `github.com/jackc/pgx/v5`, `github.com/golang-migrate/migrate/v4`, `github.com/stretchr/testify`. No new dependencies — `net/http`/`encoding/json`/`math/big` (stdlib) cover the whole feature.

**Design spec:** `docs/superpowers/specs/2026-07-04-econindex-mappers-design.md`

## Global Constraints

- Module is `pixkb`; Go 1.25.0; `CGO_ENABLED=0` pure-Go only — no cgo dependencies; no new `go.mod` entries.
- **No `--dsn` flag on any new command, anywhere.** DSN is resolved exclusively via `loadConfig()` (reads `pixkb.yaml` then `PIXKB_DSN` env) and `openStore(ctx, cfg)` (`cmd/pixkb/config.go`). This is a hard project rule, verified by an automated test in Task 3.
- **`SeriesPoint.Value` is a `string`, never `float64`** — per this project's Brazilian financial context convention (decimal types, never floats). BCB's SGS JSON already returns dot-decimal strings (confirmed live: `"valor":"0.052531"`); store and pass them through verbatim. `internal/econindex` never does arithmetic on `Value` — only shape-validates it via `regexp`.
- **BCB's SGS API rejects (`HTTP 406`) a `dataInicial`/`dataFinal` span over 10 years on a daily series** — confirmed live during design. `FetchHistory` (Task 1) must page in `cfg.MaxRangeYears`-sized windows; do not attempt a single unbounded-range request.
- This repo is a git repo on branch `master` (`git status` inside `C:\weaver-sync\development\personal\projects\bacen`). Commit at the end of every task with `git add <files>` + `git commit`.
- All tests use `testify` (`require`/`assert`), co-located as `*_test.go` in the same package, matching existing files (e.g. `internal/store/postgres/ispb_test.go`, `cmd/pixkb/ispb_test.go`).
- Postgres integration tests must call `testDSN(t)` (skips under `-short` and when `PIXKB_TEST_DSN` unset — `internal/store/postgres/store_test.go`) and `applyTestSchema(t, dsn)` (`internal/store/postgres/schema_test.go`) before touching the DB. Do not duplicate these helpers.
- Migration files are 4-digit sequential `NNNN_name.up.sql` / `NNNN_name.down.sql` under `internal/store/postgres/schema/`, auto-picked up by the `//go:embed schema/*.sql` in `internal/store/postgres/migrations.go` — no wiring needed beyond adding the files. **Next free number is `0006`** (`0004_ispb_participant`, `0005_unaccent_extension` already exist).
- `cmd/pixkb/commands.go`'s `attachCommands(root)` is the single place new top-level commands get registered — mirrors how `newISPBCmd()` was added there.
- Every exported function needs a doc comment starting with its name (Go convention already used throughout the codebase).

---

### Task 1: `internal/econindex` — SGS client + series parsing

**Files:**
- Create: `internal/econindex/series.go`
- Create: `internal/econindex/client.go`
- Create: `internal/econindex/parse.go`
- Test: `internal/econindex/series_test.go`
- Test: `internal/econindex/client_test.go`
- Test: `internal/econindex/parse_test.go`

**Interfaces:**
- Produces: `econindex.SeriesConfig{Name, Code, Description, BaseURL, HTTPTimeout, MaxRangeYears}`, `econindex.DefaultSeriesConfigs() map[string]SeriesConfig`, `econindex.FindSeriesConfig(key string) (SeriesConfig, error)`, `econindex.SeriesPoint{SeriesCode, Date, Value, SyncedAt}`, `(SeriesPoint) Validate() error`, `econindex.DownloadLatest(ctx, cfg, log *slog.Logger) ([]byte, error)`, `econindex.DownloadRange(ctx, cfg, from, to time.Time, log *slog.Logger) ([]byte, error)`, `econindex.FetchHistory(ctx, cfg, from, to time.Time, log *slog.Logger) ([]SeriesPoint, error)`, `econindex.ParseSeries(data []byte, seriesCode string, syncedAt time.Time) ([]SeriesPoint, error)`.

**Confirmed live during design** (fetched via sandboxed HTTP, not WebFetch): `GET https://api.bcb.gov.br/dados/serie/bcdata.sgs.<codigo>/dados/ultimos/<n>?formato=json` and `GET .../dados?formato=json&dataInicial=DD/MM/YYYY&dataFinal=DD/MM/YYYY` both return a JSON array `[{"data":"DD/MM/YYYY","valor":"<decimal>"}, ...]`, already dot-decimal. A `dataInicial`/`dataFinal` span over 10 years on a daily series returns HTTP 406 with a Portuguese error body.

- [ ] **Step 1: Write the failing tests**

```go
// internal/econindex/series_test.go
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
```

```go
// internal/econindex/parse_test.go
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
```

```go
// internal/econindex/client_test.go
package econindex

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sort"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDownloadLatest(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/series-11/dados/ultimos/1", r.URL.Path)
		assert.Equal(t, "formato=json", r.URL.RawQuery)
		_, _ = w.Write([]byte(`[{"data":"03/07/2026","valor":"0.052531"}]`))
	}))
	defer srv.Close()

	cfg := SeriesConfig{Code: "11", BaseURL: srv.URL + "/series-%s", HTTPTimeout: 5 * time.Second}
	data, err := DownloadLatest(context.Background(), cfg, nil)
	require.NoError(t, err)
	assert.Contains(t, string(data), "0.052531")
}

func TestDownloadRange(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/series-1/dados", r.URL.Path)
		assert.Equal(t, "01/01/2024", r.URL.Query().Get("dataInicial"))
		assert.Equal(t, "31/01/2024", r.URL.Query().Get("dataFinal"))
		_, _ = w.Write([]byte(`[{"data":"02/01/2024","valor":"5.10"}]`))
	}))
	defer srv.Close()

	cfg := SeriesConfig{Code: "1", BaseURL: srv.URL + "/series-%s", HTTPTimeout: 5 * time.Second}
	from := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2024, 1, 31, 0, 0, 0, 0, time.UTC)
	data, err := DownloadRange(context.Background(), cfg, from, to, nil)
	require.NoError(t, err)
	assert.Contains(t, string(data), "5.10")
}

func TestDownloadRange_ErrorStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotAcceptable)
		_, _ = w.Write([]byte(`{"error":"janela de consulta"}`))
	}))
	defer srv.Close()

	cfg := SeriesConfig{Code: "11", BaseURL: srv.URL + "/series-%s", HTTPTimeout: 5 * time.Second}
	_, err := DownloadRange(context.Background(), cfg, time.Now(), time.Now(), nil)
	assert.Error(t, err)
}

func TestFetchHistory_PagesOverTenYears(t *testing.T) {
	var requests []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := r.URL.Query().Get("dataInicial")
		requests = append(requests, start+".."+r.URL.Query().Get("dataFinal"))
		if start == "01/01/2010" {
			_, _ = w.Write([]byte(`[{"data":"01/01/2010","valor":"1.0"},{"data":"31/12/2019","valor":"2.0"}]`))
			return
		}
		_, _ = w.Write([]byte(`[{"data":"01/01/2020","valor":"3.0"},{"data":"01/01/2025","valor":"4.0"}]`))
	}))
	defer srv.Close()

	cfg := SeriesConfig{Code: "11", BaseURL: srv.URL + "/series-%s", HTTPTimeout: 5 * time.Second, MaxRangeYears: 10}
	from := time.Date(2010, 1, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	points, err := FetchHistory(context.Background(), cfg, from, to, nil)
	require.NoError(t, err)
	require.Len(t, requests, 2, "a >10y range must page into exactly 2 windows")
	require.Len(t, points, 4)
	assert.True(t, sort.SliceIsSorted(points, func(i, j int) bool { return points[i].Date.Before(points[j].Date) }))
	assert.Equal(t, "4.0", points[3].Value)
}

func TestFetchHistory_InvalidRange(t *testing.T) {
	cfg := SeriesConfig{Code: "11", BaseURL: "http://unused/%s", HTTPTimeout: time.Second}
	_, err := FetchHistory(context.Background(), cfg, time.Now(), time.Now().AddDate(0, 0, -1), nil)
	assert.Error(t, err)
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/econindex/... -v`
Expected: FAIL — `undefined: SeriesPoint`, `undefined: ParseSeries`, `undefined: DownloadLatest`, etc. (package `internal/econindex` doesn't exist yet)

- [ ] **Step 3: Write the implementation**

```go
// internal/econindex/series.go

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
```

```go
// internal/econindex/parse.go
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
```

```go
// internal/econindex/client.go
package econindex

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sort"
	"time"
)

const sgsDateFormat = "02/01/2006"

// DownloadLatest fetches the single most recent point of a BACEN SGS series.
// log may be nil.
func DownloadLatest(ctx context.Context, cfg SeriesConfig, log *slog.Logger) ([]byte, error) {
	url := fmt.Sprintf(cfg.BaseURL, cfg.Code) + "/dados/ultimos/1?formato=json"
	return fetchURL(ctx, cfg, url, log)
}

// DownloadRange fetches every point of a BACEN SGS series between from and
// to (inclusive, both dates). Callers with a span over cfg.MaxRangeYears
// must page via FetchHistory instead — BCB rejects longer single requests
// with HTTP 406 for daily series (confirmed live during design).
func DownloadRange(ctx context.Context, cfg SeriesConfig, from, to time.Time, log *slog.Logger) ([]byte, error) {
	url := fmt.Sprintf(cfg.BaseURL, cfg.Code) +
		fmt.Sprintf("/dados?formato=json&dataInicial=%s&dataFinal=%s",
			from.Format(sgsDateFormat), to.Format(sgsDateFormat))
	return fetchURL(ctx, cfg, url, log)
}

func fetchURL(ctx context.Context, cfg SeriesConfig, url string, log *slog.Logger) ([]byte, error) {
	if log == nil {
		log = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	client := &http.Client{Timeout: cfg.HTTPTimeout}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch %s: %w", url, err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch %s: status %d: %s", url, resp.StatusCode, body)
	}
	log.Info("econindex: downloaded series", "series", cfg.Code, "url", url, "bytes", len(body))
	return body, nil
}

// FetchHistory downloads and parses every point of a BACEN SGS series
// between from and to, paging the request in cfg.MaxRangeYears-sized windows
// to stay under BCB's per-request range cap (confirmed live: HTTP 406 over
// 10 years on a daily series). Results are merged and returned sorted
// ascending by date; a date present in more than one window (window
// boundaries are inclusive on both ends) keeps its last-fetched value.
func FetchHistory(ctx context.Context, cfg SeriesConfig, from, to time.Time, log *slog.Logger) ([]SeriesPoint, error) {
	if to.Before(from) {
		return nil, fmt.Errorf("invalid range: to (%s) before from (%s)", to.Format(sgsDateFormat), from.Format(sgsDateFormat))
	}
	maxYears := cfg.MaxRangeYears
	if maxYears <= 0 {
		maxYears = 10
	}

	byDate := map[time.Time]SeriesPoint{}
	windowStart := from
	for !windowStart.After(to) {
		windowEnd := windowStart.AddDate(maxYears, 0, -1)
		if windowEnd.After(to) {
			windowEnd = to
		}
		data, err := DownloadRange(ctx, cfg, windowStart, windowEnd, log)
		if err != nil {
			return nil, err
		}
		points, err := ParseSeries(data, cfg.Code, time.Now())
		if err != nil {
			return nil, err
		}
		for _, p := range points {
			byDate[p.Date] = p
		}
		windowStart = windowEnd.AddDate(0, 0, 1)
	}

	out := make([]SeriesPoint, 0, len(byDate))
	for _, p := range byDate {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Date.Before(out[j].Date) })
	return out, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/econindex/... -v`
Expected: PASS (all `TestSeriesPoint_Validate*`, `TestDefaultSeriesConfigs`, `TestFindSeriesConfig`, `TestParseSeries*`, `TestDownloadLatest`, `TestDownloadRange*`, `TestFetchHistory*` subtests)

- [ ] **Step 5: Commit**

```
git add internal/econindex/
git commit -m "feat: add internal/econindex SGS client + series parsing"
```

---

### Task 2: Migration `0006_econindex_series_point` + Postgres store methods

**Files:**
- Create: `internal/store/postgres/schema/0006_econindex_series_point.up.sql`
- Create: `internal/store/postgres/schema/0006_econindex_series_point.down.sql`
- Create: `internal/store/postgres/econindex.go`
- Test: `internal/store/postgres/econindex_test.go`

**Interfaces:**
- Consumes: `econindex.SeriesPoint` (Task 1); table `econindex_series_point` (this task); `s.pool` (existing `*Store` field), `testDSN(t)`/`applyTestSchema(t, dsn)` (existing test helpers).
- Produces: table `econindex_series_point`, `(*Store) UpsertSeriesPoint(ctx, econindex.SeriesPoint) error`, `(*Store) UpsertSeriesPoints(ctx, []econindex.SeriesPoint) error`, `(*Store) GetLatestSeriesPoint(ctx, seriesCode string) (econindex.SeriesPoint, error)`, `(*Store) GetSeriesRange(ctx, seriesCode string, from, to time.Time) ([]econindex.SeriesPoint, error)`, `(*Store) CountSeriesPoints(ctx, seriesCode string) (int, error)`.

Unlike ISPB's STR-wins-over-Pix reconciliation, every `econindex_series_point` row has exactly one writer, so the upsert is an unconditional overwrite keyed on `(series_code, point_date)`.

- [ ] **Step 1: Write the migration files**

```sql
-- internal/store/postgres/schema/0006_econindex_series_point.up.sql
CREATE TABLE IF NOT EXISTS econindex_series_point (
  series_code  TEXT        NOT NULL,
  point_date   DATE        NOT NULL,
  value_text   TEXT        NOT NULL,
  synced_at    TIMESTAMPTZ NOT NULL,
  created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY (series_code, point_date)
);
```

```sql
-- internal/store/postgres/schema/0006_econindex_series_point.down.sql
DROP TABLE IF EXISTS econindex_series_point;
```

- [ ] **Step 2: Verify the migration applies**

Requires a reachable throwaway Postgres (`deploy/local-testdb.sh` / `task testdb:up`) and `PIXKB_TEST_DSN` set.

Run: `PIXKB_DSN=$PIXKB_TEST_DSN go run ./cmd/pixkb db up`
Expected: exits 0, no error. Then `PIXKB_DSN=$PIXKB_TEST_DSN go run ./cmd/pixkb db down` and `db up` again to confirm the down migration is also clean.

- [ ] **Step 3: Write the failing tests**

```go
// internal/store/postgres/econindex_test.go
package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"pixkb/internal/econindex"
)

func truncateEconIndex(t *testing.T, s *Store) {
	t.Helper()
	_, err := s.pool.Exec(context.Background(), "TRUNCATE econindex_series_point")
	require.NoError(t, err)
}

func TestUpsertSeriesPoint_ThenGetLatest(t *testing.T) {
	dsn := testDSN(t)
	ctx := context.Background()
	applyTestSchema(t, dsn)
	s, err := Open(ctx, dsn)
	require.NoError(t, err)
	defer s.Close()
	truncateEconIndex(t, s)

	synced := time.Date(2026, 7, 4, 0, 0, 0, 0, time.UTC)
	p1 := econindex.SeriesPoint{SeriesCode: "11", Date: time.Date(2026, 7, 2, 0, 0, 0, 0, time.UTC), Value: "0.05", SyncedAt: synced}
	p2 := econindex.SeriesPoint{SeriesCode: "11", Date: time.Date(2026, 7, 3, 0, 0, 0, 0, time.UTC), Value: "0.06", SyncedAt: synced}
	require.NoError(t, s.UpsertSeriesPoint(ctx, p1))
	require.NoError(t, s.UpsertSeriesPoint(ctx, p2))

	got, err := s.GetLatestSeriesPoint(ctx, "11")
	require.NoError(t, err)
	assert.True(t, got.Date.Equal(p2.Date))
	assert.Equal(t, "0.06", got.Value)
}

func TestUpsertSeriesPoint_OverwritesOnConflict(t *testing.T) {
	dsn := testDSN(t)
	ctx := context.Background()
	applyTestSchema(t, dsn)
	s, err := Open(ctx, dsn)
	require.NoError(t, err)
	defer s.Close()
	truncateEconIndex(t, s)

	date := time.Date(2026, 7, 2, 0, 0, 0, 0, time.UTC)
	require.NoError(t, s.UpsertSeriesPoint(ctx, econindex.SeriesPoint{SeriesCode: "1", Date: date, Value: "5.10", SyncedAt: time.Now()}))
	require.NoError(t, s.UpsertSeriesPoint(ctx, econindex.SeriesPoint{SeriesCode: "1", Date: date, Value: "5.19", SyncedAt: time.Now()}))

	got, err := s.GetLatestSeriesPoint(ctx, "1")
	require.NoError(t, err)
	assert.Equal(t, "5.19", got.Value, "later upsert of the same date must win")

	n, err := s.CountSeriesPoints(ctx, "1")
	require.NoError(t, err)
	assert.Equal(t, 1, n, "same (series_code, point_date) must not duplicate rows")
}

func TestUpsertSeriesPoints_Batch(t *testing.T) {
	dsn := testDSN(t)
	ctx := context.Background()
	applyTestSchema(t, dsn)
	s, err := Open(ctx, dsn)
	require.NoError(t, err)
	defer s.Close()
	truncateEconIndex(t, s)

	pts := []econindex.SeriesPoint{
		{SeriesCode: "432", Date: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), Value: "14.25", SyncedAt: time.Now()},
		{SeriesCode: "432", Date: time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC), Value: "14.25", SyncedAt: time.Now()},
	}
	require.NoError(t, s.UpsertSeriesPoints(ctx, pts))

	n, err := s.CountSeriesPoints(ctx, "432")
	require.NoError(t, err)
	assert.Equal(t, 2, n)
}

func TestGetSeriesRange(t *testing.T) {
	dsn := testDSN(t)
	ctx := context.Background()
	applyTestSchema(t, dsn)
	s, err := Open(ctx, dsn)
	require.NoError(t, err)
	defer s.Close()
	truncateEconIndex(t, s)

	for _, d := range []int{1, 2, 3, 15} {
		require.NoError(t, s.UpsertSeriesPoint(ctx, econindex.SeriesPoint{
			SeriesCode: "11", Date: time.Date(2026, 7, d, 0, 0, 0, 0, time.UTC), Value: "0.05", SyncedAt: time.Now(),
		}))
	}

	from := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 7, 3, 0, 0, 0, 0, time.UTC)
	list, err := s.GetSeriesRange(ctx, "11", from, to)
	require.NoError(t, err)
	require.Len(t, list, 3, "the 15th falls outside the range")
	assert.True(t, list[0].Date.Equal(from))
}

func TestGetLatestSeriesPoint_NotFound(t *testing.T) {
	dsn := testDSN(t)
	ctx := context.Background()
	applyTestSchema(t, dsn)
	s, err := Open(ctx, dsn)
	require.NoError(t, err)
	defer s.Close()
	truncateEconIndex(t, s)

	_, err = s.GetLatestSeriesPoint(ctx, "999")
	require.Error(t, err)
	assert.True(t, errors.Is(err, pgx.ErrNoRows))
}
```

- [ ] **Step 4: Run tests to verify they fail**

Run: `go test ./internal/store/postgres/... -run 'TestUpsertSeriesPoint|TestGetSeriesRange|TestGetLatestSeriesPoint' -v` (requires `PIXKB_TEST_DSN`; skips cleanly under `-short` or when unset)
Expected: FAIL — `undefined: Store.UpsertSeriesPoint` (compile error) once `PIXKB_TEST_DSN` is set, or SKIP if it's not.

- [ ] **Step 5: Write the implementation**

```go
// internal/store/postgres/econindex.go
package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"pixkb/internal/econindex"
)

// UpsertSeriesPoint inserts or updates a single econindex series point.
func (s *Store) UpsertSeriesPoint(ctx context.Context, p econindex.SeriesPoint) error {
	if err := p.Validate(); err != nil {
		return err
	}
	const q = `
INSERT INTO econindex_series_point (series_code, point_date, value_text, synced_at, updated_at)
VALUES ($1,$2,$3,$4, now())
ON CONFLICT (series_code, point_date) DO UPDATE SET
  value_text = EXCLUDED.value_text,
  synced_at  = EXCLUDED.synced_at,
  updated_at = now()`
	_, err := s.pool.Exec(ctx, q, p.SeriesCode, p.Date, p.Value, p.SyncedAt)
	if err != nil {
		return fmt.Errorf("upsert econindex point %s/%s: %w", p.SeriesCode, p.Date.Format("2006-01-02"), err)
	}
	return nil
}

// UpsertSeriesPoints upserts a batch of econindex series points, stopping at
// the first error.
func (s *Store) UpsertSeriesPoints(ctx context.Context, points []econindex.SeriesPoint) error {
	for _, p := range points {
		if err := s.UpsertSeriesPoint(ctx, p); err != nil {
			return err
		}
	}
	return nil
}

func scanSeriesPoint(row pgx.Row) (econindex.SeriesPoint, error) {
	var p econindex.SeriesPoint
	err := row.Scan(&p.SeriesCode, &p.Date, &p.Value, &p.SyncedAt)
	return p, err
}

// GetLatestSeriesPoint returns the most recent point stored for seriesCode.
func (s *Store) GetLatestSeriesPoint(ctx context.Context, seriesCode string) (econindex.SeriesPoint, error) {
	const q = `SELECT series_code, point_date, value_text, synced_at
FROM econindex_series_point WHERE series_code = $1 ORDER BY point_date DESC LIMIT 1`
	row := s.pool.QueryRow(ctx, q, seriesCode)
	p, err := scanSeriesPoint(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return econindex.SeriesPoint{}, fmt.Errorf("series %s: %w", seriesCode, pgx.ErrNoRows)
	}
	if err != nil {
		return econindex.SeriesPoint{}, fmt.Errorf("get latest econindex point %s: %w", seriesCode, err)
	}
	return p, nil
}

// GetSeriesRange returns every stored point for seriesCode between from and
// to (inclusive), ordered by date ascending.
func (s *Store) GetSeriesRange(ctx context.Context, seriesCode string, from, to time.Time) ([]econindex.SeriesPoint, error) {
	const q = `SELECT series_code, point_date, value_text, synced_at
FROM econindex_series_point WHERE series_code = $1 AND point_date BETWEEN $2 AND $3 ORDER BY point_date`
	rows, err := s.pool.Query(ctx, q, seriesCode, from, to)
	if err != nil {
		return nil, fmt.Errorf("get econindex range %s: %w", seriesCode, err)
	}
	defer rows.Close()
	var out []econindex.SeriesPoint
	for rows.Next() {
		p, err := scanSeriesPoint(rows)
		if err != nil {
			return nil, fmt.Errorf("scan econindex row: %w", err)
		}
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate econindex rows: %w", err)
	}
	return out, nil
}

// CountSeriesPoints returns the number of stored points for seriesCode.
func (s *Store) CountSeriesPoints(ctx context.Context, seriesCode string) (int, error) {
	var n int
	const q = `SELECT count(*) FROM econindex_series_point WHERE series_code = $1`
	if err := s.pool.QueryRow(ctx, q, seriesCode).Scan(&n); err != nil {
		return 0, fmt.Errorf("count econindex points %s: %w", seriesCode, err)
	}
	return n, nil
}
```

- [ ] **Step 6: Run tests to verify they pass**

Run: `go test ./internal/store/postgres/... -run 'TestUpsertSeriesPoint|TestGetSeriesRange|TestGetLatestSeriesPoint' -v`
Expected: PASS (5 tests). If `PIXKB_TEST_DSN` is unset they SKIP — set it to a throwaway database first (see `deploy/local-testdb.sh`).

- [ ] **Step 7: Commit**

```
git add internal/store/postgres/schema/0006_econindex_series_point.up.sql internal/store/postgres/schema/0006_econindex_series_point.down.sql internal/store/postgres/econindex.go internal/store/postgres/econindex_test.go
git commit -m "feat: add econindex_series_point migration + Postgres store methods"
```

---

### Task 3: CLI — `pixkb econindex fetch|load|sync` (online + offline-load halves)

**Files:**
- Create: `cmd/pixkb/econindex.go`
- Test: `cmd/pixkb/econindex_test.go`

**Interfaces:**
- Consumes: `loadConfig() Config`, `openStore(ctx, cfg) (*postgres.Store, error)`, `Config.MirrorDir` (`cmd/pixkb/config.go`); everything from Tasks 1–2.
- Produces: `newEconIndexCmd() *cobra.Command` (not yet registered on root — Task 4 wires it into `attachCommands`), with `fetch`, `load`, `sync` subcommands. `lookup` is added in Task 4.

Air-gap split: `fetch` (network only, stages raw SGS JSON to a file) is separate from `load` (offline, reads the staged file, parses, upserts — no network). `sync` is the online convenience wrapper (fetch in-memory + load, no staged file), supporting both a single `--series` and `--all` (every `DefaultSeriesConfigs()` entry, sequential, stop on first error). No subcommand takes `--dsn` — enforced by an automated test in this task, matching `ispb_test.go`'s `TestNewISPBCmd_NoDSNFlag`.

- [ ] **Step 1: Write the failing tests**

```go
// cmd/pixkb/econindex_test.go
package main

import (
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewEconIndexCmd_Wiring(t *testing.T) {
	t.Parallel()
	root := newEconIndexCmd()
	assert.Equal(t, "econindex", root.Name())

	names := map[string]bool{}
	for _, c := range root.Commands() {
		names[c.Name()] = true
	}
	for _, want := range []string{"fetch", "load", "sync"} {
		assert.True(t, names[want], "missing subcommand %q", want)
	}
}

// TestNewEconIndexCmd_NoDSNFlag guards the project rule that the DSN must
// come from config/env only — no econindex subcommand may expose --dsn.
func TestNewEconIndexCmd_NoDSNFlag(t *testing.T) {
	t.Parallel()
	var walk func(cmd *cobra.Command)
	walk = func(cmd *cobra.Command) {
		assert.Nilf(t, cmd.Flags().Lookup("dsn"), "%s must not have a --dsn flag", cmd.CommandPath())
		for _, c := range cmd.Commands() {
			walk(c)
		}
	}
	walk(newEconIndexCmd())
}

func TestEconIndexFetch_UnknownSeries(t *testing.T) {
	t.Parallel()
	cmd := newEconIndexCmd()
	cmd.SetArgs([]string{"fetch", "--series", "not-a-series"})
	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown econindex series")
}

func TestEconIndexFetch_MissingSeries(t *testing.T) {
	t.Parallel()
	cmd := newEconIndexCmd()
	cmd.SetArgs([]string{"fetch"})
	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--series is required")
}

// TestEconIndexSync_NoDSN documents that sync opens the store (to fail fast
// on a missing DSN) before validating --series/--all, matching the ordering
// established by ispb_test.go's TestISPBLookup_NoDSN.
func TestEconIndexSync_NoDSN(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("PIXKB_DSN", "")
	root := NewRootCmd()
	root.SetArgs([]string{"econindex", "sync"})
	err := root.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no database DSN")
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./cmd/pixkb/... -run TestNewEconIndexCmd -v` and `go test ./cmd/pixkb/... -run TestEconIndex -v`
Expected: FAIL — `undefined: newEconIndexCmd`. (`TestEconIndexSync_NoDSN` also fails with "unknown command econindex" until Task 4 registers it on root — acceptable at this step; it will pass once Task 4 wires `attachCommands`. Run the other four via `-run` filters that exclude it if isolating this task's diff.)

- [ ] **Step 3: Write the implementation**

```go
// cmd/pixkb/econindex.go
package main

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"pixkb/internal/econindex"
)

func newEconIndexCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "econindex",
		Short: "Fetch and look up BACEN economic-indicator series (SELIC, USD/BRL PTAX)",
	}
	cmd.AddCommand(newEconIndexFetchCmd(), newEconIndexLoadCmd(), newEconIndexSyncCmd())
	return cmd
}

func econIndexLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, nil))
}

func defaultEconIndexPath(cfg Config, seriesName string) string {
	return filepath.Join(cfg.MirrorDir, "bacen-econindex", seriesName+".json")
}

// parseEconIndexDate parses a CLI-facing YYYY-MM-DD date flag into UTC midnight.
func parseEconIndexDate(s string) (time.Time, error) {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid date %q: want YYYY-MM-DD", s)
	}
	return t, nil
}

func newEconIndexFetchCmd() *cobra.Command {
	var series, from, to, out string
	cmd := &cobra.Command{
		Use:   "fetch",
		Short: "Download a BACEN SGS series (latest point, or a date range) and stage it to a file",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if series == "" {
				return fmt.Errorf("--series is required")
			}
			cfg, err := econindex.FindSeriesConfig(series)
			if err != nil {
				return err
			}

			var data []byte
			switch {
			case from == "" && to == "":
				data, err = econindex.DownloadLatest(cmd.Context(), cfg, econIndexLogger())
			case from != "" && to != "":
				fromT, err2 := parseEconIndexDate(from)
				if err2 != nil {
					return err2
				}
				toT, err2 := parseEconIndexDate(to)
				if err2 != nil {
					return err2
				}
				data, err = econindex.DownloadRange(cmd.Context(), cfg, fromT, toT, econIndexLogger())
			default:
				return fmt.Errorf("--from and --to must be given together")
			}
			if err != nil {
				return err
			}

			cliCfg := loadConfig()
			path := out
			if path == "" {
				path = defaultEconIndexPath(cliCfg, cfg.Name)
			}
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				return fmt.Errorf("create staging dir: %w", err)
			}
			if err := os.WriteFile(path, data, 0o644); err != nil {
				return fmt.Errorf("write %s: %w", path, err)
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "staged %d bytes to %s\n", len(data), path)
			return nil
		},
	}
	cmd.Flags().StringVar(&series, "series", "", "series key or SGS code (e.g. selic-diaria, 11)")
	cmd.Flags().StringVar(&from, "from", "", "range start, YYYY-MM-DD (requires --to; default: latest point only)")
	cmd.Flags().StringVar(&to, "to", "", "range end, YYYY-MM-DD (requires --from)")
	cmd.Flags().StringVar(&out, "out", "", "output path (default: <mirror_dir>/bacen-econindex/<series>.json)")
	return cmd
}

func newEconIndexLoadCmd() *cobra.Command {
	var series, file string
	cmd := &cobra.Command{
		Use:   "load",
		Short: "Parse a staged BACEN SGS JSON file and upsert it into the database",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if series == "" {
				return fmt.Errorf("--series is required")
			}
			cfg, err := econindex.FindSeriesConfig(series)
			if err != nil {
				return err
			}
			cliCfg := loadConfig()
			path := file
			if path == "" {
				path = defaultEconIndexPath(cliCfg, cfg.Name)
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return fmt.Errorf("read %s: %w", path, err)
			}
			points, err := econindex.ParseSeries(data, cfg.Code, time.Now())
			if err != nil {
				return err
			}
			ctx := cmd.Context()
			st, err := openStore(ctx, cliCfg)
			if err != nil {
				return err
			}
			defer st.Close()
			if err := st.UpsertSeriesPoints(ctx, points); err != nil {
				return err
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "loaded %d %s points\n", len(points), cfg.Name)
			return nil
		},
	}
	cmd.Flags().StringVar(&series, "series", "", "series key or SGS code (e.g. selic-diaria, 11)")
	cmd.Flags().StringVar(&file, "file", "", "input path (default: <mirror_dir>/bacen-econindex/<series>.json)")
	return cmd
}

func newEconIndexSyncCmd() *cobra.Command {
	var series, from, to string
	var all bool
	cmd := &cobra.Command{
		Use:   "sync",
		Short: "Download and load one (or all) BACEN SGS series in one step",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cliCfg := loadConfig()
			ctx := cmd.Context()
			st, err := openStore(ctx, cliCfg)
			if err != nil {
				return err
			}
			defer st.Close()

			var targets []econindex.SeriesConfig
			if all {
				for _, cfg := range econindex.DefaultSeriesConfigs() {
					targets = append(targets, cfg)
				}
			} else {
				if series == "" {
					return fmt.Errorf("--series is required unless --all is given")
				}
				cfg, err := econindex.FindSeriesConfig(series)
				if err != nil {
					return err
				}
				targets = []econindex.SeriesConfig{cfg}
			}

			var fromT, toT time.Time
			ranged := from != "" || to != ""
			if ranged {
				if from == "" || to == "" {
					return fmt.Errorf("--from and --to must be given together")
				}
				if fromT, err = parseEconIndexDate(from); err != nil {
					return err
				}
				if toT, err = parseEconIndexDate(to); err != nil {
					return err
				}
			}

			for _, cfg := range targets {
				var data []byte
				if ranged {
					data, err = econindex.DownloadRange(ctx, cfg, fromT, toT, econIndexLogger())
				} else {
					data, err = econindex.DownloadLatest(ctx, cfg, econIndexLogger())
				}
				if err != nil {
					return err
				}
				points, err := econindex.ParseSeries(data, cfg.Code, time.Now())
				if err != nil {
					return err
				}
				if err := st.UpsertSeriesPoints(ctx, points); err != nil {
					return err
				}
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "synced %d %s points\n", len(points), cfg.Name)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&series, "series", "", "series key or SGS code (e.g. selic-diaria, 11)")
	cmd.Flags().StringVar(&from, "from", "", "range start, YYYY-MM-DD (requires --to; default: latest point only)")
	cmd.Flags().StringVar(&to, "to", "", "range end, YYYY-MM-DD (requires --from)")
	cmd.Flags().BoolVar(&all, "all", false, "sync every known series instead of a single --series")
	return cmd
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./cmd/pixkb/... -run 'TestNewEconIndexCmd|TestEconIndexFetch' -v`
Expected: PASS (`TestNewEconIndexCmd_Wiring`, `TestNewEconIndexCmd_NoDSNFlag`, `TestEconIndexFetch_UnknownSeries`, `TestEconIndexFetch_MissingSeries`). `TestEconIndexSync_NoDSN` still fails at this step (root doesn't know `econindex` yet) — resolved in Task 4.

- [ ] **Step 5: Commit**

```
git add cmd/pixkb/econindex.go cmd/pixkb/econindex_test.go
git commit -m "feat: add pixkb econindex fetch/load/sync commands"
```

---

### Task 4: CLI — `pixkb econindex lookup` + root wiring

**Files:**
- Modify: `cmd/pixkb/econindex.go` — add `newEconIndexLookupCmd()`, register it on `newEconIndexCmd()`
- Modify: `cmd/pixkb/commands.go` — add `newEconIndexCmd()` to `attachCommands`
- Modify: `cmd/pixkb/econindex_test.go` — add lookup tests

**Interfaces:**
- Consumes: `(*Store) GetLatestSeriesPoint`, `(*Store) GetSeriesRange` (Task 2); `econindex.FindSeriesConfig`, `parseEconIndexDate` (Task 3).
- Produces: `lookup` subcommand on `newEconIndexCmd()`; `newEconIndexCmd()` registered on root as `pixkb econindex`.

`lookup <series>` is fully offline/DB-only — no network call — matching the air-gap split's "load"/"lookup" half. With no `--date`, it reads the latest stored point; with `--date YYYY-MM-DD`, it reads that exact date via `GetSeriesRange(from, to)` with `from == to`.

- [ ] **Step 1: Write the failing tests** (append to `cmd/pixkb/econindex_test.go`)

```go
func TestNewEconIndexCmd_LookupWired(t *testing.T) {
	t.Parallel()
	root := newEconIndexCmd()
	_, _, err := root.Find([]string{"lookup"})
	require.NoError(t, err)
}

func TestEconIndexLookup_UnknownSeries(t *testing.T) {
	t.Parallel()
	root := NewRootCmd()
	root.SetArgs([]string{"econindex", "lookup", "not-a-series"})
	err := root.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown econindex series")
}

func TestEconIndexLookup_NoDSN(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("PIXKB_DSN", "")
	root := NewRootCmd()
	root.SetArgs([]string{"econindex", "lookup", "selic-diaria"})
	err := root.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no database DSN")
}

// TestEconIndexLookup_BadDateFlag documents that the series resolves and the
// store-open is attempted before --date is parsed, so an unset DSN still
// surfaces first here — same ordering as TestEconIndexLookup_NoDSN.
func TestEconIndexLookup_BadDateFlag(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("PIXKB_DSN", "")
	root := NewRootCmd()
	root.SetArgs([]string{"econindex", "lookup", "selic-diaria", "--date", "07-04-2026"})
	err := root.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no database DSN")
}
```

Also re-run `TestEconIndexSync_NoDSN` from Task 3 — it now passes because `econindex` is registered on root.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./cmd/pixkb/... -run 'TestNewEconIndexCmd_LookupWired|TestEconIndexLookup' -v`
Expected: FAIL — `undefined: newEconIndexLookupCmd` / `root.Find` errors with "unknown command lookup".

- [ ] **Step 3: Write the implementation**

```go
// cmd/pixkb/econindex.go — add these imports: "io"
// (full new import block)
import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"pixkb/internal/econindex"
)
```

```go
// cmd/pixkb/econindex.go — update newEconIndexCmd()
func newEconIndexCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "econindex",
		Short: "Fetch and look up BACEN economic-indicator series (SELIC, USD/BRL PTAX)",
	}
	cmd.AddCommand(newEconIndexFetchCmd(), newEconIndexLoadCmd(), newEconIndexSyncCmd(), newEconIndexLookupCmd())
	return cmd
}
```

```go
// cmd/pixkb/econindex.go — new function, appended after newEconIndexSyncCmd()
func newEconIndexLookupCmd() *cobra.Command {
	var date string
	cmd := &cobra.Command{
		Use:   "lookup <series>",
		Short: "Look up a stored BACEN SGS series point (latest, or a specific date)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := econindex.FindSeriesConfig(args[0])
			if err != nil {
				return err
			}
			cliCfg := loadConfig()
			ctx := cmd.Context()
			st, err := openStore(ctx, cliCfg)
			if err != nil {
				return err
			}
			defer st.Close()

			out := cmd.OutOrStdout()
			if date == "" {
				p, err := st.GetLatestSeriesPoint(ctx, cfg.Code)
				if err != nil {
					return err
				}
				return printEconIndexPoint(out, cfg, p)
			}

			d, err := parseEconIndexDate(date)
			if err != nil {
				return err
			}
			points, err := st.GetSeriesRange(ctx, cfg.Code, d, d)
			if err != nil {
				return err
			}
			if len(points) == 0 {
				return fmt.Errorf("no stored point for series %s on %s", cfg.Name, date)
			}
			return printEconIndexPoint(out, cfg, points[0])
		},
	}
	cmd.Flags().StringVar(&date, "date", "", "exact date to look up, YYYY-MM-DD (default: latest stored point)")
	return cmd
}

func printEconIndexPoint(out io.Writer, cfg econindex.SeriesConfig, p econindex.SeriesPoint) error {
	_, err := fmt.Fprintf(out, "Series:   %s (%s, code %s)\nDate:     %s\nValue:    %s\nSynced:   %s\n",
		cfg.Name, cfg.Description, cfg.Code, p.Date.Format("2006-01-02"), p.Value, p.SyncedAt.Format(time.RFC3339))
	return err
}
```

```go
// cmd/pixkb/commands.go — attachCommands, add newEconIndexCmd() to the AddCommand list
func attachCommands(root *cobra.Command) {
	root.AddCommand(newIngestCmd(), newSearchCmd(), newReindexCmd(), newDiffCmd(), newStatsCmd(), newRelatedCmd(), newSimilarCmd(), newAgentsCmd(), newConceptCmd(), newMCPCmd(), newHygieneCmd(), newCurateCmd(), newQRCmd(), newAskCmd(), newISPBCmd(), newEvalCmd(), newVocabCmd(), newSearchHealthCmd(), newEconIndexCmd())
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./cmd/pixkb/... -run 'TestNewEconIndexCmd|TestEconIndex' -v`
Expected: PASS (all wiring, no-DSN, unknown-series, and Task 3's now-passing `TestEconIndexSync_NoDSN`).

Run: `go build ./...`
Expected: builds clean (confirms `commands.go`'s new call site compiles).

- [ ] **Step 5: Commit**

```
git add cmd/pixkb/econindex.go cmd/pixkb/econindex_test.go cmd/pixkb/commands.go
git commit -m "feat: add pixkb econindex lookup command + wire into root"
```

---

### Task 5: Full-suite verification + `docs/BACKLOG.md` update

**Files:**
- Modify: `docs/BACKLOG.md`

**Interfaces:** none (documentation + verification only).

- [ ] **Step 1: Run the full test suite**

Run: `go test ./... -short`
Expected: PASS everywhere, including the new `internal/econindex`, `cmd/pixkb` tests (Postgres-backed tests in `internal/store/postgres` skip cleanly under `-short`).

Run (with a throwaway Postgres up and `PIXKB_TEST_DSN` set): `go test ./... `
Expected: PASS, including `internal/store/postgres/econindex_test.go`'s 5 integration tests.

- [ ] **Step 2: Lint**

Run: `golangci-lint run ./...`
Expected: 0 issues (matches the Phase 7 "golangci-lint clean" bar already established for this codebase).

- [ ] **Step 3: Manual smoke test (optional, requires network + a real Postgres)**

```
PIXKB_DSN=$PIXKB_TEST_DSN go run ./cmd/pixkb db up
PIXKB_DSN=$PIXKB_TEST_DSN go run ./cmd/pixkb econindex sync --series selic-diaria
PIXKB_DSN=$PIXKB_TEST_DSN go run ./cmd/pixkb econindex lookup selic-diaria
PIXKB_DSN=$PIXKB_TEST_DSN go run ./cmd/pixkb econindex sync --all
PIXKB_DSN=$PIXKB_TEST_DSN go run ./cmd/pixkb econindex lookup usd-ptax-venda
```
Expected: `sync` prints `synced N selic-diaria points`; `lookup` prints the `Series/Date/Value/Synced` block for a recent date.

- [ ] **Step 4: Update `docs/BACKLOG.md`**

Replace the existing SELIC/Dólar backlog bullet (currently under `## P2`) with a
shipped-status note, following this file's existing convention of marking a
shipped item inline rather than deleting its history. Find the bullet
starting `- **SELIC and Dólar (USD/BRL) mappers — current + historical
series.**` and replace its full body with:

```markdown
- ~~**SELIC and Dólar (USD/BRL) mappers — current + historical series.**~~
  SHIPPED (2026-07-04). `internal/econindex` (SGS API client + parser,
  `DownloadLatest`/`DownloadRange`/`FetchHistory` with 10-year-window paging)
  + `econindex_series_point` table (migration `0006`) + `pixkb econindex
  {fetch,load,sync,lookup}` cover all three known series (`selic-diaria`
  code 11, `selic-meta` code 432, `usd-ptax-venda` code 1) via the same
  air-gap fetch/load/sync split used by `internal/ispb`. Deliberately out of
  scope, tracked as a future follow-up if ever needed: the Olinda PTAX OData
  API (`olinda.bcb.gov.br/olinda/servico/PTAX`) for compra/abertura/fechamento
  quotes beyond the SGS venda series; no MCP tool / concept-store integration
  yet (same deferral as ISPB's own v1).
```

Bump the revision tag on the line directly after the `# pixkb Backlog` H1
from `<!-- rev:046 -->` to `<!-- rev:047 -->` (or the current value + 1, if
other backlog edits have landed between this plan's writing and its
execution — always bump from whatever value is live in the file at commit
time, never overwrite a higher number with 047).

- [ ] **Step 5: Commit**

```
git add docs/BACKLOG.md
git commit -m "docs: mark SELIC/Dólar econindex mappers shipped in BACKLOG.md"
```
