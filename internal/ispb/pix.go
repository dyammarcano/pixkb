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
			cnpj = digitsOnly(row[colCNPJ])
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

// digitsOnly strips everything but ASCII digits from s. BACEN's live Pix CSV
// formats CNPJ with punctuation (e.g. "24.313.102/0001-25", 18 chars); a CNPJ
// is inherently a 14-digit number, so the digits-only form is the correct
// normalized representation, not a lossy one, and fits the schema's
// cnpj VARCHAR(14) column.
func digitsOnly(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}
