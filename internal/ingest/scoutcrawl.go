package ingest

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"pixkb/internal/okf"
)

const scoutCrawlMinBodyChars = 40

type scoutCrawlSource struct {
	dir     string
	baseURL string
}

// NewScoutCrawlSource builds a Source that ingests a Scout `knowledge` crawl
// snapshot's markdown pages (dir should point at the crawl's pages/
// directory). Each page's real content is bracketed by its first H1 heading
// and the literal footer marker "Siga o BC"; pages with no H1 (failed
// capture) or too little content between H1 and footer (list-only stub
// pages) are skipped.
func NewScoutCrawlSource(dir, baseURL string) Source {
	return &scoutCrawlSource{dir: dir, baseURL: strings.TrimSuffix(baseURL, "/")}
}

func (s *scoutCrawlSource) Name() string { return "scout-crawl" }

func (s *scoutCrawlSource) Fetch(_ context.Context) ([]okf.Concept, error) {
	var out []okf.Concept
	err := filepath.WalkDir(s.dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".md") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("scout crawl %s: %w", path, err)
		}
		c, ok, err := s.extract(path, string(data))
		if err != nil {
			return err
		}
		if ok {
			out = append(out, c)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (s *scoutCrawlSource) extract(path, data string) (okf.Concept, bool, error) {
	lines := strings.Split(data, "\n")

	h1Idx := -1
	for i, l := range lines {
		if strings.HasPrefix(l, "# ") {
			h1Idx = i
			break
		}
	}
	if h1Idx < 0 {
		return okf.Concept{}, false, nil // failed capture: no H1 at all
	}
	title := strings.TrimSpace(lines[h1Idx][2:])

	footerIdx := len(lines)
	for i := h1Idx + 1; i < len(lines); i++ {
		if strings.Contains(lines[i], "Siga o BC") {
			footerIdx = i
			break
		}
	}

	body := strings.TrimSpace(strings.Join(lines[h1Idx+1:footerIdx], "\n"))
	meaningful := len(strings.Join(strings.Fields(body), ""))
	if meaningful < scoutCrawlMinBodyChars {
		return okf.Concept{}, false, nil // list-only stub page, no real content
	}

	relPath, err := filepath.Rel(s.dir, path)
	if err != nil {
		return okf.Concept{}, false, fmt.Errorf("scout crawl %s: %w", path, err)
	}
	relPath = filepath.ToSlash(strings.TrimSuffix(relPath, ".md"))

	sourceURL := s.baseURL + "/" + relPath
	if relPath == "index" {
		sourceURL = s.baseURL + "/"
	}

	slug := slugify(relPath)
	fullBody := "# " + title + "\n\n" + body

	tags := []string{"web", "bcb"}
	if i := strings.Index(relPath, "/"); i >= 0 {
		tags = append(tags, relPath[:i])
	}

	return okf.Concept{
		ID:          "web/" + slug + ".md",
		Type:        "WebPage",
		Title:       title,
		Description: firstLine(body),
		Resource:    path,
		Tags:        tags,
		Language:    detectMarkdownLang(body),
		SourceURI:   sourceURL,
		Body:        fullBody,
		ContentSHA:  okf.ComputeSHA(fullBody),
	}, true, nil
}
