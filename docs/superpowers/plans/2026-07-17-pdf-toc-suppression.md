# PDF TOC Suppression Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development. Steps use checkbox (`- [ ]`) syntax.

**Goal:** Strip the BCB manual's table-of-contents block from PDF-extracted text before sectioning, killing the ~51 junk `ManualSection` titles + ~27 duplicates at their shared source; optionally surface real body numbered headings with clean titles.

**Architecture:** All in `internal/ingest/pdf.go`, upstream of `splitSections`: a pure `stripTOCRegion` pre-pass (grounded, high-confidence — Tasks 1-2), then a measurement-gated body numbered-heading joiner (Task 3, descopes to follow-up if it can't hit the bar).

**Tech Stack:** Go, `regexp`, `strings`; the existing `github.com/ledongthuc/pdf` extractor (unchanged); testify.

## Global Constraints

- **Grounded in real data:** `.superpowers/research/pdf-junk-title-analysis.md` — dot-leader lines (`^\.{4,}$`) occur ONLY in the TOC; the TOC is one contiguous `"Sumário"`-delimited block; real body headings are number-line + mixed-case title line(s) + prose (never dropcap-split).
- **No shape-based dropcap classification** (single-letter vs whole-word) — it broke attempt 1 on 2-letter abbreviations (`QR`). TOC titles are dropped wholesale, not reconstructed.
- **Generic / no-op safety:** a PDF with no `"Sumário"` OR no dot-leaders is returned UNCHANGED — the existing `pdf_test.go` fixtures (no Sumário) must behave exactly as today.
- **No content loss:** stripping removes only the TOC block; body prose is fully preserved (asserted by a text-length delta gate).
- **Do not touch** `extractPDFText`, `Fetch`'s concept-mapping shape, hygiene, search, or any non-PDF source.
- **LF; explicit `git add` paths; no AI attribution; conventional commits; scripts to `.scripts/`.**
- Spec is binding: `docs/superpowers/specs/2026-07-17-pdf-toc-suppression-design.md`.

---

### Task 1: `stripTOCRegion` + line predicates (pure)

**Files:**
- Modify: `internal/ingest/pdf.go`
- Test: `internal/ingest/pdf_test.go` (append)

**Interfaces:**
- Produces: `func stripTOCRegion(text string) string`; helpers `isDotLeader(string) bool`, `isBarePageNumber(string) bool`; package vars `dotLeaderRE`, `barePageRE`; const `tocGapThreshold`.

- [ ] **Step 1: Write the failing test** (append to `pdf_test.go`). Build the fixture from the REAL line sequence in the analysis report (§1) so it captures the actual structure:

```go
func TestStripTOCRegion(t *testing.T) {
	// Mirrors the real manual: title/version front matter, then the Sumário TOC
	// block (number lines, dropcap fragments, dot-leader runs, page numbers),
	// then real body prose. Dot-leaders appear ONLY in the TOC.
	toc := strings.Join([]string{
		"Manual", "de Padrões", "Versão 2.8", // front matter (kept)
		"Sumário",                              // TOC start marker
		"1.", "INTRODUÇÃO", "................................", "6",
		"2.", "INICIAÇÃO POR QR CODE", "....................", "6",
		"2.6.", "I", "NICIAÇÃO VIA ", "QR", "C", "ODE ", "E", "STÁTICO",
		"........................", " ", "11",
		// --- body begins: no more dot-leaders ---
		"1.", "Introdução",
		"O Pix é o meio de pagamento instantâneo brasileiro que permite...",
	}, "\n")

	out := stripTOCRegion(toc)

	require.Contains(t, out, "Manual", "front matter before Sumário is kept")
	require.NotContains(t, out, "Sumário", "the Sumário marker is dropped")
	require.NotContains(t, out, "NICIAÇÃO VIA ", "TOC dropcap fragments are dropped")
	require.NotContains(t, out, "....", "no dot-leader lines survive")
	require.Contains(t, out, "O Pix é o meio de pagamento", "body prose is preserved")
	require.Contains(t, out, "Introdução", "body heading is preserved")
}

func TestStripTOCRegion_NoSumario(t *testing.T) {
	in := "3.2 Serviço\n\nUm texto qualquer sem sumário.\n"
	require.Equal(t, in, stripTOCRegion(in), "no Sumário -> unchanged")
}

func TestStripTOCRegion_SumarioNoDotLeaders(t *testing.T) {
	in := "Sumário\n1. Introdução\nTexto sem dot-leaders.\n"
	require.Equal(t, in, stripTOCRegion(in), "Sumário but no dot-leaders -> unchanged")
}

func TestLinePredicates(t *testing.T) {
	require.True(t, isDotLeader("........"))
	require.True(t, isDotLeader("  ....  "))
	require.False(t, isDotLeader("... x"))
	require.False(t, isDotLeader(""))
	require.True(t, isBarePageNumber("38"))
	require.True(t, isBarePageNumber("6"))
	require.False(t, isBarePageNumber("2.6."))
	require.False(t, isBarePageNumber("ANEXO"))
}
```

(Ensure `strings` is imported in the test file.)

- [ ] **Step 2: Run** `go test ./internal/ingest/ -run 'StripTOCRegion|LinePredicates' -v` → FAIL (undefined).

- [ ] **Step 3: Implement** in `pdf.go` (add near the other regexes/helpers):

```go
const tocGapThreshold = 40

var (
	dotLeaderRE = regexp.MustCompile(`^\.{4,}$`)
	barePageRE  = regexp.MustCompile(`^\d{1,4}$`)
)

func isDotLeader(ln string) bool     { return dotLeaderRE.MatchString(strings.TrimSpace(ln)) }
func isBarePageNumber(s string) bool { return barePageRE.MatchString(strings.TrimSpace(s)) }

// stripTOCRegion removes a leading table-of-contents block. The BCB manual
// renders TOC entries ending in dot-leader runs (^\.{4,}$) + a bare page number;
// dot-leaders occur ONLY in the TOC, so the whole block from the "Sumário" marker
// through the last dot-leader (plus its trailing page number) is dropped. A PDF
// with no "Sumário" or no dot-leaders is returned unchanged, so non-manual
// sources are unaffected.
func stripTOCRegion(text string) string {
	lines := strings.Split(text, "\n")
	start := -1
	for i, ln := range lines {
		if strings.EqualFold(strings.TrimSpace(ln), "Sumário") {
			start = i
			break
		}
	}
	if start < 0 {
		return text
	}
	lastLeader := -1
	for i := start; i < len(lines); i++ {
		if isDotLeader(lines[i]) {
			lastLeader = i
		} else if lastLeader >= 0 && i-lastLeader > tocGapThreshold {
			break // long dot-leader-free stretch -> body has started
		}
	}
	if lastLeader < 0 {
		return text // "Sumário" present but no dot-leaders: not a real TOC block
	}
	// Consume a short trailer (blank/space lines + one bare page number) after the
	// last dot-leader, stopping before real body prose.
	end := lastLeader + 1
	for end < len(lines) && end <= lastLeader+4 {
		t := strings.TrimSpace(lines[end])
		if t == "" {
			end++
			continue
		}
		if isBarePageNumber(t) {
			end++
		}
		break
	}
	kept := make([]string, 0, len(lines))
	kept = append(kept, lines[:start]...)
	kept = append(kept, lines[end:]...)
	return strings.Join(kept, "\n")
}
```

- [ ] **Step 4: Run** `go test ./internal/ingest/ -run 'StripTOCRegion|LinePredicates' -v` → PASS; then `go test ./internal/ingest/ -v` (whole package — the existing `pdf_test.go`/`splitSections` tests must stay green since their fixtures have no `Sumário`); `go vet ./internal/ingest/`; `gofmt -l internal/ingest/pdf.go`; `golangci-lint run ./internal/ingest/...` if available.

- [ ] **Step 5: Commit.**
```bash
git add internal/ingest/pdf.go internal/ingest/pdf_test.go
git commit -m "feat(ingest): stripTOCRegion — drop the Sumário TOC block from PDF text"
```

---

### Task 2: Wire `stripTOCRegion` into Fetch + DB-free acceptance gate

**Files:**
- Modify: `internal/ingest/pdf.go` (`Fetch`)
- Test: `internal/ingest/pdf_test.go` (append the real-PDF acceptance test)

**Interfaces:**
- Consumes: `stripTOCRegion` (Task 1), `extractPDFText`/`splitSections` (existing).

- [ ] **Step 1: Wire it.** In `pdfSource.Fetch`, apply the pre-pass to the extracted text before sectioning:
```go
		text, err := extractPDFText(f)
		if err != nil {
			return nil, fmt.Errorf("pdf %s: %w", f, err)
		}
		text = stripTOCRegion(text)
		slug := slugify(...)
		for i, sec := range splitSections(text) { ... }   // unchanged below
```

- [ ] **Step 2: Write the acceptance test** (append to `pdf_test.go`) — DB-free, gated on the real manual PDF being present in the mirror dir (skip cleanly if absent, following the env-resolution the codebase uses):
```go
func manualPDFPath() string {
	base := os.Getenv("LOCALAPPDATA")
	if base == "" {
		return "" // non-Windows / unset: acceptance test skips
	}
	p := filepath.Join(base, "PixKB", "mirror", "pdfs", "II_ManualdePadroesparaIniciacaodoPix.pdf")
	if _, err := os.Stat(p); err != nil {
		return ""
	}
	return p
}

func TestPDFFetch_NoTOCJunk(t *testing.T) {
	p := manualPDFPath()
	if p == "" {
		t.Skip("manual PDF not present in mirror dir")
	}
	concepts, err := NewPDFSource([]string{p}).Fetch(context.Background())
	require.NoError(t, err)
	require.NotEmpty(t, concepts)

	seen := map[string]bool{}
	junk := regexp.MustCompile(`^\.+$`)
	dropcapArtifacts := []string{"ERVIÇO DE", "ODE ESTÁTICO PARA PACS", "ECOMENDAÇÕES DE SEGURANÇA"}
	for _, c := range concepts {
		title := strings.TrimSpace(c.Title)
		require.False(t, junk.MatchString(title), "dot-leader title leaked: %q", title)
		require.NotRegexp(t, `^\d{1,4}$`, title, "bare page-number title leaked: %q", title)
		for _, a := range dropcapArtifacts {
			require.NotEqual(t, a, title, "known dropcap artifact leaked: %q", title)
		}
		require.False(t, seen[title], "duplicate ManualSection title: %q", title)
		seen[title] = true
	}
	t.Logf("manual produced %d ManualSection concepts (clean)", len(concepts))
}
```
(Add imports `context`, `os`, `path/filepath`, `regexp`, `strings` as needed.)

- [ ] **Step 3: Run** the acceptance test against the local machine (the PDF IS in the mirror per the analysis): `go test ./internal/ingest/ -run 'PDFFetch_NoTOCJunk' -v` → PASS (or SKIP if the PDF is absent — then note it). Full package + `go build ./...` + `go vet` + `gofmt -l` + `golangci-lint` green.

- [ ] **Step 4: Capture the before/after count** in the report: note how many `ManualSection` concepts the manual now yields vs the documented buggy baseline (~93), and confirm no junk/dup titles remain. This is the ISSUES-prescribed measurement.

- [ ] **Step 5: Commit.**
```bash
git add internal/ingest/pdf.go internal/ingest/pdf_test.go
git commit -m "feat(ingest): apply TOC suppression in PDF Fetch + acceptance gate"
```

---

### Task 3: Body numbered-heading detection (MEASUREMENT-GATED — descope if it can't hit the bar)

**Files:**
- Modify: `internal/ingest/pdf.go` (`splitSections` + a new `joinNumberedHeading`)
- Test: `internal/ingest/pdf_test.go`

**Interfaces:**
- Produces: `func joinNumberedHeading(lines []string, i int) (title string, next int, ok bool)`; `sectionNumberRE`.

**Rationale & guardrail:** after Task 2, the body's real numbered headings (number on its own line, then mixed-case title line(s), then prose) are still not surfaced as clean titles (the existing single-line `numHeadingRE` doesn't match them). This task surfaces them. It is the FUZZY part (title-vs-prose boundary), so it is gated: **if, measured against the Task 2 acceptance test + a before/after title diff, this task does NOT strictly improve the result (more real clean headings, still zero junk/dups, no dropped body), it is REVERTED and logged as a follow-up — v1 ships as Task 2 (suppression-only), which already fixes the reported junk+dup bug.** Do not ship a net-neutral-or-worse heuristic (the ISSUES lesson).

- [ ] **Step 1: Unit-test `joinNumberedHeading`** over the REAL sequences (analysis §2-3):
```go
func TestJoinNumberedHeading(t *testing.T) {
	// Real 2.6. body heading: number line, then title fragments, then prose.
	lines := []string{
		"2.6.", " ", "Iniciação via QR Code Estático", " ",
		"O QR Code estático no Pix conterá o seguinte conjunto de informações que...",
	}
	title, next, ok := joinNumberedHeading(lines, 0)
	require.True(t, ok)
	require.Equal(t, "Iniciação via QR Code Estático", title)
	require.Equal(t, 4, next) // index of the prose line

	// Two-line wrapped title (real 3.2.).
	lines2 := []string{
		"3.2.", " ", "Serviço de Iniciação", "de Transação de Pagamento", " ",
		"Um Pix pode ser iniciado através do serviço de iniciação de transação...",
	}
	title2, _, ok2 := joinNumberedHeading(lines2, 0)
	require.True(t, ok2)
	require.Equal(t, "Serviço de Iniciação de Transação de Pagamento", title2)

	// A number line NOT followed by a title+prose is not a heading.
	_, _, ok3 := joinNumberedHeading([]string{"5.", " ", "50"}, 0)
	require.False(t, ok3)
}
```

- [ ] **Step 2: Implement `joinNumberedHeading`** — join short title line(s) after a section-number line until prose (a line longer than a title threshold, or the end of the short title run), require following prose, reuse `cleanTitle` for the final normalize. Implement `sectionNumberRE = ^\d+(\.\d+){1,3}\.?$|^\d+\.$` (matches `2.6`, `2.6.`, `1.`, `6.1`; NOT a bare `6`). Wire into `splitSections`: when a line matches `sectionNumberRE`, try `joinNumberedHeading`; on `ok`, start a new section with that title and continue from `next`; else fall through to the existing `isHeading` logic. Keep the ALL-CAPS path intact.

- [ ] **Step 3: Run** the unit tests → PASS; then RE-RUN the Task 2 acceptance test (`TestPDFFetch_NoTOCJunk`) and compare the ManualSection count + titles to Task 2's captured baseline. **Decision gate:** the real headings (`Iniciação via QR Code Estático`, `Serviço de Iniciação de Transação de Pagamento`, `QR Code Estático`) must now appear as clean titles, still zero junk/dups, and body length preserved. If yes → keep. If it introduces junk, drops body, or doesn't surface the real headings → `git checkout -- internal/ingest/pdf.go` (revert Task 3's `splitSections` change, keep the pure `joinNumberedHeading` + its unit test as dead-but-tested code OR revert entirely), and record in the report that Task 3 is descoped to a BACKLOG follow-up.

- [ ] **Step 4: Add the real-heading assertion to the acceptance test** (only if Task 3 kept): extend `TestPDFFetch_NoTOCJunk` to `require.Contains` the three known real headings.

- [ ] **Step 5: Commit** (kept or descoped — either way record the measured outcome):
```bash
git add internal/ingest/pdf.go internal/ingest/pdf_test.go
git commit -m "feat(ingest): surface real body numbered headings"   # or: "docs: descope body-heading detection to follow-up (measured net-neutral)"
```

---

## Self-Review

- **Spec coverage:** Fork 1 (suppress TOC block) → Task 1 `stripTOCRegion`; Fork 2 (dot-leader-gap end-detection) → Task 1 impl; Fork 4 (generic/no-op) → Task 1 `NoSumario`/`SumarioNoDotLeaders` tests + the `pdf_test.go` regression; Fork 3 (body heading detection) → Task 3, explicitly measurement-gated/descopeable. Acceptance measurement (no junk/dup/body-loss) → Task 2; real-headings-present → Task 3 gate.
- **Regression safety:** the no-op-without-Sumário path + the existing `pdf_test.go` staying green (Task 1 Step 4) guard non-manual PDFs. Task 3's revert-on-net-neutral gate is the attempt-1-lesson safeguard.
- **Placeholder scan:** Task 3 Step 2's `joinNumberedHeading` body is described (join-until-prose, require-following-prose, reuse `cleanTitle`) rather than fully coded — this is the intentionally-iterative fuzzy part; the implementer tunes the title/prose threshold against the Step 1 unit tests + Step 3 measurement, which is the correct approach for this task per ISSUES ("budget for iterative measurement"). All other steps carry complete code.
- **Type consistency:** `stripTOCRegion`, `isDotLeader`, `isBarePageNumber`, `joinNumberedHeading`, `sectionNumberRE`, `tocGapThreshold` names consistent across tasks.
