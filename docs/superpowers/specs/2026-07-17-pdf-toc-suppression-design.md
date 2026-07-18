# Design: PDF TOC suppression — kill junk `ManualSection` titles at the source

**Date:** 2026-07-17
**Status:** Draft (authored autonomously under `docs/AUTONOMY.md`; forks settled below)
**Issue:** `docs/ISSUES.md` "Content quality" — the BCB manual's table-of-contents
leaks into search as ~51 junk `ManualSection` titles (mangled all-caps dropcap
fragments) + ~27 duplicates. Attempt 1 (dropcap-rejoin heuristic) net-regressed
and was reverted.
**Grounded by:** `.superpowers/research/pdf-junk-title-analysis.md` (real extracted
text of the manual, characterized line-by-line).

## Goal

Produce clean `ManualSection` concepts from the BCB Pix manual PDF: no mangled
dropcap-fragment titles, no TOC/body duplicate titles, real body sections chunked
under their real mixed-case headings — while fully preserving the body prose that
search indexes.

Success is measured by re-running `NewPDFSource(manual).Fetch`: (a) zero titles
that are pure dot-leaders, lone fragments, or dropcap artifacts; (b) known real
headings present (`Iniciação via QR Code Estático`, `Serviço de Iniciação de
Transação de Pagamento`, `QR Code Estático`); (c) every real title appears once;
(d) total body text length preserved (no content dropped).

## Root cause (from the analysis, corrected vs attempt 1)

`internal/ingest/pdf.go`'s `splitSections` runs on the FULL extracted text (TOC +
body) and treats each line as a candidate heading. The manual's TOC renders every
entry as `[section-number line] → [per-word dropcap fragment lines] → [dot-leader
run `^\.+$`] → [bare page-number line]`. `splitSections` turns each fragment line
into a mangled all-caps title, and because every heading appears in BOTH the TOC
and the body, it duplicates. Two grounded facts drive the fix:

1. **Dot-leader lines (`^\.+$`) appear ONLY in the TOC** — real body headings are
   followed immediately by prose, never dot-leaders. So dot-leaders are a
   near-perfect TOC signal, and the TOC is one contiguous `Sumário`-delimited
   block (spanning all chapters + annexes).
2. **Naive shape classification (single-letter = dropcap) is WRONG** — 2-letter
   abbreviations (`QR`, `BR`) sit whole among single-letter fragments, so the
   attempt-1 rule dropped them (`QR CODE` → `ODE`). The fix does NOT classify
   fragment shapes at all.

## Settled forks (decided + logged in `docs/AUTONOMY.md`)

1. **Suppress the whole TOC region as a block; do NOT reconstruct TOC titles.**
   The TOC entries are pure duplicates of body headings, so they are dropped
   entirely — no dropcap-rejoin of TOC fragments needed (this sidesteps attempt
   1's failure mode completely: suppressed fragments can't be mangled). The
   region is delimited by the single `"Sumário"` marker (start) and the end of
   the contiguous dot-leader-bearing run (end).
2. **Detect the TOC end by the dot-leader density gap, not a line count.** From
   `Sumário`, the region continues while dot-leader lines keep recurring; it ends
   after the last dot-leader (plus its trailing page number) once a long
   dot-leader-free stretch begins (real body prose). Robust to the TOC's large,
   variable span.
3. **Add grounded real-heading detection for the body** (the "shared line-join
   routine"): outside the suppressed TOC region, a section-number line
   (`^\d+(\.\d+){0,3}\.?$`) followed by title line(s) then prose is a real
   heading; join the title line(s) with an EMPTY separator + whitespace-collapse
   to reconstruct the clean mixed-case title. This both chunks the body properly
   AND is the same join logic the TOC uses — gated by inside/outside the region.
   The existing ALL-CAPS `looksUpperHeading` path is KEPT for body headings that
   use that style.
4. **Generic, not manual-specific.** `"Sumário"`/`"Índice"` and the
   dot-leader/page-number TOC-entry shape are generic PT-PDF typography, so the
   pre-pass is written generically (it no-ops on a PDF with no `Sumário` + no
   dot-leaders — e.g. today's other sources), not hardcoded to this file.

## Non-goals

- No change to `extractPDFText` (the raw text layer is what it is).
- No learned/OCR re-extraction — pure text-layer post-processing.
- No change to any non-PDF source, to `ManualSection` downstream handling, to
  hygiene, or to search.
- Not attempting to perfectly title every conceivable PDF layout — the target is
  this manual's structure (the only PDF ingested today), done generically enough
  not to break a `Sumário`-less PDF.

## Architecture

All changes are in `internal/ingest/pdf.go`, upstream of the existing
`splitSections` heading logic:

1. **`stripTOCRegion(text string) string`** (new, pure): if a `"Sumário"` line
   exists, drop the contiguous TOC block (Fork 2's dot-leader-gap end-detection);
   else return text unchanged. Runs on the extracted text before sectioning.
2. **`joinNumberedHeading(lines []string, i int) (title string, next int, ok bool)`**
   (new, pure): given a line index at a section-number line, join the following
   non-space/non-dot title lines with `""` until real prose (a line that is not a
   fragment/space/dot) begins; returns the reconstructed title and the index where
   the body starts. Used by the body sectioner.
3. **`splitSections`**: run on `stripTOCRegion(text)`. Extend its scan so a
   section-number line triggers `joinNumberedHeading` (emitting a real numbered
   section), keeping the existing ALL-CAPS `isHeading`/`looksUpperHeading` path
   for the other body headings. `dot`/`isDotLeader(ln)` = `^\.+$` (a helper).
4. `Fetch` is unchanged in shape — it still maps sections → `ManualSection`
   concepts; it just receives clean sections.

## Data flow

`NewPDFSource([manual]).Fetch` → `extractPDFText` (25,482 lines, unchanged) →
`stripTOCRegion` drops the ~1,000-line `Sumário` block → `splitSections` sees
body-only text and emits real numbered/ALL-CAPS sections with clean titles →
`ManualSection` concepts (clean titles, deduped because the TOC copy is gone).

## Error handling / safety

- **No `Sumário` or no dot-leaders** → `stripTOCRegion` returns the text
  unchanged (other PDFs unaffected; this manual always has both).
- **Over-suppression guard (the key regression risk):** the end-detector must not
  eat body content. Mitigation: the end is anchored to the LAST dot-leader line;
  since dot-leaders don't occur in the body, the region can't extend past the
  TOC. A unit test asserts the first real body line (`INTRODUÇÃO` chapter prose)
  survives stripping, and the acceptance test asserts total post-strip text length
  ≈ full length minus the TOC block (body preserved).
- **`joinNumberedHeading` never fabricates** — if a number-line is not followed by
  a plausible title-then-prose, it is not treated as a heading (falls through to
  existing logic), so a stray number can't create a bogus section.

## Testing

- **Unit (pure, no PDF/DB) — over REAL fragment sequences copied from the
  analysis report:**
  - `stripTOCRegion`: a fixture built from the real `Sumário`…dot-leader…page#
    TOC lines followed by a real body start; assert the TOC lines are gone and the
    body lines remain intact; assert a no-`Sumário` input is returned verbatim;
    assert a `Sumário` with the real dot-leader-gap end drops exactly the block.
  - `joinNumberedHeading`: the real `2.6.` sequence (`"I"+"NICIAÇÃO VIA "+"QR"+…`)
    → `"INICIAÇÃO VIA QR CODE ESTÁTICO"` (proves the empty-join, incl. the `QR`
    2-letter case attempt 1 broke); the real body `3.2.` two-line wrap
    (`"Serviço de Iniciação"`/`"de Transação de Pagamento"`) → the joined title.
  - `isDotLeader`: `"...."`→true, `". x"`→false, `""`→false.
- **Acceptance (DB-free integration, gated on the manual PDF being present in the
  mirror — skip cleanly if absent):** call `NewPDFSource([manualPath]).Fetch(ctx)`
  and assert over the returned concepts: (a) NO title matches `^\.+$`, is a lone
  ≤3-char fragment, or is a known dropcap artifact (`ERVIÇO DE`, `ODE ESTÁTICO
  PARA PACS`, …); (b) the three known real headings are present; (c) titles are
  unique (no dup); (d) the concatenated body length across concepts is within a
  small delta of the body text length (no prose dropped). This is the ISSUES-
  prescribed "re-ingest and diff" gate, made deterministic and DB-free.
- **Regression:** existing `pdf_test.go` stays green (its fixtures have no
  `Sumário`/dot-leaders, so `stripTOCRegion` no-ops and `splitSections` behaves as
  today for them).
- **Manual measurement note (operator, not CI):** re-ingest against the local
  throwaway DB and eyeball `pixkb search --type ManualSection` titles + run
  `pixkb hygiene` to confirm the junk-title/duplicate counts drop.

## Open questions for the plan

1. Exact dot-leader-gap threshold for TOC-end (start ~30 lines; the unit test over
   the real sequence pins it — the first real chapter's prose is dense and
   dot-leader-free, so a generous gap is safe).
2. Whether `joinNumberedHeading` should also require the following prose to be
   non-empty within a lookahead (recommended: yes — a number-line at the very end
   of a page with no following title is not a heading).
3. Whether to cap a reconstructed title's length (the existing `cleanTitle`
   already truncates to 80 — reuse it).
