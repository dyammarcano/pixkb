# Design: docx + xlsx ingest sources

**Date:** 2026-07-18
**Status:** Draft (authored autonomously under `docs/AUTONOMY.md`; forks settled below)
**Directive:** operator request — "implement md, pdf, docx, excel parsers." md
(`internal/ingest/markdown.go`) and pdf (`internal/ingest/pdf.go`) already exist,
so this delivers the two missing formats.

## Goal

Ingest Microsoft Word (`.docx`) and Excel (`.xlsx`) documents into the KB as
`Reference` concepts, so operator-supplied Office documents (BACEN/Receita specs,
participant spreadsheets, etc.) become searchable alongside the existing PDF/
Markdown/OpenAPI corpus — offline, pure-Go, matching the established
`ingest.Source` adapter pattern.

## Settled forks (operator-approved + decided)

1. **Scope: add docx + xlsx only** (operator choice). Two new `ingest.Source`
   adapters (`NewDocxSource`, `NewXlsxSource`) + config keys, wired into
   `buildSources` exactly like `pdfs:`/`markdown:`. md/pdf are left untouched.
2. **Parse: excelize for xlsx, stdlib for docx** (operator choice). `xlsx` uses
   `github.com/xuri/excelize/v2` (pure-Go — reads local files, no native runtime,
   air-gap-safe). `docx` is hand-rolled with `archive/zip` + `encoding/xml`
   (`word/document.xml` text extraction is simple; no dependency needed).
3. **Both emit `Reference` concepts** (matching `markdown.go`), not new `Type`
   values — avoids type proliferation; distinguished by a `docx`/`xlsx` content
   tag. Coarse `--type Reference` groups all reference docs; the tag/`domain`
   filter separates.
4. **Domain via the default backfill.** Config keys are plain `[]string` file
   lists (like `pdfs:`/`markdown:`); emitted concepts carry no `domain:` tag, so
   `ingest.tagDomain` backfills `domain:pix`. (A per-file domain can be added
   later if needed — YAGNI now, consistent with pdfs/markdown.)

## Non-goals

- No change to md/pdf/openapi/legislation sources, search, or ranking.
- No rich formatting fidelity — text + structure only (docx: paragraphs +
  heading-style section splits; xlsx: cell values rendered as a Markdown table).
  Images, charts, styles, formulas-as-formulas are out (formula *values* come
  through excelize's computed cell strings).
- No streaming of huge files — bounded (xlsx row cap, see below).

## Architecture

Two new files, mirroring `markdown.go`/`pdf.go`:

### `internal/ingest/docx.go`
- `NewDocxSource(files []string) Source`; `Name()` = `"docx"`.
- `Fetch`: for each file, `archive/zip.OpenReader`, read the `word/document.xml`
  entry, `encoding/xml`-decode paragraphs. A paragraph is a **heading** when its
  `w:pPr/w:pStyle/@w:val` begins with `Heading` (EN) or `Título`/`Titulo` (PT
  Word). Text = concatenation of the paragraph's `w:t` run texts.
- Section split (same shape as markdown): each heading paragraph starts a new
  section; text before the first heading → an `Overview` section; a doc with no
  headings → one whole-document section. Emit one `Reference` per section:
  - `ID`: `reference/<fileslug>/<NN-headingslug>.md`
  - `Tags`: `["docx", fileslug]`; `Language`: `detectMarkdownLang(body)` (reused);
    `SourceURI`: `"docx:" + base + "#section-N"`; `Body`: `"# " + title + "\n\n" +
    text`; `ContentSHA`: `okf.ComputeSHA(body)`.

### `internal/ingest/xlsx.go`
- `NewXlsxSource(files []string) Source`; `Name()` = `"xlsx"`.
- `Fetch`: `excelize.OpenFile`; for each sheet, `GetRows` → render a Markdown
  table (header row = first row; subsequent rows as `| … |`). Emit one `Reference`
  concept **per non-empty sheet**:
  - `ID`: `reference/<fileslug>/<sheetslug>.md`; `Title`: `"<file> — <sheet>"`;
    `Tags`: `["xlsx", fileslug, sheetslug]`; `SourceURI`: `"xlsx:" + base +
    "#" + sheet`; `Body`: `"# " + title + "\n\n" + markdownTable`;
    `ContentSHA`, `Language` as above.
  - **Row cap:** render at most `maxXlsxRows` (e.g. 500) data rows; if truncated,
    append a `_… (N more rows omitted)_` note so a giant sheet can't create a
    multi-MB concept. Empty sheets (0 rows) are skipped.
  - Ragged rows are padded to the widest row so the Markdown table is valid.

### Config + wiring
- `cmd/pixkb/config.go`: add `Docx []string \`yaml:"docx"\`` and `Xlsx []string
  \`yaml:"xlsx"\`` to `Config`; merge each in `applyConfigFile` (`if len(...) > 0`).
- `cmd/pixkb/commands.go` `buildSources`: `if len(cfg.Docx) > 0 { srcs = append(...,
  ingest.NewDocxSource(cfg.Docx)) }`; same for `Xlsx`.
- `pixkb.yaml.example`: document `docx:` / `xlsx:` (files under the per-user mirror
  dir, like `pdfs:`).
- `go.mod`: add `github.com/xuri/excelize/v2` (pure-Go).

## Data flow

`pixkb ingest` → `GatherAll` runs all sources; `NewDocxSource`/`NewXlsxSource`
read local `.docx`/`.xlsx` → `Reference` concepts tagged `docx`/`xlsx` →
`tagDomain` backfills `domain:pix` → epoch commit → searchable.

## Error handling

- **Missing/unreadable file** → `Fetch` returns a wrapped error (`fmt.Errorf("docx
  %s: %w", …)`), consistent with the all-or-nothing gather contract of pdf/markdown.
- **Corrupt zip / no `word/document.xml`** (docx) → error naming the file.
- **Malformed xlsx** → excelize's open error is wrapped.
- **Empty document / all-empty sheets** → zero concepts for that file (warn via
  `slog`, don't fail — matches openapi's "no paths → zero concepts" convention).
- **Zip-slip / path safety:** docx only reads the fixed `word/document.xml` entry
  by name (never extracts to disk), so there is no path-traversal surface.

## Testing

- **docx (no committed binary fixture):** the test BUILDS a minimal `.docx` in a
  temp dir — an `archive/zip` with `[Content_Types].xml` + a `word/document.xml`
  containing two `Heading1` paragraphs and body paragraphs — then
  `NewDocxSource([tmp]).Fetch` and asserts: two `Reference` concepts with the
  heading titles, bodies containing the paragraph text, `docx` tag, correct IDs;
  plus a no-heading doc → one whole-document concept; plus a missing-file error.
- **xlsx (no committed binary):** the test WRITES a fixture `.xlsx` with excelize
  (2 sheets, a header + rows, one empty sheet), then `NewXlsxSource([tmp]).Fetch`
  and asserts: one `Reference` per non-empty sheet, body is a valid Markdown table
  containing the cell values, `xlsx`+sheet tags, empty sheet skipped, and the row
  cap truncates + notes on an over-cap sheet.
- **Config load test** for `docx:`/`xlsx:` mirroring the `openapi_specs` test.
- **buildSources**: a config with docx/xlsx files appends the sources (unit test
  over the source `Name()`s).
- All DB-free (pure `Fetch` + config unit tests). No prod/live-DB dependency.

## Open questions for the plan

1. `maxXlsxRows` value (lean 500) and whether to also cap columns (lean: no —
   sheets are usually narrow; a note on the row cap suffices).
2. docx heading-style match set — `Heading[1-9]`, `Título`/`Titulo`; whether to
   also treat `Title`/`Subtitle` styles as headings (lean: treat `Title` as the
   doc title / first heading; `Subtitle` as body).
3. xlsx Markdown-table escaping of `|` inside cell values (lean: replace `|` →
   `\|` so the table stays well-formed).
