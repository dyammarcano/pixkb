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
