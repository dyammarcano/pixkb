package ingest

import (
	"context"
	"fmt"
	"html"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"pixkb/internal/okf"
)

type apiDocSource struct{ files []string }

// NewAPIDocSource builds a Source that turns local API-DICT HTML pages into
// ApiEndpoint concepts (one per file). Pure stdlib HTML-to-text — no browser,
// air-gap friendly.
func NewAPIDocSource(htmlFiles []string) Source { return &apiDocSource{files: htmlFiles} }

func (s *apiDocSource) Name() string { return "api-doc" }

func (s *apiDocSource) Fetch(_ context.Context) ([]okf.Concept, error) {
	var out []okf.Concept
	for _, f := range s.files {
		data, err := os.ReadFile(f)
		if err != nil {
			return nil, fmt.Errorf("apidoc %s: %w", f, err)
		}
		raw := string(data)
		text := stripHTML(raw)
		slug := slugify(strings.TrimSuffix(filepath.Base(f), filepath.Ext(f)))
		title := htmlTitle(raw)
		if title == "" {
			title = slug
		}
		body := "# " + title + "\n\n" + text
		out = append(out, okf.Concept{
			ID:          "apis/" + slug + ".md",
			Type:        "ApiEndpoint",
			Title:       title,
			Description: firstLine(text),
			Resource:    f,
			Tags:        []string{"api", "dict", slug},
			Language:    "pt",
			SourceURI:   "apidoc:" + filepath.Base(f),
			Body:        body,
			ContentSHA:  okf.ComputeSHA(body),
		})
	}
	return out, nil
}

var (
	tagRE        = regexp.MustCompile(`(?is)<[^>]*>`)
	blockEndRE   = regexp.MustCompile(`(?is)</(p|div|h[1-6]|li|tr|section|article)>|<br\s*/?>`)
	scriptStyle  = regexp.MustCompile(`(?is)<(script|style)[^>]*>.*?</(script|style)>`)
	titleTagRE   = regexp.MustCompile(`(?is)<title[^>]*>(.*?)</title>`)
	multiNewline = regexp.MustCompile(`\n{2,}`)
)

// stripHTML converts HTML to readable plain text, preserving block breaks so
// downstream heading detection still works.
func stripHTML(s string) string {
	s = scriptStyle.ReplaceAllString(s, " ")
	s = blockEndRE.ReplaceAllString(s, "\n")
	s = tagRE.ReplaceAllString(s, "")
	s = html.UnescapeString(s)
	// collapse runs of spaces per line, then runs of blank lines
	lines := strings.Split(s, "\n")
	for i, ln := range lines {
		lines[i] = strings.Join(strings.Fields(ln), " ")
	}
	s = strings.Join(lines, "\n")
	return strings.TrimSpace(multiNewline.ReplaceAllString(s, "\n\n"))
}

func htmlTitle(s string) string {
	if m := titleTagRE.FindStringSubmatch(s); m != nil {
		return strings.TrimSpace(html.UnescapeString(m[1]))
	}
	return ""
}
