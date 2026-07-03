# ISPB Mapper Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add an ISPB→participant mapper to `pixkb`: a `pixkb ispb` CLI that downloads, parses, and stores BACEN's STR (canonical, all SPB participants) and Pix (adherents + BCB-authorization flag) participant registries, then looks them up by 8-digit ISPB code.

**Architecture:** A pure `internal/ispb` library (no DB import) holds two independent source readers — `str.go` (static CSV, UTF-8, no date probing) and `pix.go` (dated CSV, Windows-1252, date-probed — ported from `shield_ispb`). `internal/store/postgres/ispb.go` adds per-source upsert methods plus merged lookup/list/count methods on the existing `*Store`. `cmd/pixkb/ispb.go` wires both sources into a nested Cobra command tree honoring the project's air-gap convention (online fetch → staged file → offline load).

**Tech Stack:** Go 1.25.0, `github.com/spf13/cobra`, `github.com/jackc/pgx/v5`, `github.com/golang-migrate/migrate/v4`, `github.com/stretchr/testify`, `golang.org/x/text` (Windows-1252 decoding, Pix path only).

**Design spec:** `docs/superpowers/specs/2026-07-03-ispb-mapper-design.md`

## Global Constraints

- Module is `pixkb`; Go 1.25.0; `CGO_ENABLED=0` pure-Go only — no cgo dependencies.
- **No `--dsn` flag on any new command, anywhere.** DSN is resolved exclusively via `loadConfig()` (reads `pixkb.yaml` then `PIXKB_DSN` env) and `openStore(ctx, cfg)` (`cmd/pixkb/config.go:44,105`). This is a hard project rule.
- This directory (`C:\weaver-sync\development\personal\projects\bacen`) is **not a git repository** (`git status` fails with "not a git repository"). Skip every `git add`/`git commit` step in this plan — just mark the checkbox and move on. If the user initializes git later, all task diffs are already on disk.
- All tests use `testify` (`require`/`assert`), co-located as `*_test.go` in the same package, matching existing files (e.g. `internal/store/postgres/stats_test.go`, `cmd/pixkb/config_test.go`).
- Postgres integration tests must call `testDSN(t)` (skips under `-short` and when `PIXKB_TEST_DSN` unset — `internal/store/postgres/store_test.go:17`) and `applyTestSchema(t, dsn)` (`internal/store/postgres/schema_test.go:28`) before touching the DB. Do not duplicate these helpers.
- Migration files are 4-digit sequential `NNNN_name.up.sql` / `NNNN_name.down.sql` under `internal/store/postgres/schema/`, auto-picked up by the `//go:embed schema/*.sql` in `internal/store/postgres/migrations.go` — no wiring needed beyond adding the files.
- `golang.org/x/text v0.36.0` is already in `go.mod` as an **indirect** dependency. Once `internal/ispb/pix.go` imports it directly, run `go mod tidy` to promote it — do not hand-edit `go.mod`.
- Every exported function needs a doc comment starting with its name (Go convention already used throughout the codebase).

---

### Task 1: Participant model + ISPB validation

**Files:**
- Create: `internal/ispb/participant.go`
- Test: `internal/ispb/participant_test.go`

**Interfaces:**
- Produces: `ispb.ValidateISPB(code string) error` (regex `^\d{8}$`), `ispb.Participant` struct (merged read model — used by Task 5's store methods, not written to by Tasks 2/3).

- [ ] **Step 1: Write the failing test**

```go
// internal/ispb/participant_test.go
package ispb

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestValidateISPB(t *testing.T) {
	tests := []struct {
		name    string
		code    string
		wantErr bool
	}{
		{name: "valid 8 digits", code: "00000208", wantErr: false},
		{name: "too short", code: "1234", wantErr: true},
		{name: "too long", code: "123456789", wantErr: true},
		{name: "non-digit", code: "0000020A", wantErr: true},
		{name: "empty", code: "", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateISPB(tt.code)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/ispb/... -run TestValidateISPB -v`
Expected: FAIL — `undefined: ValidateISPB` (package `internal/ispb` doesn't exist yet)

- [ ] **Step 3: Write the implementation**

```go
// internal/ispb/participant.go

// Package ispb maps BACEN ISPB codes to SPB/Pix participant institutions.
package ispb

import (
	"fmt"
	"regexp"
	"time"
)

var ispbPattern = regexp.MustCompile(`^\d{8}$`)

// ValidateISPB checks that code is exactly 8 digits, zero-padded.
func ValidateISPB(code string) error {
	if !ispbPattern.MatchString(code) {
		return fmt.Errorf("invalid ISPB code %q: must be 8 digits", code)
	}
	return nil
}

// Participant is the merged, store-level view of an ISPB record: STR fields
// (canonical) plus Pix-specific fields. A source's fields are left at their
// zero value when that source has never been synced for this code.
type Participant struct {
	ISPB              string
	Name              string
	LegalName         string
	CompeCode         string
	ParticipatesCompe bool
	AccessType        string
	OperationStart    time.Time
	STRSyncedAt       time.Time
	CNPJ              string
	PixAuthorized     bool
	PixSyncedAt       time.Time
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/ispb/... -run TestValidateISPB -v`
Expected: PASS

- [ ] **Step 5: Mark task complete** (no git repo — see Global Constraints)

---

### Task 2: STR source (canonical registry, static CSV)

**Files:**
- Create: `internal/ispb/str.go`
- Test: `internal/ispb/str_test.go`

**Interfaces:**
- Consumes: `ValidateISPB(code string) error` (Task 1).
- Produces: `ispb.STRConfig`, `ispb.DefaultSTRConfig() STRConfig`, `ispb.STRRecord{ISPB, Name, LegalName, CompeCode, ParticipatesCompe, AccessType, OperationStart, SyncedAt}`, `(STRRecord) Validate() error`, `ispb.DownloadSTR(ctx context.Context, cfg STRConfig, log *slog.Logger) ([]byte, error)`, `ispb.ParseSTR(data []byte, syncedAt time.Time) ([]STRRecord, error)`.

**Confirmed live** (fetched via sandboxed HTTP during design, not WebFetch): `GET https://www.bcb.gov.br/content/estabilidadefinanceira/str1/ParticipantesSTR.csv` returns UTF-8-with-BOM, comma-delimited, RFC4180-quoted CSV with header `ISPB,Nome_Reduzido,Número_Código,Participa_da_Compe,Acesso_Principal,Nome_Extenso,Início_da_Operação`. `Início_da_Operação` values are `DD/MM/YYYY`. `Número_Código` is `n/a` for non-bank entities (Selic, Bacen, STN) — normalize to `""`.

**Note:** the original `shield_ispb` zero-pads short ISPB codes via `fmt.Sprintf("%08s", code)` — but `fmt.Sprintf("%08s", "")` returns `"00000000"` (a real bank's code, not an error sentinel), silently corrupting rows with a missing ISPB. This plan fixes that by skipping empty codes *before* padding (applies to both this task and Task 3).

- [ ] **Step 1: Write the failing tests**

```go
// internal/ispb/str_test.go
package ispb

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const strFixture = "\xEF\xBB\xBFISPB,Nome_Reduzido,Número_Código,Participa_da_Compe,Acesso_Principal,Nome_Extenso,Início_da_Operação\n" +
	"00000000,BCO DO BRASIL S.A.,001,Sim,RSFN,Banco do Brasil S.A.,22/04/2002\n" +
	"00038121,Selic,n/a,Não,RSFN,Banco Central do Brasil - Selic,22/04/2002\n" +
	"00122327,SANTINVEST S.A. - CFI,539,Não,RSFN,\"SANTINVEST S.A. - CREDITO, FINANCIAMENTO E INVESTIMENTOS\",17/04/2023\n" +
	"3456,SHORT CODE BANK,999,Sim,Internet,Short Code Bank S.A.,01/01/2020\n" +
	",NO ISPB AT ALL,100,Sim,RSFN,Should Be Skipped,01/01/2020\n"

func TestParseSTR(t *testing.T) {
	synced := time.Date(2026, 7, 3, 0, 0, 0, 0, time.UTC)
	records, err := ParseSTR([]byte(strFixture), synced)
	require.NoError(t, err)
	require.Len(t, records, 4, "5 data rows minus 1 skipped empty-ISPB row")

	brasil := records[0]
	assert.Equal(t, "00000000", brasil.ISPB)
	assert.Equal(t, "BCO DO BRASIL S.A.", brasil.Name)
	assert.Equal(t, "001", brasil.CompeCode)
	assert.True(t, brasil.ParticipatesCompe)
	assert.Equal(t, "RSFN", brasil.AccessType)
	assert.Equal(t, "Banco do Brasil S.A.", brasil.LegalName)
	assert.Equal(t, time.Date(2002, 4, 22, 0, 0, 0, 0, time.UTC), brasil.OperationStart)
	assert.Equal(t, synced, brasil.SyncedAt)

	selic := records[1]
	assert.Equal(t, "", selic.CompeCode, "n/a compe code normalizes to empty")
	assert.False(t, selic.ParticipatesCompe)

	santinvest := records[2]
	assert.Equal(t, "SANTINVEST S.A. - CREDITO, FINANCIAMENTO E INVESTIMENTOS", santinvest.LegalName,
		"quoted comma survives RFC4180 parsing")

	short := records[3]
	assert.Equal(t, "00003456", short.ISPB, "short code zero-padded to 8 digits")
}

func TestParseSTR_NoDataRows(t *testing.T) {
	_, err := ParseSTR([]byte("ISPB,Nome_Reduzido\n"), time.Now())
	assert.Error(t, err)
}

func TestDownloadSTR(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(strFixture))
	}))
	defer srv.Close()

	data, err := DownloadSTR(context.Background(), STRConfig{URL: srv.URL, HTTPTimeout: 5 * time.Second}, nil)
	require.NoError(t, err)
	assert.Equal(t, strFixture, string(data))
}

func TestDownloadSTR_NotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	_, err := DownloadSTR(context.Background(), STRConfig{URL: srv.URL, HTTPTimeout: 5 * time.Second}, nil)
	assert.Error(t, err)
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/ispb/... -run TestParseSTR -v` and `go test ./internal/ispb/... -run TestDownloadSTR -v`
Expected: FAIL — `undefined: ParseSTR`, `undefined: DownloadSTR`, `undefined: STRConfig`

- [ ] **Step 3: Write the implementation**

```go
// internal/ispb/str.go
package ispb

import (
	"bytes"
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

// STRConfig configures the BACEN STR-participants CSV download. The file is
// published at a static URL and always reflects the latest registry — no
// date probing is needed (contrast PixConfig).
type STRConfig struct {
	URL         string
	HTTPTimeout time.Duration
}

// DefaultSTRConfig returns the standard BACEN STR-participants CSV settings.
func DefaultSTRConfig() STRConfig {
	return STRConfig{
		URL:         "https://www.bcb.gov.br/content/estabilidadefinanceira/str1/ParticipantesSTR.csv",
		HTTPTimeout: 30 * time.Second,
	}
}

// STRRecord is one row of the BACEN STR-participants CSV: the full SPB
// participant registry (banks, DTVMs, credit unions, special entities).
type STRRecord struct {
	ISPB              string
	Name              string
	LegalName         string
	CompeCode         string
	ParticipatesCompe bool
	AccessType        string
	OperationStart    time.Time
	SyncedAt          time.Time
}

// Validate checks that ISPB is exactly 8 digits.
func (r STRRecord) Validate() error {
	return ValidateISPB(r.ISPB)
}

// DownloadSTR fetches the current BACEN STR-participants CSV. log may be nil.
func DownloadSTR(ctx context.Context, cfg STRConfig, log *slog.Logger) ([]byte, error) {
	if log == nil {
		log = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	client := &http.Client{Timeout: cfg.HTTPTimeout}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, cfg.URL, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch %s: %w", cfg.URL, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch %s: status %d", cfg.URL, resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}
	log.Info("ispb: downloaded STR participants", "url", cfg.URL, "bytes", len(body))
	return body, nil
}

// ParseSTR decodes the UTF-8 (BOM-prefixed) BACEN STR-participants CSV into
// STRRecords. syncedAt is stamped on every returned record.
func ParseSTR(data []byte, syncedAt time.Time) ([]STRRecord, error) {
	data = bytes.TrimPrefix(data, []byte{0xEF, 0xBB, 0xBF})
	reader := csv.NewReader(bytes.NewReader(data))
	reader.Comma = ','
	reader.LazyQuotes = true
	reader.FieldsPerRecord = -1

	records, err := reader.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("parse csv: %w", err)
	}
	if len(records) < 2 {
		return nil, fmt.Errorf("csv has no data rows (got %d lines)", len(records))
	}

	header := records[0]
	colISPB := strColumn(header, "ISPB")
	colName := strColumn(header, "Nome_Reduzido")
	colCompe := strColumn(header, "Número_Código")
	colParticipates := strColumn(header, "Participa_da_Compe")
	colAccess := strColumn(header, "Acesso_Principal")
	colLegal := strColumn(header, "Nome_Extenso")
	colStart := strColumn(header, "Início_da_Operação")
	if colISPB < 0 || colName < 0 {
		return nil, fmt.Errorf("required columns not found (ISPB=%d, Nome_Reduzido=%d)", colISPB, colName)
	}

	var out []STRRecord
	for _, row := range records[1:] {
		if len(row) <= colISPB || len(row) <= colName {
			continue
		}
		code := strings.TrimSpace(row[colISPB])
		if code == "" {
			continue
		}
		if len(code) < 8 {
			code = fmt.Sprintf("%08s", code)
		}
		if len(code) != 8 {
			continue
		}
		name := strings.TrimSpace(row[colName])
		if name == "" {
			continue
		}
		r := STRRecord{ISPB: code, Name: name, SyncedAt: syncedAt}
		if colLegal >= 0 && colLegal < len(row) {
			r.LegalName = strings.TrimSpace(row[colLegal])
		}
		if colCompe >= 0 && colCompe < len(row) {
			compe := strings.TrimSpace(row[colCompe])
			if !strings.EqualFold(compe, "n/a") {
				r.CompeCode = compe
			}
		}
		if colParticipates >= 0 && colParticipates < len(row) {
			r.ParticipatesCompe = strings.EqualFold(strings.TrimSpace(row[colParticipates]), "sim")
		}
		if colAccess >= 0 && colAccess < len(row) {
			r.AccessType = strings.TrimSpace(row[colAccess])
		}
		if colStart >= 0 && colStart < len(row) {
			if t, err := time.Parse("02/01/2006", strings.TrimSpace(row[colStart])); err == nil {
				r.OperationStart = t
			}
		}
		out = append(out, r)
	}
	return out, nil
}

func strColumn(header []string, name string) int {
	for i, col := range header {
		if strings.EqualFold(strings.TrimSpace(col), name) {
			return i
		}
	}
	return -1
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/ispb/... -v`
Expected: PASS (all `TestValidateISPB*`, `TestParseSTR*`, `TestDownloadSTR*` subtests)

- [ ] **Step 5: Mark task complete**

---

### Task 3: Pix source (ported from shield_ispb)

**Files:**
- Create: `internal/ispb/pix.go`
- Test: `internal/ispb/pix_test.go`

**Interfaces:**
- Consumes: `ValidateISPB(code string) error` (Task 1).
- Produces: `ispb.PixConfig`, `ispb.DefaultPixConfig() PixConfig`, `ispb.PixRecord{ISPB, Name, CNPJ, Authorized, SyncedAt}`, `(PixRecord) Validate() error`, `ispb.DownloadPix(ctx context.Context, cfg PixConfig, log *slog.Logger) ([]byte, url string, error)`, `ispb.ParsePix(data []byte, cfg PixConfig, syncedAt time.Time) ([]PixRecord, error)`.

Ported from `C:\weaver-sync\dell\projects\eligibility\apps\shield_ispb\internal\bacensync\{downloader,parser,config}.go`, with two fixes over the original: (1) the empty-ISPB zero-pad bug described in Task 2, applied here too; (2) `resp.Body.Close()` called explicitly per loop iteration instead of `defer`red inside the loop (the original's `defer` inside a `for` only runs when the enclosing function returns, leaking every failed iteration's response body until the whole download completes).

- [ ] **Step 1: Write the failing tests**

```go
// internal/ispb/pix_test.go
package ispb

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// pixFixture uses Windows-1252 bytes: \xE3 = ã, \xC9 = É.
var pixFixture = []byte(
	"Lista de participantes em ades\xE3o ao Pix\n" +
		";ISPB;Nome Reduzido;CNPJ;Autorizada pelo BCB\n" +
		"1;00000000;BCO DO BRASIL S.A.;00000000000191;Sim\n" +
		"2;00204963;COOPERATIVA CR\xC9DITO;00204963000110;Sim\n" +
		"3;38166;BACEN;38166000105;Nao\n" +
		"4;;SEM ISPB;12345678000100;Sim\n")

func TestParsePix(t *testing.T) {
	synced := time.Date(2026, 7, 3, 0, 0, 0, 0, time.UTC)
	records, err := ParsePix(pixFixture, DefaultPixConfig(), synced)
	require.NoError(t, err)
	require.Len(t, records, 3, "4 data rows minus 1 skipped empty-ISPB row")

	brasil := records[0]
	assert.Equal(t, "00000000", brasil.ISPB)
	assert.Equal(t, "BCO DO BRASIL S.A.", brasil.Name)
	assert.Equal(t, "00000000000191", brasil.CNPJ)
	assert.True(t, brasil.Authorized)
	assert.Equal(t, synced, brasil.SyncedAt)

	coop := records[1]
	assert.Equal(t, "COOPERATIVA CRÉDITO", coop.Name, "Windows-1252 0xC9 decodes to É")

	bacen := records[2]
	assert.Equal(t, "00038166", bacen.ISPB, "short code zero-padded to 8 digits")
	assert.False(t, bacen.Authorized, "Nao is not in AuthorizedValues")
}

func TestParsePix_NoDataRows(t *testing.T) {
	_, err := ParsePix([]byte(";ISPB;Nome Reduzido\n"), DefaultPixConfig(), time.Now())
	assert.Error(t, err)
}

func TestDownloadPix_ProbesDatesBackward(t *testing.T) {
	today := time.Now()
	validDate := today.AddDate(0, 0, -2).Format("20060102")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/pix-"+validDate+".csv" {
			w.WriteHeader(http.StatusOK)
			if r.Method == http.MethodGet {
				_, _ = w.Write(pixFixture)
			}
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	cfg := PixConfig{
		BaseURL:          srv.URL + "/pix-%s.csv",
		HTTPTimeout:      5 * time.Second,
		MaxDaysBack:      10,
		CSVDelimiter:     ';',
		ColumnISPB:       "ISPB",
		ColumnName:       "Nome Reduzido",
		ColumnCNPJ:       "CNPJ",
		ColumnAuthorized: "Autorizada pelo BCB",
		AuthorizedValues: []string{"sim"},
	}
	data, url, err := DownloadPix(context.Background(), cfg, nil)
	require.NoError(t, err)
	assert.Equal(t, pixFixture, data)
	assert.Equal(t, fmt.Sprintf("%s/pix-%s.csv", srv.URL, validDate), url)
}

func TestDownloadPix_ExhaustsMaxDaysBack(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	cfg := DefaultPixConfig()
	cfg.BaseURL = srv.URL + "/pix-%s.csv"
	cfg.MaxDaysBack = 2
	cfg.HTTPTimeout = 5 * time.Second

	_, _, err := DownloadPix(context.Background(), cfg, nil)
	assert.Error(t, err)
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/ispb/... -run TestParsePix -v` and `go test ./internal/ispb/... -run TestDownloadPix -v`
Expected: FAIL — `undefined: ParsePix`, `undefined: DownloadPix`, `undefined: PixConfig`

- [ ] **Step 3: Write the implementation**

```go
// internal/ispb/pix.go
package ispb

import (
	"bytes"
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"time"

	"golang.org/x/text/encoding/charmap"
	"golang.org/x/text/transform"
)

// PixConfig configures the BACEN Pix-participants CSV download. BACEN
// publishes a dated file daily, but not every date has one, so the client
// probes dates backward from today.
type PixConfig struct {
	BaseURL          string // fmt template with one %s for YYYYMMDD
	HTTPTimeout      time.Duration
	MaxDaysBack      int
	CSVDelimiter     rune
	ColumnISPB       string
	ColumnName       string
	ColumnCNPJ       string
	ColumnAuthorized string
	AuthorizedValues []string
}

// DefaultPixConfig returns the standard BACEN Pix-participants CSV settings.
func DefaultPixConfig() PixConfig {
	return PixConfig{
		BaseURL:          "https://www.bcb.gov.br/content/estabilidadefinanceira/participantes_pix/lista-participantes-instituicoes-em-adesao-pix-%s.csv",
		HTTPTimeout:      30 * time.Second,
		MaxDaysBack:      60,
		CSVDelimiter:     ';',
		ColumnISPB:       "ISPB",
		ColumnName:       "Nome Reduzido",
		ColumnCNPJ:       "CNPJ",
		ColumnAuthorized: "Autorizada pelo BCB",
		AuthorizedValues: []string{"sim", "yes", "s", "1"},
	}
}

// PixRecord is one row of the BACEN Pix-participants CSV: Pix adherents only,
// with a BCB-authorization flag the STR registry doesn't carry.
type PixRecord struct {
	ISPB       string
	Name       string
	CNPJ       string
	Authorized bool
	SyncedAt   time.Time
}

// Validate checks that ISPB is exactly 8 digits.
func (r PixRecord) Validate() error {
	return ValidateISPB(r.ISPB)
}

// DownloadPix fetches the latest available BACEN Pix-participants CSV,
// probing dates backward from today, and returns the raw CSV bytes and the
// URL that succeeded. log may be nil.
func DownloadPix(ctx context.Context, cfg PixConfig, log *slog.Logger) ([]byte, string, error) {
	if log == nil {
		log = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	client := &http.Client{Timeout: cfg.HTTPTimeout}
	now := time.Now()
	for i := 0; i <= cfg.MaxDaysBack; i++ {
		dateStr := now.AddDate(0, 0, -i).Format("20060102")
		url := fmt.Sprintf(cfg.BaseURL, dateStr)

		headReq, err := http.NewRequestWithContext(ctx, http.MethodHead, url, nil)
		if err != nil {
			continue
		}
		resp, err := client.Do(headReq)
		if err != nil {
			log.Debug("ispb: pix probe failed", "url", url, "err", err)
			continue
		}
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			continue
		}

		getReq, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			continue
		}
		resp, err = client.Do(getReq)
		if err != nil {
			log.Debug("ispb: pix fetch failed", "url", url, "err", err)
			continue
		}
		if resp.StatusCode != http.StatusOK {
			_ = resp.Body.Close()
			continue
		}
		body, err := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if err != nil {
			return nil, "", fmt.Errorf("read body: %w", err)
		}
		log.Info("ispb: downloaded Pix participants", "url", url, "bytes", len(body))
		return body, url, nil
	}
	return nil, "", fmt.Errorf("no BACEN Pix-participants CSV found within last %d days", cfg.MaxDaysBack)
}

// ParsePix decodes the Windows-1252 BACEN Pix-participants CSV into
// PixRecords. syncedAt is stamped on every returned record.
func ParsePix(data []byte, cfg PixConfig, syncedAt time.Time) ([]PixRecord, error) {
	decoder := charmap.Windows1252.NewDecoder()
	utf8Reader := transform.NewReader(bytes.NewReader(data), decoder)
	reader := csv.NewReader(utf8Reader)
	reader.Comma = cfg.CSVDelimiter
	reader.LazyQuotes = true
	reader.FieldsPerRecord = -1

	records, err := reader.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("parse csv: %w", err)
	}
	if len(records) < 2 {
		return nil, fmt.Errorf("csv has no data rows (got %d lines)", len(records))
	}

	headerIdx := 0
	for i, row := range records {
		for _, col := range row {
			if strings.Contains(strings.ToLower(strings.TrimSpace(col)), strings.ToLower(cfg.ColumnISPB)) {
				headerIdx = i
				break
			}
		}
		if headerIdx > 0 {
			break
		}
	}
	records = records[headerIdx:]
	if len(records) < 2 {
		return nil, fmt.Errorf("csv has no data rows after header detection")
	}

	header := records[0]
	colISPB := pixColumn(header, cfg.ColumnISPB)
	colName := pixColumn(header, cfg.ColumnName)
	colCNPJ := pixColumn(header, cfg.ColumnCNPJ)
	colAuth := pixColumn(header, cfg.ColumnAuthorized)
	if colISPB < 0 || colName < 0 {
		return nil, fmt.Errorf("required columns not found (ISPB=%d, %s=%d)", colISPB, cfg.ColumnName, colName)
	}

	var out []PixRecord
	for _, row := range records[1:] {
		if len(row) <= colISPB || len(row) <= colName {
			continue
		}
		code := strings.TrimSpace(row[colISPB])
		if code == "" {
			continue
		}
		if len(code) < 8 {
			code = fmt.Sprintf("%08s", code)
		}
		if len(code) != 8 {
			continue
		}
		name := strings.TrimSpace(row[colName])
		if name == "" {
			continue
		}
		cnpj := ""
		if colCNPJ >= 0 && colCNPJ < len(row) {
			cnpj = strings.TrimSpace(row[colCNPJ])
		}
		authorized := true
		if colAuth >= 0 && colAuth < len(row) {
			val := strings.TrimSpace(strings.ToLower(row[colAuth]))
			authorized = slices.Contains(cfg.AuthorizedValues, val)
		}
		out = append(out, PixRecord{ISPB: code, Name: name, CNPJ: cnpj, Authorized: authorized, SyncedAt: syncedAt})
	}
	return out, nil
}

func pixColumn(header []string, name string) int {
	target := strings.ToLower(strings.TrimSpace(name))
	for i, col := range header {
		if strings.ToLower(strings.TrimSpace(col)) == target {
			return i
		}
	}
	return -1
}
```

- [ ] **Step 4: Run `go mod tidy` and verify tests pass**

Run: `go mod tidy`
Expected: `golang.org/x/text` moves from the `// indirect` block to the direct `require` block in `go.mod`.

Run: `go test ./internal/ispb/... -v`
Expected: PASS (all tests in the package, including Task 1 and Task 2's)

- [ ] **Step 5: Mark task complete**

---

### Task 4: Migration `0004_ispb_participant`

**Files:**
- Create: `internal/store/postgres/schema/0004_ispb_participant.up.sql`
- Create: `internal/store/postgres/schema/0004_ispb_participant.down.sql`

**Interfaces:**
- Produces: table `ispb_participant` (columns below), auto-embedded via existing `//go:embed schema/*.sql` (`internal/store/postgres/migrations.go`). No Go code in this task.

Nullable timestamp/date columns are avoided in favor of a `0001-01-01`
sentinel default — this makes `time.Time.IsZero()` in Go the direct signal
for "never synced by this source," with no nullable-scan handling needed in
Task 5, and matches the codebase's existing sentinel pattern
(`internal/store/postgres/stats.go:33`, `COALESCE(max(n), -1)`).

- [ ] **Step 1: Write the migration files**

```sql
-- internal/store/postgres/schema/0004_ispb_participant.up.sql
CREATE TABLE IF NOT EXISTS ispb_participant (
  ispb_code           VARCHAR(8)  PRIMARY KEY,
  institution_name    TEXT        NOT NULL,
  legal_name          TEXT        NOT NULL DEFAULT '',
  compe_code          TEXT        NOT NULL DEFAULT '',
  participates_compe  BOOLEAN     NOT NULL DEFAULT FALSE,
  access_type         TEXT        NOT NULL DEFAULT '',
  operation_start     DATE        NOT NULL DEFAULT '0001-01-01',
  str_synced_at       TIMESTAMPTZ NOT NULL DEFAULT '0001-01-01T00:00:00Z',
  cnpj                VARCHAR(14) NOT NULL DEFAULT '',
  pix_authorized      BOOLEAN     NOT NULL DEFAULT FALSE,
  pix_synced_at       TIMESTAMPTZ NOT NULL DEFAULT '0001-01-01T00:00:00Z',
  created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at          TIMESTAMPTZ NOT NULL DEFAULT now()
);
```

```sql
-- internal/store/postgres/schema/0004_ispb_participant.down.sql
DROP TABLE IF EXISTS ispb_participant;
```

- [ ] **Step 2: Verify the migration applies**

Requires a reachable throwaway Postgres (see `deploy/local-testdb.sh` if one isn't already running) and `PIXKB_TEST_DSN` set.

Run: `PIXKB_DSN=$PIXKB_TEST_DSN go run ./cmd/pixkb db up`
Expected: exits 0, no error. Then `PIXKB_DSN=$PIXKB_TEST_DSN go run ./cmd/pixkb db down` and `db up` again to confirm the down migration is also clean.

- [ ] **Step 3: Mark task complete**

---

### Task 5: Postgres store methods

**Files:**
- Create: `internal/store/postgres/ispb.go`
- Test: `internal/store/postgres/ispb_test.go`

**Interfaces:**
- Consumes: `ispb.STRRecord`, `ispb.PixRecord`, `ispb.Participant` (Tasks 1–3); table `ispb_participant` (Task 4); `s.pool` (existing `*Store` field), `testDSN(t)`/`applyTestSchema(t, dsn)` (existing test helpers).
- Produces: `(*Store) UpsertSTR(ctx, ispb.STRRecord) error`, `(*Store) UpsertPix(ctx, ispb.PixRecord) error`, `(*Store) GetISPB(ctx, code string) (ispb.Participant, error)`, `(*Store) ListISPB(ctx) ([]ispb.Participant, error)`, `(*Store) CountISPB(ctx) (int, error)`.

`UpsertPix` only overwrites `institution_name` while `str_synced_at` is still
at its zero-sentinel — i.e. Pix's name is authoritative until STR has synced
this ISPB at least once, after which STR's name sticks permanently. This
implements the spec's "STR wins deterministically" rule precisely (a plain
`COALESCE` would let the *first* writer win regardless of source, which is
not the same thing).

- [ ] **Step 1: Write the failing tests**

```go
// internal/store/postgres/ispb_test.go
package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"pixkb/internal/ispb"
)

func truncateISPB(t *testing.T, s *Store) {
	t.Helper()
	_, err := s.pool.Exec(context.Background(), "TRUNCATE ispb_participant")
	require.NoError(t, err)
}

func TestUpsertSTR_ThenGet(t *testing.T) {
	dsn := testDSN(t)
	ctx := context.Background()
	applyTestSchema(t, dsn)
	s, err := Open(ctx, dsn)
	require.NoError(t, err)
	defer s.Close()
	truncateISPB(t, s)

	synced := time.Date(2026, 7, 3, 0, 0, 0, 0, time.UTC)
	rec := ispb.STRRecord{
		ISPB: "00000208", Name: "BRB - BCO DE BRASILIA S.A.", LegalName: "BRB - BANCO DE BRASILIA S.A.",
		CompeCode: "070", ParticipatesCompe: true, AccessType: "RSFN",
		OperationStart: time.Date(2002, 4, 22, 0, 0, 0, 0, time.UTC), SyncedAt: synced,
	}
	require.NoError(t, s.UpsertSTR(ctx, rec))

	got, err := s.GetISPB(ctx, "00000208")
	require.NoError(t, err)
	assert.Equal(t, rec.Name, got.Name)
	assert.Equal(t, rec.LegalName, got.LegalName)
	assert.Equal(t, rec.CompeCode, got.CompeCode)
	assert.True(t, got.ParticipatesCompe)
	assert.Equal(t, rec.AccessType, got.AccessType)
	assert.True(t, got.STRSyncedAt.Equal(synced))
	assert.True(t, got.PixSyncedAt.IsZero(), "never Pix-synced")
}

func TestUpsertPix_DoesNotOverwriteSTRName(t *testing.T) {
	dsn := testDSN(t)
	ctx := context.Background()
	applyTestSchema(t, dsn)
	s, err := Open(ctx, dsn)
	require.NoError(t, err)
	defer s.Close()
	truncateISPB(t, s)

	require.NoError(t, s.UpsertSTR(ctx, ispb.STRRecord{
		ISPB: "00000208", Name: "STR CANONICAL NAME", SyncedAt: time.Now(),
	}))
	require.NoError(t, s.UpsertPix(ctx, ispb.PixRecord{
		ISPB: "00000208", Name: "PIX NAME VARIANT", CNPJ: "00000208000100", Authorized: true, SyncedAt: time.Now(),
	}))

	got, err := s.GetISPB(ctx, "00000208")
	require.NoError(t, err)
	assert.Equal(t, "STR CANONICAL NAME", got.Name, "STR name must survive a later Pix upsert")
	assert.Equal(t, "00000208000100", got.CNPJ)
	assert.True(t, got.PixAuthorized)
	assert.False(t, got.PixSyncedAt.IsZero())
}

func TestUpsertPix_NameUsedUntilSTRSyncs(t *testing.T) {
	dsn := testDSN(t)
	ctx := context.Background()
	applyTestSchema(t, dsn)
	s, err := Open(ctx, dsn)
	require.NoError(t, err)
	defer s.Close()
	truncateISPB(t, s)

	require.NoError(t, s.UpsertPix(ctx, ispb.PixRecord{ISPB: "00000208", Name: "PIX ONLY NAME", SyncedAt: time.Now()}))
	got, err := s.GetISPB(ctx, "00000208")
	require.NoError(t, err)
	assert.Equal(t, "PIX ONLY NAME", got.Name, "Pix name is authoritative before any STR sync")
	assert.True(t, got.STRSyncedAt.IsZero())
}

func TestGetISPB_NotFound(t *testing.T) {
	dsn := testDSN(t)
	ctx := context.Background()
	applyTestSchema(t, dsn)
	s, err := Open(ctx, dsn)
	require.NoError(t, err)
	defer s.Close()
	truncateISPB(t, s)

	_, err = s.GetISPB(ctx, "99999999")
	require.Error(t, err)
	assert.True(t, errors.Is(err, pgx.ErrNoRows))
}

func TestListISPB_And_CountISPB(t *testing.T) {
	dsn := testDSN(t)
	ctx := context.Background()
	applyTestSchema(t, dsn)
	s, err := Open(ctx, dsn)
	require.NoError(t, err)
	defer s.Close()
	truncateISPB(t, s)

	require.NoError(t, s.UpsertSTR(ctx, ispb.STRRecord{ISPB: "00000208", Name: "B", SyncedAt: time.Now()}))
	require.NoError(t, s.UpsertSTR(ctx, ispb.STRRecord{ISPB: "00000001", Name: "A", SyncedAt: time.Now()}))

	n, err := s.CountISPB(ctx)
	require.NoError(t, err)
	assert.Equal(t, 2, n)

	list, err := s.ListISPB(ctx)
	require.NoError(t, err)
	require.Len(t, list, 2)
	assert.Equal(t, "00000001", list[0].ISPB, "ordered by ispb_code")
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/store/postgres/... -run TestUpsertSTR -v` (requires `PIXKB_TEST_DSN`; skips cleanly under `-short` or when unset)
Expected: FAIL — `undefined: Store.UpsertSTR` (compile error) once `PIXKB_TEST_DSN` is set, or SKIP if it's not.

- [ ] **Step 3: Write the implementation**

```go
// internal/store/postgres/ispb.go
package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"pixkb/internal/ispb"
)

// UpsertSTR inserts or updates a participant from the STR (canonical)
// registry.
func (s *Store) UpsertSTR(ctx context.Context, r ispb.STRRecord) error {
	if err := r.Validate(); err != nil {
		return err
	}
	const q = `
INSERT INTO ispb_participant
  (ispb_code, institution_name, legal_name, compe_code, participates_compe,
   access_type, operation_start, str_synced_at, updated_at)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8, now())
ON CONFLICT (ispb_code) DO UPDATE SET
  institution_name   = EXCLUDED.institution_name,
  legal_name          = EXCLUDED.legal_name,
  compe_code          = EXCLUDED.compe_code,
  participates_compe  = EXCLUDED.participates_compe,
  access_type         = EXCLUDED.access_type,
  operation_start     = EXCLUDED.operation_start,
  str_synced_at       = EXCLUDED.str_synced_at,
  updated_at          = now()`
	_, err := s.pool.Exec(ctx, q,
		r.ISPB, r.Name, r.LegalName, r.CompeCode, r.ParticipatesCompe,
		r.AccessType, r.OperationStart, r.SyncedAt)
	if err != nil {
		return fmt.Errorf("upsert str participant %s: %w", r.ISPB, err)
	}
	return nil
}

// UpsertPix inserts or updates a participant from the Pix-participants
// registry. institution_name is only overwritten while str_synced_at is
// still at its zero-sentinel — once STR has synced this ISPB, its name
// sticks regardless of later Pix syncs.
func (s *Store) UpsertPix(ctx context.Context, r ispb.PixRecord) error {
	if err := r.Validate(); err != nil {
		return err
	}
	const q = `
INSERT INTO ispb_participant
  (ispb_code, institution_name, cnpj, pix_authorized, pix_synced_at, updated_at)
VALUES ($1,$2,$3,$4,$5, now())
ON CONFLICT (ispb_code) DO UPDATE SET
  institution_name = CASE
    WHEN ispb_participant.str_synced_at = '0001-01-01T00:00:00Z'::timestamptz
      THEN EXCLUDED.institution_name
    ELSE ispb_participant.institution_name
  END,
  cnpj           = EXCLUDED.cnpj,
  pix_authorized = EXCLUDED.pix_authorized,
  pix_synced_at  = EXCLUDED.pix_synced_at,
  updated_at     = now()`
	_, err := s.pool.Exec(ctx, q, r.ISPB, r.Name, r.CNPJ, r.Authorized, r.SyncedAt)
	if err != nil {
		return fmt.Errorf("upsert pix participant %s: %w", r.ISPB, err)
	}
	return nil
}

const ispbSelectCols = `ispb_code, institution_name, legal_name, compe_code, participates_compe,
	access_type, operation_start, str_synced_at, cnpj, pix_authorized, pix_synced_at`

func scanParticipant(row pgx.Row) (ispb.Participant, error) {
	var p ispb.Participant
	err := row.Scan(&p.ISPB, &p.Name, &p.LegalName, &p.CompeCode, &p.ParticipatesCompe,
		&p.AccessType, &p.OperationStart, &p.STRSyncedAt, &p.CNPJ, &p.PixAuthorized, &p.PixSyncedAt)
	return p, err
}

// GetISPB looks up a single participant by its 8-digit ISPB code.
func (s *Store) GetISPB(ctx context.Context, code string) (ispb.Participant, error) {
	if err := ispb.ValidateISPB(code); err != nil {
		return ispb.Participant{}, err
	}
	row := s.pool.QueryRow(ctx, "SELECT "+ispbSelectCols+" FROM ispb_participant WHERE ispb_code = $1", code)
	p, err := scanParticipant(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return ispb.Participant{}, fmt.Errorf("ispb %s: %w", code, pgx.ErrNoRows)
	}
	if err != nil {
		return ispb.Participant{}, fmt.Errorf("get ispb %s: %w", code, err)
	}
	return p, nil
}

// ListISPB returns every participant, ordered by ISPB code.
func (s *Store) ListISPB(ctx context.Context) ([]ispb.Participant, error) {
	rows, err := s.pool.Query(ctx, "SELECT "+ispbSelectCols+" FROM ispb_participant ORDER BY ispb_code")
	if err != nil {
		return nil, fmt.Errorf("list ispb: %w", err)
	}
	defer rows.Close()
	var out []ispb.Participant
	for rows.Next() {
		p, err := scanParticipant(rows)
		if err != nil {
			return nil, fmt.Errorf("scan ispb row: %w", err)
		}
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate ispb rows: %w", err)
	}
	return out, nil
}

// CountISPB returns the number of stored participants.
func (s *Store) CountISPB(ctx context.Context) (int, error) {
	var n int
	if err := s.pool.QueryRow(ctx, "SELECT count(*) FROM ispb_participant").Scan(&n); err != nil {
		return 0, fmt.Errorf("count ispb: %w", err)
	}
	return n, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/store/postgres/... -run 'TestUpsertSTR|TestUpsertPix|TestGetISPB|TestListISPB' -v`
Expected: PASS (5 tests). If `PIXKB_TEST_DSN` is unset they SKIP — set it to a throwaway database first (see `deploy/local-testdb.sh`).

- [ ] **Step 5: Mark task complete**

---

### Task 6: CLI — `pixkb ispb`

**Files:**
- Create: `cmd/pixkb/ispb.go`
- Modify: `cmd/pixkb/commands.go:18` — add `newISPBCmd()` to the `attachCommands` list
- Test: `cmd/pixkb/ispb_test.go`

**Interfaces:**
- Consumes: `loadConfig() Config`, `openStore(ctx, cfg) (*postgres.Store, error)`, `Config.MirrorDir` (`cmd/pixkb/config.go`); everything from Tasks 1, 2, 3, 5.
- Produces: `newISPBCmd() *cobra.Command`, registered on root as `pixkb ispb {str,pix,sync,lookup}`.

Command tree: `ispb str {fetch,load,sync}`, `ispb pix {fetch,load,sync}`
(mirrors the two independent sources), plus top-level `ispb sync` (both,
sequential, stops on first error) and `ispb lookup <code>` (offline, validates
the code format before touching the DB). No subcommand takes `--dsn` — this
is enforced by an automated test in this task (`TestNewISPBCmd_NoDSNFlag`),
not just documentation.

- [ ] **Step 1: Write the failing tests**

```go
// cmd/pixkb/ispb_test.go
package main

import (
	"bytes"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewISPBCmd_Wiring(t *testing.T) {
	t.Parallel()
	root := newISPBCmd()
	assert.Equal(t, "ispb", root.Use)

	names := map[string]bool{}
	for _, c := range root.Commands() {
		names[c.Name()] = true
	}
	for _, want := range []string{"str", "pix", "sync", "lookup"} {
		assert.True(t, names[want], "missing subcommand %q", want)
	}

	str, _, err := root.Find([]string{"str"})
	require.NoError(t, err)
	strNames := map[string]bool{}
	for _, c := range str.Commands() {
		strNames[c.Name()] = true
	}
	for _, want := range []string{"fetch", "load", "sync"} {
		assert.True(t, strNames[want], "missing str subcommand %q", want)
	}

	pix, _, err := root.Find([]string{"pix"})
	require.NoError(t, err)
	pixNames := map[string]bool{}
	for _, c := range pix.Commands() {
		pixNames[c.Name()] = true
	}
	for _, want := range []string{"fetch", "load", "sync"} {
		assert.True(t, pixNames[want], "missing pix subcommand %q", want)
	}
}

// TestNewISPBCmd_NoDSNFlag guards the project rule that the DSN must come
// from config/env only — no ispb subcommand may expose a --dsn flag.
func TestNewISPBCmd_NoDSNFlag(t *testing.T) {
	t.Parallel()
	var walk func(cmd *cobra.Command)
	walk = func(cmd *cobra.Command) {
		assert.Nilf(t, cmd.Flags().Lookup("dsn"), "%s must not have a --dsn flag", cmd.CommandPath())
		for _, c := range cmd.Commands() {
			walk(c)
		}
	}
	walk(newISPBCmd())
}

func TestISPBLookup_InvalidCode(t *testing.T) {
	t.Parallel()
	root := NewRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetArgs([]string{"ispb", "lookup", "bad"})
	err := root.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid ISPB code")
}

func TestISPBLookup_NoDSN(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("PIXKB_DSN", "")
	root := NewRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetArgs([]string{"ispb", "lookup", "00000208"})
	err := root.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no database DSN")
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./cmd/pixkb/... -run TestNewISPBCmd -v` and `go test ./cmd/pixkb/... -run TestISPBLookup -v`
Expected: FAIL — `undefined: newISPBCmd`

- [ ] **Step 3: Write the implementation**

```go
// cmd/pixkb/ispb.go
package main

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"pixkb/internal/ispb"
)

func newISPBCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "ispb",
		Short: "Map BACEN ISPB codes to SPB/Pix participant institutions",
	}
	cmd.AddCommand(newISPBSTRCmd(), newISPBPixCmd(), newISPBSyncCmd(), newISPBLookupCmd())
	return cmd
}

func ispbLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, nil))
}

func defaultISPBPath(cfg Config, name string) string {
	return filepath.Join(cfg.MirrorDir, "bacen-ispb", name)
}

func newISPBSTRCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "str",
		Short: "STR participants source (canonical, all SPB participants)",
	}
	cmd.AddCommand(newISPBSTRFetchCmd(), newISPBSTRLoadCmd(), newISPBSTRSyncCmd())
	return cmd
}

func newISPBSTRFetchCmd() *cobra.Command {
	var out string
	cmd := &cobra.Command{
		Use:   "fetch",
		Short: "Download the STR participants CSV and stage it to a file",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg := loadConfig()
			path := out
			if path == "" {
				path = defaultISPBPath(cfg, "str-participants.csv")
			}
			data, err := ispb.DownloadSTR(cmd.Context(), ispb.DefaultSTRConfig(), ispbLogger())
			if err != nil {
				return err
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
	cmd.Flags().StringVar(&out, "out", "", "output path (default: <mirror_dir>/bacen-ispb/str-participants.csv)")
	return cmd
}

func newISPBSTRLoadCmd() *cobra.Command {
	var file string
	cmd := &cobra.Command{
		Use:   "load",
		Short: "Parse a staged STR participants CSV and upsert it into the database",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg := loadConfig()
			path := file
			if path == "" {
				path = defaultISPBPath(cfg, "str-participants.csv")
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return fmt.Errorf("read %s: %w", path, err)
			}
			records, err := ispb.ParseSTR(data, time.Now())
			if err != nil {
				return err
			}
			ctx := cmd.Context()
			st, err := openStore(ctx, cfg)
			if err != nil {
				return err
			}
			defer st.Close()
			for _, r := range records {
				if err := st.UpsertSTR(ctx, r); err != nil {
					return err
				}
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "loaded %d STR participants\n", len(records))
			return nil
		},
	}
	cmd.Flags().StringVar(&file, "file", "", "input path (default: <mirror_dir>/bacen-ispb/str-participants.csv)")
	return cmd
}

func newISPBSTRSyncCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "sync",
		Short: "Download and load the STR participants CSV in one step",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg := loadConfig()
			ctx := cmd.Context()
			data, err := ispb.DownloadSTR(ctx, ispb.DefaultSTRConfig(), ispbLogger())
			if err != nil {
				return err
			}
			records, err := ispb.ParseSTR(data, time.Now())
			if err != nil {
				return err
			}
			st, err := openStore(ctx, cfg)
			if err != nil {
				return err
			}
			defer st.Close()
			for _, r := range records {
				if err := st.UpsertSTR(ctx, r); err != nil {
					return err
				}
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "synced %d STR participants\n", len(records))
			return nil
		},
	}
}

func newISPBPixCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "pix",
		Short: "Pix participants source (Pix adherents, BCB-authorization flag)",
	}
	cmd.AddCommand(newISPBPixFetchCmd(), newISPBPixLoadCmd(), newISPBPixSyncCmd())
	return cmd
}

func newISPBPixFetchCmd() *cobra.Command {
	var out string
	var maxDaysBack int
	cmd := &cobra.Command{
		Use:   "fetch",
		Short: "Download the Pix participants CSV and stage it to a file",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg := loadConfig()
			path := out
			if path == "" {
				path = defaultISPBPath(cfg, "pix-participants.csv")
			}
			pcfg := ispb.DefaultPixConfig()
			if maxDaysBack > 0 {
				pcfg.MaxDaysBack = maxDaysBack
			}
			data, _, err := ispb.DownloadPix(cmd.Context(), pcfg, ispbLogger())
			if err != nil {
				return err
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
	cmd.Flags().StringVar(&out, "out", "", "output path (default: <mirror_dir>/bacen-ispb/pix-participants.csv)")
	cmd.Flags().IntVar(&maxDaysBack, "max-days-back", 0, "days to probe backward for a dated CSV (default: 60)")
	return cmd
}

func newISPBPixLoadCmd() *cobra.Command {
	var file string
	cmd := &cobra.Command{
		Use:   "load",
		Short: "Parse a staged Pix participants CSV and upsert it into the database",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg := loadConfig()
			path := file
			if path == "" {
				path = defaultISPBPath(cfg, "pix-participants.csv")
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return fmt.Errorf("read %s: %w", path, err)
			}
			records, err := ispb.ParsePix(data, ispb.DefaultPixConfig(), time.Now())
			if err != nil {
				return err
			}
			ctx := cmd.Context()
			st, err := openStore(ctx, cfg)
			if err != nil {
				return err
			}
			defer st.Close()
			for _, r := range records {
				if err := st.UpsertPix(ctx, r); err != nil {
					return err
				}
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "loaded %d Pix participants\n", len(records))
			return nil
		},
	}
	cmd.Flags().StringVar(&file, "file", "", "input path (default: <mirror_dir>/bacen-ispb/pix-participants.csv)")
	return cmd
}

func newISPBPixSyncCmd() *cobra.Command {
	var maxDaysBack int
	cmd := &cobra.Command{
		Use:   "sync",
		Short: "Download and load the Pix participants CSV in one step",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg := loadConfig()
			ctx := cmd.Context()
			pcfg := ispb.DefaultPixConfig()
			if maxDaysBack > 0 {
				pcfg.MaxDaysBack = maxDaysBack
			}
			data, _, err := ispb.DownloadPix(ctx, pcfg, ispbLogger())
			if err != nil {
				return err
			}
			records, err := ispb.ParsePix(data, pcfg, time.Now())
			if err != nil {
				return err
			}
			st, err := openStore(ctx, cfg)
			if err != nil {
				return err
			}
			defer st.Close()
			for _, r := range records {
				if err := st.UpsertPix(ctx, r); err != nil {
					return err
				}
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "synced %d Pix participants\n", len(records))
			return nil
		},
	}
	cmd.Flags().IntVar(&maxDaysBack, "max-days-back", 0, "days to probe backward for a dated CSV (default: 60)")
	return cmd
}

func newISPBSyncCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "sync",
		Short: "Download and load both STR and Pix participant sources",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := newISPBSTRSyncCmd().RunE(cmd, args); err != nil {
				return fmt.Errorf("str sync: %w", err)
			}
			if err := newISPBPixSyncCmd().RunE(cmd, args); err != nil {
				return fmt.Errorf("pix sync: %w", err)
			}
			return nil
		},
	}
}

func newISPBLookupCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "lookup <ispb-code>",
		Short: "Look up a participant by its 8-digit ISPB code",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			code := args[0]
			if err := ispb.ValidateISPB(code); err != nil {
				return err
			}
			cfg := loadConfig()
			ctx := cmd.Context()
			st, err := openStore(ctx, cfg)
			if err != nil {
				return err
			}
			defer st.Close()
			p, err := st.GetISPB(ctx, code)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			_, _ = fmt.Fprintf(out, "ISPB:          %s\n", p.ISPB)
			_, _ = fmt.Fprintf(out, "Name:          %s\n", p.Name)
			if p.LegalName != "" {
				_, _ = fmt.Fprintf(out, "Legal name:    %s\n", p.LegalName)
			}
			if p.CompeCode != "" {
				_, _ = fmt.Fprintf(out, "COMPE code:    %s\n", p.CompeCode)
			}
			if !p.STRSyncedAt.IsZero() {
				_, _ = fmt.Fprintf(out, "Participates COMPE: %t\n", p.ParticipatesCompe)
				_, _ = fmt.Fprintf(out, "Access type:   %s\n", p.AccessType)
				_, _ = fmt.Fprintf(out, "STR synced:    %s\n", p.STRSyncedAt.Format(time.RFC3339))
			}
			if !p.PixSyncedAt.IsZero() {
				_, _ = fmt.Fprintf(out, "CNPJ:          %s\n", p.CNPJ)
				_, _ = fmt.Fprintf(out, "Pix authorized: %t\n", p.PixAuthorized)
				_, _ = fmt.Fprintf(out, "Pix synced:    %s\n", p.PixSyncedAt.Format(time.RFC3339))
			}
			return nil
		},
	}
}
```

Now wire it into `attachCommands`:

```go
// cmd/pixkb/commands.go:18 — before:
func attachCommands(root *cobra.Command) {
	root.AddCommand(newIngestCmd(), newSearchCmd(), newReindexCmd(), newDiffCmd(), newStatsCmd(), newRelatedCmd(), newAgentsCmd(), newConceptCmd(), newMCPCmd(), newHygieneCmd(), newCurateCmd(), newQRCmd(), newAskCmd())
}
```

```go
// cmd/pixkb/commands.go:18 — after:
func attachCommands(root *cobra.Command) {
	root.AddCommand(newIngestCmd(), newSearchCmd(), newReindexCmd(), newDiffCmd(), newStatsCmd(), newRelatedCmd(), newAgentsCmd(), newConceptCmd(), newMCPCmd(), newHygieneCmd(), newCurateCmd(), newQRCmd(), newAskCmd(), newISPBCmd())
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./cmd/pixkb/... -v`
Expected: PASS (all tests in the package, including pre-existing ones — confirms the wiring didn't break anything)

- [ ] **Step 5: Build the whole module and run the full test suite**

Run: `go build ./...`
Expected: exits 0, no errors.

Run: `go test ./... -short`
Expected: PASS (Postgres integration tests SKIP under `-short`; everything else runs).

If `PIXKB_TEST_DSN` is available, also run: `go test ./... -p 1`
Expected: PASS (full suite including Postgres integration tests, run serially per existing convention).

- [ ] **Step 6: Mark task complete**

---

## Manual smoke test (optional, requires network + a real Postgres)

```bash
go run ./cmd/pixkb db up
go run ./cmd/pixkb ispb str sync
go run ./cmd/pixkb ispb pix sync
go run ./cmd/pixkb ispb lookup 00000000   # Banco do Brasil — should show both STR and Pix fields
```
