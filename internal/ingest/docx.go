package ingest

import (
	"archive/zip"
	"context"
	"encoding/xml"
	"fmt"
	"io"
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
// <w:document>; XMLName pins the decode there and the custom-decoded body
// preserves the document order of paragraphs and tables.
type docxDocument struct {
	XMLName xml.Name `xml:"document"`
	Body    docxBody `xml:"body"`
}

// docxBody walks the body's direct children in order, collecting top-level
// paragraphs and flattening tables (w:tbl) into synthetic body paragraphs so
// tabular text is not silently dropped. A custom decoder is required because
// encoding/xml struct tags cannot preserve the interleaved order of <w:p> and
// <w:tbl> siblings.
type docxBody struct {
	Blocks []docxPara
}

func (b *docxBody) UnmarshalXML(d *xml.Decoder, _ xml.StartElement) error {
	for {
		tok, err := d.Token()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		se, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		switch se.Name.Local {
		case "p":
			var p docxPara
			if err := d.DecodeElement(&p, &se); err != nil {
				return err
			}
			b.Blocks = append(b.Blocks, p)
		case "tbl":
			var t docxTbl
			if err := d.DecodeElement(&t, &se); err != nil {
				return err
			}
			b.Blocks = append(b.Blocks, t.rows()...)
		default:
			if err := d.Skip(); err != nil {
				return err
			}
		}
	}
}

// docxTbl mirrors a w:tbl: rows (w:tr) of cells (w:tc), each cell holding
// paragraphs.
type docxTbl struct {
	Rows []struct {
		Cells []struct {
			Paras []docxPara `xml:"p"`
		} `xml:"tc"`
	} `xml:"tr"`
}

// rows renders each table row as one synthetic body paragraph, joining the
// non-empty cell texts with " | " (no heading style — table text is body).
func (t docxTbl) rows() []docxPara {
	var out []docxPara
	for _, r := range t.Rows {
		var cells []string
		for _, c := range r.Cells {
			var parts []string
			for _, p := range c.Paras {
				if txt := p.text(); txt != "" {
					parts = append(parts, txt)
				}
			}
			if cell := strings.Join(parts, " "); cell != "" {
				cells = append(cells, cell)
			}
		}
		if line := strings.Join(cells, " | "); line != "" {
			out = append(out, docxPara{Texts: []string{line}})
		}
	}
	return out
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
	for _, p := range doc.Body.Blocks {
		txt := p.text()
		// A heading-styled paragraph always starts a new section — even one with
		// empty text, so a stray empty heading does not silently merge the
		// sections on either side of it. An empty-text heading gets the
		// "Overview" fallback title at flush time.
		if isHeadingStyle(p.style()) {
			flush()
			cur = section{title: cleanTitle(txt)}
			continue
		}
		if txt == "" {
			continue
		}
		cur.body += txt + "\n\n"
	}
	flush()
	return secs
}
