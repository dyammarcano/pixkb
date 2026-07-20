package ingest

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/ledongthuc/pdf"

	"pixkb/internal/okf"
)

type pdfSource struct{ files []string }

// NewPDFSource builds a Source that extracts text from each PDF and splits it
// into ManualSection concepts (one per detected heading section).
func NewPDFSource(files []string) Source { return &pdfSource{files: files} }

func (s *pdfSource) Name() string { return "pdf" }

func (s *pdfSource) Fetch(_ context.Context) ([]okf.Concept, error) {
	var out []okf.Concept
	for _, f := range s.files {
		text, err := extractPDFText(f)
		if err != nil {
			return nil, fmt.Errorf("pdf %s: %w", f, err)
		}
		text = stripTOCRegion(text)
		slug := slugify(strings.TrimSuffix(filepath.Base(f), filepath.Ext(f)))
		for i, sec := range splitSections(text) {
			id := fmt.Sprintf("manuals/%s/secao-%d.md", slug, i)
			body := "# " + sec.title + "\n\n" + sec.body
			out = append(out, okf.Concept{
				ID:          id,
				Type:        "ManualSection",
				Title:       sec.title,
				Description: firstLine(sec.body),
				Resource:    f,
				Tags:        []string{"manual", slug},
				Language:    "pt",
				SourceURI:   "pdf:" + filepath.Base(f) + "#section-" + strconv.Itoa(i),
				Body:        body,
				ContentSHA:  okf.ComputeSHA(body),
			})
		}
	}
	return out, nil
}

func extractPDFText(path string) (string, error) {
	f, r, err := pdf.Open(path)
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()
	var b strings.Builder
	for i := 1; i <= r.NumPage(); i++ {
		p := r.Page(i)
		if p.V.IsNull() {
			continue
		}
		txt, err := p.GetPlainText(nil)
		if err != nil {
			// One malformed page must not abort the whole manual; skip it.
			slog.Warn("pdf page extract failed; skipping", "path", path, "page", i, "err", err)
			continue
		}
		b.WriteString(txt)
		b.WriteString("\n")
	}
	return b.String(), nil
}

type section struct {
	title string
	body  string
}

// numHeadingRE matches a numbered heading: "3.2 Título" — a section number
// (up to 4 levels) followed by UPPERCASE-initial real text. Requiring the title
// to start uppercase rejects mid-sentence/table fragments like "1 de autorização
// do Pix" or "4 de 10" that a page number + flowing lowercase text would form.
var numHeadingRE = regexp.MustCompile(`^(\d+(?:\.\d+){0,3})\.?\s+(\p{Lu}.{2,80})$`)

// wsRE collapses runs of whitespace (PDF extraction sprays stray spaces).
var wsRE = regexp.MustCompile(`\s+`)

// isHeading reports whether a line looks like a real section heading rather
// than a table cell, a code, or OCR noise. It accepts either a numbered
// heading ("3.2 Foo") or an all-caps heading made of real words — and rejects
// digit-only / code-only fragments like "63 04" or "EMV QRCPS" that the old
// permissive regex used to promote to titles.
func isHeading(ln string) bool {
	t := strings.TrimSpace(ln)
	if t == "" {
		return false
	}
	fields := strings.Fields(t)
	if len(fields) == 0 || len(fields) > 12 {
		return false
	}
	if m := numHeadingRE.FindStringSubmatch(t); m != nil {
		return hasRealWord(m[2])
	}
	return looksUpperHeading(t)
}

// hasRealWord is true if s contains a token with >=3 letters including a vowel
// (filters bare codes/acronyms).
func hasRealWord(s string) bool {
	for w := range strings.FieldsSeq(s) {
		letters, vowel := 0, false
		for _, r := range strings.ToLower(w) {
			if (r >= 'a' && r <= 'z') || r >= 0x00e0 {
				letters++
				switch r {
				case 'a', 'e', 'i', 'o', 'u', 'á', 'â', 'ã', 'à', 'é', 'ê', 'í', 'ó', 'ô', 'õ', 'ú', 'ç':
					vowel = true
				}
			}
		}
		if letters >= 3 && vowel {
			return true
		}
	}
	return false
}

// looksUpperHeading accepts an ALL-CAPS line of real words: no lowercase, at
// least two words, length 8..80, more letters than digits, and at least one
// 4+-letter word with a vowel (so "EMV QRCPS" / "63 04" are rejected).
func looksUpperHeading(t string) bool {
	if len(t) < 8 || len(t) > 80 {
		return false
	}
	if strings.IndexFunc(t, func(r rune) bool {
		return (r >= 'a' && r <= 'z') || (r >= 0x00e0 && r <= 0x00ff)
	}) >= 0 {
		return false // has lowercase (incl. accented é/ç): not an all-caps heading
	}
	flds := strings.Fields(t)
	if len(flds) < 2 {
		return false
	}
	// Reject headings whose first word is a stray single/double letter — a leading
	// article or OCR fragment, e.g. `O "ANEXO IV`. Real all-caps headings begin
	// with a substantive (>=3-letter) word. Strip non-letters first so `"ANEXO`
	// counts its letters but a lone `O` does not survive.
	firstWord := strings.Map(func(r rune) rune {
		if (r >= 'A' && r <= 'Z') || r >= 0x00c0 {
			return r
		}
		return -1
	}, flds[0])
	if len([]rune(firstWord)) < 3 {
		return false
	}
	// EMV/TLV table dumps start with a numeric field id ("60 08 BRASILIA"):
	// a leading all-digit token means this is tabular data, not a heading.
	if strings.IndexFunc(flds[0], func(r rune) bool { return r < '0' || r > '9' }) < 0 {
		return false
	}
	letters, digits := 0, 0
	for _, r := range t {
		switch {
		case (r >= 'A' && r <= 'Z') || r >= 0x00c0:
			letters++
		case r >= '0' && r <= '9':
			digits++
		}
	}
	if letters <= digits {
		return false
	}
	// Reject if no 4+-letter word containing a vowel.
	for w := range strings.FieldsSeq(t) {
		n, vowel := 0, false
		for _, r := range strings.ToLower(w) {
			if (r >= 'a' && r <= 'z') || r >= 0x00e0 {
				n++
				switch r {
				case 'a', 'e', 'i', 'o', 'u', 'á', 'â', 'ã', 'à', 'é', 'ê', 'í', 'ó', 'ô', 'õ', 'ú':
					vowel = true
				}
			}
		}
		if n >= 4 && vowel {
			return true
		}
	}
	return false
}

const tocGapThreshold = 40

var (
	dotLeaderRE = regexp.MustCompile(`^\.{4,}$`)
	barePageRE  = regexp.MustCompile(`^\d{1,4}$`)
)

func isDotLeader(ln string) bool     { return dotLeaderRE.MatchString(strings.TrimSpace(ln)) }
func isBarePageNumber(s string) bool { return barePageRE.MatchString(strings.TrimSpace(s)) }

// isSumarioMarker reports whether a line is a "Sumário" TOC heading, tolerating
// the accentless "Sumario" that some PDF text layers extract (á -> a).
func isSumarioMarker(ln string) bool {
	t := strings.TrimSpace(ln)
	return strings.EqualFold(t, "Sumário") || strings.EqualFold(t, "Sumario")
}

// stripTOCRegion removes a leading table-of-contents block. The BCB manual
// renders TOC entries ending in dot-leader runs (^\.{4,}$) + a bare page number;
// dot-leaders occur ONLY in the TOC, so the whole block from the "Sumário" marker
// (accentless "Sumario" is also tolerated) through the last dot-leader (plus its
// trailing page number) is dropped. A PDF with no such marker or no dot-leaders
// is returned unchanged, so non-manual sources are unaffected.
func stripTOCRegion(text string) string {
	lines := strings.Split(text, "\n")
	start := -1
	for i, ln := range lines {
		if isSumarioMarker(ln) {
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

// cleanTitle normalizes a heading line into a tidy title.
func cleanTitle(ln string) string {
	t := wsRE.ReplaceAllString(strings.TrimSpace(ln), " ")
	if len(t) > 80 {
		t = strings.TrimSpace(t[:80])
	}
	return t
}

// splitSections splits plain text into sections at heading-like lines. If no
// headings are found, the whole document is one section. Sections shorter than
// a few words are merged into the previous one to avoid fragments.
func splitSections(text string) []section {
	lines := strings.Split(text, "\n")
	var secs []section
	cur := section{title: "Documento"}
	flush := func() {
		cur.body = strings.TrimSpace(cur.body)
		if cur.body == "" && len(secs) > 0 {
			return
		}
		secs = append(secs, cur)
	}
	for _, ln := range lines {
		if isHeading(ln) {
			if strings.TrimSpace(cur.body) != "" {
				flush()
			}
			cur = section{title: cleanTitle(ln)}
			continue
		}
		cur.body += ln + "\n"
	}
	flush()
	if len(secs) == 0 {
		return []section{{title: "Documento", body: strings.TrimSpace(text)}}
	}
	return secs
}

// Slugify exposes the package's filesystem/id-safe slug helper for callers that
// stage files by a derived name (e.g. the Dump/Ingest URL handler).
func Slugify(s string) string { return slugify(s) }

func slugify(s string) string {
	s = strings.ToLower(s)
	var b strings.Builder
	prevDash := false
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			prevDash = false
		default:
			if !prevDash {
				b.WriteByte('-')
				prevDash = true
			}
		}
	}
	return strings.Trim(b.String(), "-")
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	if len(s) > 200 {
		s = s[:200]
	}
	return s
}
