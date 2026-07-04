# SELIC/Dólar (econindex) Mappers — Design Spec

**Date:** 2026-07-04
**Project:** `pixkb` (`C:\weaver-sync\development\personal\projects\bacen`)
**Status:** Draft — awaiting user approval

## Goal

Add an **economic-indicator mapper** to `pixkb`, structurally identical to the
ISPB mapper (`internal/ispb`, `docs/superpowers/specs/2026-07-03-ispb-mapper-design.md`):
lookup and historical storage for three BACEN series, all served by the same
unauthenticated JSON API — no CSV, no encoding quirks, no date-probing (a
strict simplification versus ISPB's Pix source).

| Series key | SGS code | Description |
|---|---|---|
| `selic-diaria` | 11 | Taxa Selic diária (daily rate, expressed as a small decimal, e.g. `0.052531`) |
| `selic-meta` | 432 | Taxa Selic meta/target (Copom target rate, e.g. `14.25`) |
| `usd-ptax-venda` | 1 | Dólar comercial, PTAX venda (daily USD/BRL sell quote, e.g. `5.1945`) |

## Source — confirmed live during design

(Fetched via sandboxed HTTP per this project's context-mode routing rules,
not WebFetch/curl.)

`GET https://api.bcb.gov.br/dados/serie/bcdata.sgs.<codigo>/dados/ultimos/<n>?formato=json`
— latest N points, unauthenticated:

```json
[{"data":"02/07/2026","valor":"0.052531"},{"data":"03/07/2026","valor":"0.052531"}]
```

`GET https://api.bcb.gov.br/dados/serie/bcdata.sgs.<codigo>/dados?formato=json&dataInicial=DD/MM/YYYY&dataFinal=DD/MM/YYYY`
— full-range history, same shape. Verified against series 11 (Jan 2024):
returns one point per business day, `valor` already dot-decimal (**not**
comma-decimal — simpler than the ISPB Pix CSV's locale quirks).

**Confirmed server-side range cap:** a `dataInicial`/`dataFinal` span over 10
years on a daily series returns **HTTP 406**:

```json
{"error":"O sistema aceita uma janela de consulta de, no máximo, 10 anos em séries de periodicidade diária", ...}
```

This means full-history fetches **must** page in ≤10-year windows — confirmed
requirement, not speculation. All three series above are daily-periodicity,
so the same 10-year cap applies uniformly; no per-series exception needed.

**Olinda PTAX OData API** (`olinda.bcb.gov.br/olinda/servico/PTAX`) was
evaluated as a possible better fit for the *official* daily PTAX quote (it
separates compra/venda and abertura/fechamento explicitly). **Decision: out
of scope for v1.** The SGS series-1 endpoint already returns exactly the venda
quote needed, with the same client/parse code path as the other two series —
adding a second, differently-shaped API doubles the surface for a distinction
(official vs. SGS-mirrored PTAX) this feature doesn't need yet. Tracked as a
follow-up in `docs/BACKLOG.md` if a consumer ever needs compra/abertura/fechamento.

## Architecture

```
internal/econindex/              # pure: no DB import
  series.go   SeriesConfig, DefaultSeriesConfigs() map[string]SeriesConfig,
              SeriesPoint{SeriesCode, Date, Value, SyncedAt}, (SeriesPoint) Validate()
  client.go   DownloadLatest(ctx, cfg, log) ([]byte, error)
              DownloadRange(ctx, cfg, from, to, log) ([]byte, error)
              FetchHistory(ctx, cfg, from, to, log) ([]SeriesPoint, error)  — pages ≤10y windows, calls DownloadRange + ParseSeries per window, merges+sorts
  parse.go    ParseSeries(data []byte, seriesCode string, syncedAt time.Time) ([]SeriesPoint, error)

internal/store/postgres/
  econindex.go   methods on *Store: UpsertSeriesPoint / UpsertSeriesPoints /
                 GetLatestSeriesPoint / GetSeriesRange / CountSeriesPoints
  schema/0006_econindex_series_point.up.sql
  schema/0006_econindex_series_point.down.sql

cmd/pixkb/
  econindex.go   newEconIndexCmd(): `econindex fetch`, `econindex load`,
                 `econindex sync`, `econindex lookup <series>`
```

**Rationale:** Same file-per-concern split as ISPB (`participant.go` /
`str.go` / `pix.go` → here `series.go` / `client.go` / `parse.go`), but
collapsed to one *source* instead of two, because all three indicators share
one API shape. No STR/Pix-style "two sources reconciling one row" complexity
— every point has exactly one writer, so upserts are unconditional overwrites.

## Data model

```go
// internal/econindex/series.go

// SeriesConfig identifies one BACEN SGS series and how to fetch it.
type SeriesConfig struct {
    Name          string        // short key, e.g. "selic-diaria"
    Code          string        // SGS numeric series code, e.g. "11"
    Description   string        // human label, e.g. "Taxa Selic diária"
    BaseURL       string        // "%s"-templated with Code, e.g. "https://api.bcb.gov.br/dados/serie/bcdata.sgs.%s"
    HTTPTimeout   time.Duration
    MaxRangeYears int           // pagination window for FetchHistory; BCB caps daily series at 10y/request
}

// SeriesPoint is one dated value of a BACEN SGS series.
type SeriesPoint struct {
    SeriesCode string    // SGS numeric code, e.g. "11" (not the short Name — Name is a client-side convenience key only)
    Date       time.Time // UTC, truncated to day
    Value      string    // decimal value, verbatim from BCB (already dot-separated), e.g. "0.052531"
    SyncedAt   time.Time
}
```

`SeriesPoint.Validate()` enforces: `SeriesCode` non-empty and numeric,
`Date` non-zero, `Value` matches `^-?\d+(\.\d+)?$`.

**Why `Value` is a string, not `float64`:** per this project's Brazilian
financial context convention (decimal types, never `float64`), the raw BCB
decimal string is stored verbatim — no lossy float round-trip. Callers that
need arithmetic parse it themselves via stdlib `math/big` (`new(big.Rat).SetString(v)`);
`internal/econindex` never does arithmetic on `Value`, only validates its shape.

## Config

`DefaultSeriesConfigs()` returns the three known series, keyed by short name:

```go
func DefaultSeriesConfigs() map[string]SeriesConfig {
    const base = "https://api.bcb.gov.br/dados/serie/bcdata.sgs.%s"
    mk := func(name, code, desc string) SeriesConfig {
        return SeriesConfig{Name: name, Code: code, Description: desc,
            BaseURL: base, HTTPTimeout: 30 * time.Second, MaxRangeYears: 10}
    }
    return map[string]SeriesConfig{
        "selic-diaria":   mk("selic-diaria", "11", "Taxa Selic diária"),
        "selic-meta":     mk("selic-meta", "432", "Taxa Selic meta (Copom)"),
        "usd-ptax-venda": mk("usd-ptax-venda", "1", "Dólar comercial, PTAX venda"),
    }
}
```

No new `pixkb.yaml` keys in v1 — matches ISPB precedent. **No `--dsn` flag
anywhere**; DSN resolution stays exclusively `loadConfig()` / `openStore()`.

## Migration — `0006_econindex_series_point`

(`0004` is `ispb_participant`, `0005` is `unaccent_extension` — next free
slot is `0006`.) One table, one row per `(series_code, date)`:

```sql
-- up
CREATE TABLE IF NOT EXISTS econindex_series_point (
    series_code   TEXT        NOT NULL,
    point_date    DATE        NOT NULL,
    value_text    TEXT        NOT NULL,
    synced_at     TIMESTAMPTZ NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (series_code, point_date)
);
-- down
DROP TABLE IF EXISTS econindex_series_point;
```

Upsert is an unconditional overwrite (single source of truth, unlike ISPB's
STR-wins-over-Pix precedence rule):

```sql
INSERT INTO econindex_series_point (series_code, point_date, value_text, synced_at, updated_at)
VALUES ($1,$2,$3,$4, now())
ON CONFLICT (series_code, point_date) DO UPDATE SET
  value_text = EXCLUDED.value_text,
  synced_at  = EXCLUDED.synced_at,
  updated_at = now()
```

Auto-picked up by the embedded `schema/*.sql` FS (`internal/store/postgres/migrations.go`);
applied with the existing `pixkb db up`.

## CLI — `pixkb econindex`

Flat command group (no nested per-source subgroups — there's only one
source), appended to `attachCommands(root)` alongside `newISPBCmd()`:

| Command | Network | DB | Action |
|---|---|---|---|
| `econindex fetch --series <key> [--from DATE --to DATE] [--out <path>]` | ✅ | — | no `--from`/`--to` → `DownloadLatest` (1 point); both given → `DownloadRange` (paged internally if >10y). Writes raw JSON bytes to file. |
| `econindex load --series <key> --file <path>` | — | ✅ | read file → `ParseSeries` → `UpsertSeriesPoints` |
| `econindex sync --series <key> [--from --to]` | ✅ | ✅ | fetch (in-memory, no staged file) → parse → upsert |
| `econindex sync --all [--from --to]` | ✅ | ✅ | `sync` for all three `DefaultSeriesConfigs()` entries, stop on first error |
| `econindex lookup <series> [--date YYYY-MM-DD]` | — | ✅ | no `--date` → `GetLatestSeriesPoint`; else → exact-date point from `GetSeriesRange` |

`<series>` accepts either the short key (`selic-diaria`) or the raw SGS code
(`11`) — resolved via `DefaultSeriesConfigs()` by matching `Name` or `Code`.

Default staged path: `<mirror_dir>/bacen-econindex/<series-key>.json` (mirrors
ISPB's `mirrors/bacen-ispb/` convention). `--from`/`--to` accept `YYYY-MM-DD`
(Go-idiomatic; converted to BCB's `DD/MM/YYYY` internally — the CLI layer
should not leak BCB's date format to users).

`DownloadLatest`/`DownloadRange`/`FetchHistory` receive a `*slog.Logger`
(constructed in the command, writing to `os.Stderr`, matching `ispbLogger()`).

## Dependencies

None new. `net/http`, `encoding/json`, `time`, `math/big` (validation only,
stdlib) — pure Go, safe under `CGO_ENABLED=0`.

## Testing (testify)

- `internal/econindex/series_test.go` — `Validate()` table tests (bad
  `SeriesCode`, zero `Date`, malformed `Value`).
- `internal/econindex/parse_test.go` — SGS JSON fixture (2+ points, one
  malformed row) → assert parsed `[]SeriesPoint`; date parsing (`DD/MM/YYYY`
  → UTC midnight); malformed-row skip behavior documented and tested.
- `internal/econindex/client_test.go` — `httptest.Server` for `DownloadLatest`
  and `DownloadRange` (fixture bytes, injected `BaseURL`, no real network);
  `FetchHistory` test with a >10-year requested range against a test server
  that serves two windows, asserting exactly 2 requests were made and results
  are merged+sorted — this is the pagination behavior confirmed live above.
- `internal/store/postgres/econindex_test.go` — `UpsertSeriesPoint` +
  `UpsertSeriesPoints` (batch) → `GetLatestSeriesPoint`/`GetSeriesRange`/`CountSeriesPoints`
  roundtrip, plus overwrite-on-conflict; guarded by `PIXKB_TEST_DSN`, skipped
  under `-short` (matches `internal/store/postgres/ispb_test.go`).
- `cmd/pixkb/econindex_test.go` — command wiring (`TestNewEconIndexCmd_Wiring`),
  no-`--dsn`-flag guard (`TestNewEconIndexCmd_NoDSNFlag`, walks the command
  tree like `ispb_test.go`'s equivalent), `lookup` with an unknown series key
  and with `PIXKB_DSN` unset.

## Out of scope (YAGNI)

- No Olinda PTAX OData integration (see rationale above) — tracked as a
  `docs/BACKLOG.md` follow-up if compra/abertura/fechamento is ever needed.
- No scheduler / cron auto-sync.
- No unit conversion or rate compounding (e.g. daily Selic → annualized) —
  `Value` is stored and returned exactly as BCB publishes it.
- No MCP tool / concept-store integration in v1 (can be a follow-up, same as ISPB).
- No `--fail-under`-style CI gating on `lookup` (it's a read command, not a
  measurement tool — this note is only to pre-empt scope creep from the
  `pixkb eval` convention elsewhere in this codebase).
