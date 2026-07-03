package ingest

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"pixkb/internal/okf"
)

type markdownSource struct{ files []string }

// NewMarkdownSource builds a Source that ingests curated local Markdown
// reference docs, emitting one Reference concept per H2 (`## `) section. Unlike
// the PDF source the input is already clean Markdown, so segmentation is exact:
// the H2 heading is the concept title and everything down to the next H2 is the
// body (nested H3+ sub-sections, tables, and code fences are preserved). Content
// between the H1 and the first H2 becomes an "Overview" concept.
func NewMarkdownSource(files []string) Source { return &markdownSource{files: files} }

func (s *markdownSource) Name() string { return "markdown" }

func (s *markdownSource) Fetch(_ context.Context) ([]okf.Concept, error) {
	var out []okf.Concept
	for _, f := range s.files {
		raw, err := os.ReadFile(f)
		if err != nil {
			return nil, fmt.Errorf("markdown %s: %w", f, err)
		}
		docTitle, secs := splitMarkdown(string(raw))
		slug := slugify(strings.TrimSuffix(filepath.Base(f), filepath.Ext(f)))
		for i, sec := range secs {
			title := sec.title
			if title == "" {
				title = docTitle
			}
			anchor := fmt.Sprintf("%02d-%s", i, slugify(sec.title))
			body := "# " + title + "\n\n" + sec.body
			out = append(out, okf.Concept{
				ID:          fmt.Sprintf("reference/%s/%s.md", slug, anchor),
				Type:        "Reference",
				Title:       title,
				Description: firstLine(sec.body),
				Resource:    f,
				Tags:        []string{"reference", slug},
				Language:    detectMarkdownLang(sec.body),
				SourceURI:   "markdown:" + filepath.Base(f) + "#" + anchor,
				Body:        body,
				ContentSHA:  okf.ComputeSHA(body),
			})
		}
	}
	return out, nil
}

// revTagRE matches the hidden revision tag maintained on living docs; it is
// stripped so it never pollutes a concept title or body.
var revTagRE = regexp.MustCompile(`^<!--\s*rev:\d+\s*-->\s*$`)

// splitMarkdown returns the document H1 title and its H2 sections. The text
// before the first H2 (the lead paragraph under the H1) becomes an "Overview"
// section so no content is dropped.
func splitMarkdown(text string) (string, []section) {
	lines := strings.Split(text, "\n")
	var docTitle string
	var secs []section
	cur := section{title: ""} // leading section before any H2 → "Overview"
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
	for _, ln := range lines {
		switch {
		case revTagRE.MatchString(strings.TrimSpace(ln)):
			continue
		case strings.HasPrefix(ln, "# "):
			docTitle = strings.TrimSpace(ln[2:])
			continue
		case strings.HasPrefix(ln, "## "):
			flush()
			cur = section{title: strings.TrimSpace(ln[3:])}
			continue
		}
		cur.body += ln + "\n"
	}
	flush()
	if len(secs) == 0 {
		secs = []section{{title: docTitle, body: strings.TrimSpace(text)}}
	}
	return docTitle, secs
}

// ptMarkerRE counts Portuguese-distinctive tokens to pick the per-concept
// language config (drives FTS ranking). Curated reference docs are often
// English prose with embedded Portuguese domain terms; this keeps each section
// ranked under the config that matches its dominant language.
var ptMarkerRE = regexp.MustCompile(`(?i)\b(não|são|função|implementação|cobrança|devolução|reservas|liquidação|que|para|uma)\b`)

func detectMarkdownLang(body string) string {
	if len(ptMarkerRE.FindAllString(body, 6)) >= 5 {
		return "pt"
	}
	return "en"
}
