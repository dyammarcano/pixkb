package ingest

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"

	"github.com/xuri/excelize/v2"

	"pixkb/internal/okf"
)

// maxXlsxRows caps the data rows rendered per sheet so a large spreadsheet cannot
// create a multi-megabyte concept; a note is appended when a sheet is truncated.
const maxXlsxRows = 500

type xlsxSource struct{ files []string }

// NewXlsxSource builds a Source that renders each non-empty sheet of an .xlsx
// workbook as a Markdown table in a Reference concept. Offline (excelize reads
// local files, pure Go).
func NewXlsxSource(files []string) Source { return &xlsxSource{files: files} }

func (s *xlsxSource) Name() string { return "xlsx" }

func (s *xlsxSource) Fetch(_ context.Context) ([]okf.Concept, error) {
	var out []okf.Concept
	for _, f := range s.files {
		fx, err := excelize.OpenFile(f)
		if err != nil {
			return nil, fmt.Errorf("xlsx %s: %w", f, err)
		}
		base := filepath.Base(f)
		slug := slugify(strings.TrimSuffix(base, filepath.Ext(f)))
		emitted := 0
		for idx, sheet := range fx.GetSheetList() {
			rows, err := fx.GetRows(sheet)
			if err != nil {
				_ = fx.Close()
				return nil, fmt.Errorf("xlsx %s sheet %q: %w", f, sheet, err)
			}
			table, ok := renderXlsxTable(rows)
			if !ok {
				continue
			}
			title := base + " — " + sheet
			body := "# " + title + "\n\n" + table
			sheetSlug := slugify(sheet)
			out = append(out, okf.Concept{
				// Prefix the sheet index so distinct sheet names that slugify to
				// the same value (or to "" for all-non-ASCII names) cannot collide
				// on ID and silently overwrite each other on upsert.
				ID:          fmt.Sprintf("reference/%s/%02d-%s.md", slug, idx, sheetSlug),
				Type:        "Reference",
				Title:       title,
				Description: fmt.Sprintf("Spreadsheet %s, sheet %s", base, sheet),
				Resource:    f,
				Tags:        []string{"xlsx", slug, sheetSlug},
				Language:    detectMarkdownLang(table),
				SourceURI:   fmt.Sprintf("xlsx:%s#%s", base, sheet),
				Body:        body,
				ContentSHA:  okf.ComputeSHA(body),
			})
			emitted++
		}
		_ = fx.Close()
		if emitted == 0 {
			slog.Warn("xlsx: no non-empty sheets", "path", f)
		}
	}
	return out, nil
}

// renderXlsxTable renders rows as a GitHub-flavored Markdown table (first row =
// header). ok=false for an empty sheet. Ragged rows are padded; '|' and newlines
// in cells are escaped/flattened; at most maxXlsxRows data rows render (a note is
// appended when truncated).
func renderXlsxTable(rows [][]string) (string, bool) {
	width := 0
	for _, r := range rows {
		if len(r) > width {
			width = len(r)
		}
	}
	if width == 0 {
		return "", false
	}
	cell := func(r []string, i int) string {
		v := ""
		if i < len(r) {
			v = r[i]
		}
		v = strings.ReplaceAll(v, "|", `\|`)
		v = strings.ReplaceAll(v, "\n", " ")
		if v == "" {
			v = " "
		}
		return v
	}
	row := func(r []string) string {
		var b strings.Builder
		b.WriteString("|")
		for i := 0; i < width; i++ {
			b.WriteString(" " + cell(r, i) + " |")
		}
		b.WriteString("\n")
		return b.String()
	}
	var b strings.Builder
	b.WriteString(row(rows[0]))
	b.WriteString("|")
	for i := 0; i < width; i++ {
		b.WriteString(" --- |")
	}
	b.WriteString("\n")
	data := rows[1:]
	truncated := len(data) > maxXlsxRows
	if truncated {
		data = data[:maxXlsxRows]
	}
	for _, r := range data {
		b.WriteString(row(r))
	}
	if truncated {
		fmt.Fprintf(&b, "\n_… (%d more rows omitted)_\n", len(rows)-1-maxXlsxRows)
	}
	return b.String(), true
}
