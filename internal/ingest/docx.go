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

// docxDocument mirrors the read parts of word/document.xml. The root element is
// <w:document>; XMLName pins the decode there and xml:"body>p" collects the
// body's paragraphs.
type docxDocument struct {
	XMLName xml.Name   `xml:"document"`
	Paras   []docxPara `xml:"body>p"`
}

type docxPara struct {
	PPr struct {
		PStyle struct {
			Val string `xml:"val,attr"`
		} `xml:"pStyle"`
	} `xml:"pPr"`
	Texts []string `xml:"r>t"`
}

func (p docxPara) text() string {
	return strings.TrimSpace(strings.Join(p.Texts, ""))
}

func (p docxPara) style() string { return p.PPr.PStyle.Val }

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
	for _, p := range doc.Paras {
		txt := p.text()
		if txt == "" {
			continue
		}
		if isHeadingStyle(p.style()) {
			flush()
			cur = section{title: cleanTitle(txt)}
			continue
		}
		cur.body += txt + "\n\n"
	}
	flush()
	return secs
}
