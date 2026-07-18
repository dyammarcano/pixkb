# docx + xlsx ingest sources Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development. Steps use checkbox (`- [ ]`) syntax.

**Goal:** Two new `ingest.Source` adapters — `NewDocxSource` (stdlib zip+xml) and `NewXlsxSource` (excelize) — emitting `Reference` concepts, wired into config/buildSources like `pdfs:`/`markdown:`.

**Architecture:** `internal/ingest/docx.go` + `internal/ingest/xlsx.go` mirror `markdown.go`; config keys `docx:`/`xlsx:`; `github.com/xuri/excelize/v2` added for xlsx. Reuses the package's `section` struct, `slugify`, `firstLine`, `cleanTitle`, `detectMarkdownLang`, `okf.ComputeSHA`.

**Tech Stack:** Go, `archive/zip`, `encoding/xml`, `github.com/xuri/excelize/v2` (pure-Go), testify.

## Global Constraints

- **Air-gap:** both sources read LOCAL files only; excelize is pure-Go (no native runtime). No network at ingest.
- **Match the adapter pattern** (`markdown.go`): `Reference` type, `Tags [kind, slug]`, `Body = "# " + title + "\n\n" + text`, `SourceURI`/`ID` conventions, `ContentSHA = okf.ComputeSHA(body)`, `Language = detectMarkdownLang(body)`. Reuse the existing `section` struct (defined in `pdf.go`) and helpers — do NOT redefine them.
- **No change** to md/pdf/other sources, search, ranking, or `okf.Concept` types.
- **LF; explicit `git add` paths; no AI attribution; conventional commits; scripts to `.scripts/`.**
- Spec is binding: `docs/superpowers/specs/2026-07-18-docx-xlsx-ingest-design.md`.

---

### Task 1: docx source (stdlib zip + xml)

**Files:**
- Create: `internal/ingest/docx.go`, `internal/ingest/docx_test.go`

**Interfaces:**
- Produces: `func NewDocxSource(files []string) Source` (`Name()` = `"docx"`).

- [ ] **Step 1: Write the failing test** `internal/ingest/docx_test.go`:

```go
package ingest

import (
	"archive/zip"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// writeTestDocx builds a minimal .docx (zip with word/document.xml) from
// (style, text) paragraphs. An empty style means a body paragraph.
func writeTestDocx(t *testing.T, paras [][2]string) string {
	t.Helper()
	var b strings.Builder
	b.WriteString(`<?xml version="1.0"?><w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body>`)
	for _, p := range paras {
		b.WriteString("<w:p>")
		if p[0] != "" {
			b.WriteString(`<w:pPr><w:pStyle w:val="` + p[0] + `"/></w:pPr>`)
		}
		b.WriteString(`<w:r><w:t>` + p[1] + `</w:t></w:r></w:p>`)
	}
	b.WriteString(`</w:body></w:document>`)

	path := filepath.Join(t.TempDir(), "test.docx")
	f, err := os.Create(path)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()
	zw := zip.NewWriter(f)
	ct, err := zw.Create("[Content_Types].xml")
	require.NoError(t, err)
	_, _ = ct.Write([]byte(`<?xml version="1.0"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"/>`))
	dw, err := zw.Create("word/document.xml")
	require.NoError(t, err)
	_, _ = dw.Write([]byte(b.String()))
	require.NoError(t, zw.Close())
	return path
}

func TestDocxSource_HeadingSections(t *testing.T) {
	path := writeTestDocx(t, [][2]string{
		{"Heading1", "Visão Geral do Pix"},
		{"", "O Pix é o meio de pagamento instantâneo brasileiro."},
		{"Heading1", "Segurança"},
		{"", "A MED trata fraudes e devoluções."},
	})
	cs, err := NewDocxSource([]string{path}).Fetch(context.Background())
	require.NoError(t, err)
	require.Len(t, cs, 2)
	require.Equal(t, "Reference", cs[0].Type)
	require.Equal(t, "Visão Geral do Pix", cs[0].Title)
	require.Contains(t, cs[0].Body, "meio de pagamento instantâneo")
	require.Subset(t, cs[0].Tags, []string{"docx", "test"})
	require.Contains(t, cs[0].ID, "reference/test/")
	require.NotEmpty(t, cs[0].ContentSHA)
	require.Equal(t, "Segurança", cs[1].Title)
}

func TestDocxSource_NoHeadings(t *testing.T) {
	path := writeTestDocx(t, [][2]string{
		{"", "Um documento sem títulos, só um parágrafo."},
	})
	cs, err := NewDocxSource([]string{path}).Fetch(context.Background())
	require.NoError(t, err)
	require.Len(t, cs, 1)
	require.Equal(t, "Overview", cs[0].Title)
	require.Contains(t, cs[0].Body, "sem títulos")
}

func TestDocxSource_MissingFile(t *testing.T) {
	_, err := NewDocxSource([]string{"does-not-exist.docx"}).Fetch(context.Background())
	require.Error(t, err)
}

func TestDocxSource_Name(t *testing.T) {
	require.Equal(t, "docx", NewDocxSource(nil).Name())
}
```

- [ ] **Step 2: Run** `go test ./internal/ingest/ -run Docx -v` → FAIL (undefined).

- [ ] **Step 3: Implement `internal/ingest/docx.go`:**

```go
package ingest

import (
	"archive/zip"
	"context"
	"encoding/xml"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"

	"pixkb/internal/okf"
)

type docxSource struct{ files []string }

// NewDocxSource builds a Source that extracts text from Word .docx files,
// splitting on heading-styled paragraphs into Reference concepts. Offline: reads
// local files only (a .docx is a zip of XML).
func NewDocxSource(files []string) Source { return &docxSource{files: files} }

func (s *docxSource) Name() string { return "docx" }

// docxDocument mirrors the read parts of word/document.xml. encoding/xml matches
// on local element/attr names, so the w: prefix needs no namespace handling.
type docxDocument struct {
	Body struct {
		Paras []docxPara `xml:"body>p"`
	} `xml:",any"`
}

type docxPara struct {
	StyleVal string   `xml:"pPr>pStyle>val,attr"`
	Texts    []string `xml:"r>t"`
}

func (p docxPara) text() string {
	return strings.TrimSpace(strings.Join(p.Texts, ""))
}

// isHeadingStyle reports whether a paragraph style names a Word heading (EN
// "Heading1".., PT "Título1".., or a document "Title").
func isHeadingStyle(val string) bool {
	v := strings.ToLower(val)
	return strings.HasPrefix(v, "heading") ||
		strings.HasPrefix(v, "título") || strings.HasPrefix(v, "titulo") ||
		v == "title"
}

func (s *docxSource) Fetch(_ context.Context) ([]okf.Concept, error) {
	var out []okf.Concept
	for _, f := range s.files {
		doc, err := readDocxDocument(f)
		if err != nil {
			return nil, fmt.Errorf("docx %s: %w", f, err)
		}
		secs := splitDocx(doc)
		if len(secs) == 0 {
			slog.Warn("docx: no text extracted", "path", f)
			continue
		}
		slug := slugify(strings.TrimSuffix(filepath.Base(f), filepath.Ext(f)))
		for i, sec := range secs {
			anchor := fmt.Sprintf("%02d-%s", i, slugify(sec.title))
			body := "# " + sec.title + "\n\n" + sec.body
			out = append(out, okf.Concept{
				ID:          fmt.Sprintf("reference/%s/%s.md", slug, anchor),
				Type:        "Reference",
				Title:       sec.title,
				Description: firstLine(sec.body),
				Resource:    f,
				Tags:        []string{"docx", slug},
				Language:    detectMarkdownLang(sec.body),
				SourceURI:   fmt.Sprintf("docx:%s#section-%d", filepath.Base(f), i),
				Body:        body,
				ContentSHA:  okf.ComputeSHA(body),
			})
		}
	}
	return out, nil
}

func readDocxDocument(path string) (docxDocument, error) {
	var doc docxDocument
	zr, err := zip.OpenReader(path)
	if err != nil {
		return doc, err
	}
	defer func() { _ = zr.Close() }()
	for _, zf := range zr.File {
		if zf.Name != "word/document.xml" {
			continue
		}
		rc, err := zf.Open()
		if err != nil {
			return doc, err
		}
		defer func() { _ = rc.Close() }()
		if err := xml.NewDecoder(rc).Decode(&doc); err != nil {
			return doc, err
		}
		return doc, nil
	}
	return doc, fmt.Errorf("no word/document.xml in archive")
}

// splitDocx segments paragraphs into sections at heading-styled paragraphs. Text
// before the first heading becomes an "Overview" section (nothing dropped).
func splitDocx(doc docxDocument) []section {
	var secs []section
	cur := section{title: ""}
	flush := func() {
		cur.body = strings.TrimSpace(cur.body)
		if cur.body == "" && cur.title == "" {
			return
		}
		if cur.title == "" {
			cur.title = "Overview"
		}
		secs = append(secs, cur)
	}
	for _, p := range doc.Body.Paras {
		txt := p.text()
		if txt == "" {
			continue
		}
		if isHeadingStyle(p.StyleVal) {
			flush()
			cur = section{title: cleanTitle(txt)}
			continue
		}
		cur.body += txt + "\n\n"
	}
	flush()
	return secs
}
```

NOTE on the `docxDocument` struct: the root element is `<w:document>`; `xml:",any"` on the outer field lets the decoder descend from the root, and `xml:"body>p"` collects the body's paragraphs. If during Step 4 the decode yields zero paragraphs, switch the struct to decode from the root explicitly:
```go
type docxDocument struct {
	XMLName xml.Name   `xml:"document"`
	Paras   []docxPara `xml:"body>p"`
}
```
and change `splitDocx` to range `doc.Paras`. Pick whichever populates paragraphs against the Step-1 fixture (the test is the oracle).

- [ ] **Step 4: Run** `go test ./internal/ingest/ -run Docx -v` → PASS (all four). If paragraphs come back empty, apply the struct fallback in the NOTE. Then full package `go test ./internal/ingest/`, `go vet`, `gofmt -l`, `golangci-lint` — green.

- [ ] **Step 5: Commit.**
```bash
git add internal/ingest/docx.go internal/ingest/docx_test.go
git commit -m "feat(ingest): docx source — Word documents to Reference concepts"
```

---

### Task 2: xlsx source (excelize)

**Files:**
- Create: `internal/ingest/xlsx.go`, `internal/ingest/xlsx_test.go`
- Modify: `go.mod`, `go.sum` (add `github.com/xuri/excelize/v2`)

**Interfaces:**
- Produces: `func NewXlsxSource(files []string) Source` (`Name()` = `"xlsx"`); const `maxXlsxRows`.

- [ ] **Step 1: Add the dependency.** `go get github.com/xuri/excelize/v2@latest` then `go mod tidy`. Confirm it resolves and `go build ./...` still works. (Pure-Go; no cgo.)

- [ ] **Step 2: Write the failing test** `internal/ingest/xlsx_test.go`:

```go
package ingest

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/xuri/excelize/v2"
)

func writeTestXlsx(t *testing.T) string {
	t.Helper()
	fx := excelize.NewFile()
	// Sheet1 -> "Participantes" with a header + 2 data rows.
	require.NoError(t, fx.SetSheetName("Sheet1", "Participantes"))
	for cell, val := range map[string]string{
		"A1": "ISPB", "B1": "Nome",
		"A2": "00000000", "B2": "Banco A | LTDA",
		"A3": "11111111", "B3": "Banco B",
	} {
		require.NoError(t, fx.SetCellValue("Participantes", cell, val))
	}
	// An empty sheet (must be skipped).
	_, err := fx.NewSheet("Vazia")
	require.NoError(t, err)

	path := filepath.Join(t.TempDir(), "part.xlsx")
	require.NoError(t, fx.SaveAs(path))
	require.NoError(t, fx.Close())
	return path
}

func TestXlsxSource_SheetsToTables(t *testing.T) {
	path := writeTestXlsx(t)
	cs, err := NewXlsxSource([]string{path}).Fetch(context.Background())
	require.NoError(t, err)
	require.Len(t, cs, 1, "empty sheet skipped, one concept for Participantes")

	c := cs[0]
	require.Equal(t, "Reference", c.Type)
	require.Contains(t, c.Title, "Participantes")
	require.Subset(t, c.Tags, []string{"xlsx", "part", "participantes"})
	require.Contains(t, c.Body, "| ISPB |")     // header row
	require.Contains(t, c.Body, "| --- |")       // separator
	require.Contains(t, c.Body, "00000000")      // data
	require.Contains(t, c.Body, `Banco A \| LTDA`) // pipe escaped
	require.NotEmpty(t, c.ContentSHA)
	require.Contains(t, c.ID, "reference/part/")
}

func TestXlsxSource_RowCap(t *testing.T) {
	fx := excelize.NewFile()
	require.NoError(t, fx.SetCellValue("Sheet1", "A1", "H"))
	for i := 2; i < maxXlsxRows+50; i++ {
		require.NoError(t, fx.SetCellValue("Sheet1", "A"+itoa(i), "v"))
	}
	path := filepath.Join(t.TempDir(), "big.xlsx")
	require.NoError(t, fx.SaveAs(path))
	require.NoError(t, fx.Close())

	cs, err := NewXlsxSource([]string{path}).Fetch(context.Background())
	require.NoError(t, err)
	require.Len(t, cs, 1)
	require.Contains(t, cs[0].Body, "more rows omitted")
	require.LessOrEqual(t, strings.Count(cs[0].Body, "\n| v |"), maxXlsxRows)
}

func TestXlsxSource_Name(t *testing.T) {
	require.Equal(t, "xlsx", NewXlsxSource(nil).Name())
}

// itoa avoids importing strconv just for the fixture loop.
func itoa(n int) string { return strings.TrimSpace(strings.Map(func(r rune) rune { return r }, sprintInt(n))) }
func sprintInt(n int) string { return fmtSprintInt(n) }
```
(Simplify: just `import "strconv"` and use `strconv.Itoa` in the test instead of the `itoa` helper above — the helper is a placeholder to avoid an extra import; the implementer should use `strconv.Itoa` directly and delete the helper.)

- [ ] **Step 3: Run** → FAIL. **Step 4: Implement `internal/ingest/xlsx.go`:**

```go
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
		for _, sheet := range fx.GetSheetList() {
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
				ID:          fmt.Sprintf("reference/%s/%s.md", slug, sheetSlug),
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
```

- [ ] **Step 5: Run** `go test ./internal/ingest/ -run Xlsx -v` → PASS; full package + `go build ./...` + `go vet` + `gofmt -l` + `golangci-lint run ./internal/ingest/...` green.

- [ ] **Step 6: Commit.**
```bash
git add internal/ingest/xlsx.go internal/ingest/xlsx_test.go go.mod go.sum
git commit -m "feat(ingest): xlsx source — spreadsheets to Reference table concepts"
```

---

### Task 3: Config keys + buildSources wiring + example doc

**Files:**
- Modify: `cmd/pixkb/config.go`, `cmd/pixkb/commands.go`, `pixkb.yaml.example`
- Test: `cmd/pixkb/config_test.go`

- [ ] **Step 1: Write the failing config test** (append to `config_test.go`):
```go
func TestApplyConfigFileDocxXlsx(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "pixkb.yaml")
	require.NoError(t, os.WriteFile(path, []byte(
		"docx:\n  - a.docx\n  - b.docx\nxlsx:\n  - c.xlsx\n"), 0o644))
	var cfg Config
	applyConfigFile(&cfg, path)
	require.Equal(t, []string{"a.docx", "b.docx"}, cfg.Docx)
	require.Equal(t, []string{"c.xlsx"}, cfg.Xlsx)
}
```

- [ ] **Step 2: Run** → FAIL. **Step 3: Add fields + merge** in `config.go`. To the `Config` struct (near `Markdown`):
```go
	Docx []string `yaml:"docx"` // Word .docx files -> Reference concepts
	Xlsx []string `yaml:"xlsx"` // Excel .xlsx workbooks -> Reference table concepts
```
In `applyConfigFile` (near the `Markdown` merge):
```go
	if len(fromFile.Docx) > 0 {
		cfg.Docx = fromFile.Docx
	}
	if len(fromFile.Xlsx) > 0 {
		cfg.Xlsx = fromFile.Xlsx
	}
```

- [ ] **Step 4: Wire `buildSources`** in `commands.go` (near the `Markdown` source append):
```go
	if len(cfg.Docx) > 0 {
		srcs = append(srcs, ingest.NewDocxSource(cfg.Docx))
	}
	if len(cfg.Xlsx) > 0 {
		srcs = append(srcs, ingest.NewXlsxSource(cfg.Xlsx))
	}
```

- [ ] **Step 5: Document** in `pixkb.yaml.example` (after the `markdown:` block, or near `pdfs:`):
```yaml
# Word (.docx) and Excel (.xlsx) documents -> Reference concepts. Place under the
# per-user mirror dir like pdfs: (offline, pure-Go parsing). docx splits on Word
# heading styles; xlsx renders each sheet as a Markdown table.
docx:
  - "C:/Users/you/AppData/Local/PixKB/mirror/docs/especificacao.docx"
xlsx:
  - "C:/Users/you/AppData/Local/PixKB/mirror/docs/participantes.xlsx"
```

- [ ] **Step 6: Run** `go test ./cmd/pixkb/ -run 'ApplyConfigFile' -v` → PASS; `go build ./...` clean; `go vet ./cmd/pixkb/`; `gofmt -l`; `golangci-lint run ./cmd/pixkb/...`.

- [ ] **Step 7: Commit.**
```bash
git add cmd/pixkb/config.go cmd/pixkb/commands.go cmd/pixkb/config_test.go pixkb.yaml.example
git commit -m "feat(config): docx/xlsx source config keys + buildSources wiring"
```

---

## Self-Review

- **Spec coverage:** docx source (heading-split → Reference) → Task 1; xlsx source (excelize, sheet→table → Reference, row cap, pipe-escape) → Task 2; config keys + wiring + example → Task 3; excelize dep → Task 2 Step 1. Error handling (missing file, corrupt zip, empty doc/sheet warn) → Tasks 1-2. Reuse of `section`/`slugify`/`firstLine`/`cleanTitle`/`detectMarkdownLang`/`ComputeSHA` → Tasks 1-2.
- **Placeholder scan:** the docx `docxDocument` struct carries a NOTE with a concrete fallback keyed to the Step-1 fixture (the test is the oracle) — not a vague TODO. The xlsx test's `itoa` helper is explicitly flagged to be replaced with `strconv.Itoa`. All other code is complete.
- **Type consistency:** `NewDocxSource`/`NewXlsxSource`/`Name()`, `docxDocument`/`docxPara`/`splitDocx`/`isHeadingStyle`/`readDocxDocument`, `renderXlsxTable`/`maxXlsxRows`, `Config.Docx`/`Config.Xlsx` names consistent across tasks. Both emit `Reference`.
- **Air-gap:** excelize is pure-Go; both read local files only. The single new dependency is operator-approved (spec Settled Fork 2).
