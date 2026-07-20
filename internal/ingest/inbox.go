package ingest

import (
	"context"
	"fmt"
	"mime"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"pixkb/internal/okf"
)

type inboxSource struct{ dir string }

// NewInboxSource builds a Source over an "inbox" directory of ad-hoc dropped
// files (the Dump/Ingest UI's staging area). Each file is classified by
// extension and delegated to the matching specialized source
// (PDF/DOCX/XLSX/OpenAPI); plain text/markdown becomes a Reference concept; any
// other type becomes an Attachment concept indexed by filename + metadata so it
// stays findable and downloadable. A missing dir yields no concepts (not an
// error), so it is always safe to include in buildSources.
func NewInboxSource(dir string) Source { return &inboxSource{dir: dir} }

func (s *inboxSource) Name() string { return "inbox" }

func (s *inboxSource) Fetch(ctx context.Context) ([]okf.Concept, error) {
	if s.dir == "" {
		return nil, nil
	}
	files, err := collectInboxFiles(s.dir)
	if err != nil {
		return nil, err
	}
	var out []okf.Concept
	for _, f := range files {
		switch strings.ToLower(filepath.Ext(f)) {
		case ".pdf":
			cs, err := NewPDFSource([]string{f}).Fetch(ctx)
			if err != nil {
				return nil, fmt.Errorf("inbox pdf %s: %w", f, err)
			}
			out = append(out, cs...)
		case ".docx":
			cs, err := NewDocxSource([]string{f}).Fetch(ctx)
			if err != nil {
				return nil, fmt.Errorf("inbox docx %s: %w", f, err)
			}
			out = append(out, cs...)
		case ".xlsx":
			cs, err := NewXlsxSource([]string{f}).Fetch(ctx)
			if err != nil {
				return nil, fmt.Errorf("inbox xlsx %s: %w", f, err)
			}
			out = append(out, cs...)
		case ".json", ".yaml", ".yml":
			if looksLikeOpenAPI(f) {
				cs, err := NewOpenAPISource([]string{f}).Fetch(ctx)
				if err != nil {
					return nil, fmt.Errorf("inbox openapi %s: %w", f, err)
				}
				out = append(out, cs...)
			} else {
				out = append(out, inboxAttachment(f))
			}
		case ".md", ".markdown", ".txt":
			c, err := inboxText(f)
			if err != nil {
				return nil, err
			}
			out = append(out, c)
		default:
			out = append(out, inboxAttachment(f))
		}
	}
	return out, nil
}

// collectInboxFiles returns the regular files under dir (recursively), sorted
// for deterministic output. A non-existent dir is not an error — it means an
// empty inbox.
func collectInboxFiles(dir string) ([]string, error) {
	info, err := os.Stat(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("inbox: %s is not a directory", dir)
	}
	var files []string
	err = filepath.Walk(dir, func(path string, fi os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !fi.IsDir() {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(files)
	return files, nil
}

// inboxText turns a dropped text/markdown file into a single Reference concept.
func inboxText(f string) (okf.Concept, error) {
	data, err := os.ReadFile(f)
	if err != nil {
		return okf.Concept{}, fmt.Errorf("inbox text %s: %w", f, err)
	}
	content := string(data)
	base := filepath.Base(f)
	// Use the first line as the title only when it is an actual markdown heading;
	// otherwise fall back to the filename stem (predictable for plain text).
	var title string
	if first := strings.TrimSpace(firstLine(content)); strings.HasPrefix(first, "#") {
		title = strings.TrimSpace(strings.TrimLeft(first, "# "))
	}
	if title == "" {
		title = strings.TrimSuffix(base, filepath.Ext(base))
	}
	body := content
	if !strings.HasPrefix(strings.TrimSpace(body), "#") {
		body = "# " + title + "\n\n" + content
	}
	return okf.Concept{
		ID:          "inbox/" + slugify(strings.TrimSuffix(base, filepath.Ext(base))) + ".md",
		Type:        "Reference",
		Title:       title,
		Description: firstLine(content),
		Resource:    f,
		Tags:        []string{"inbox"},
		SourceURI:   "inbox:" + base,
		Body:        body,
		ContentSHA:  okf.ComputeSHA(body),
	}, nil
}

// inboxAttachment turns an arbitrary (unparseable) dropped file into an
// Attachment concept: it is not parsed, but stays findable by filename + type +
// size and downloadable from the on-disk path.
func inboxAttachment(f string) okf.Concept {
	base := filepath.Base(f)
	ext := strings.ToLower(filepath.Ext(f))
	mimeType := mime.TypeByExtension(ext)
	if mimeType == "" {
		mimeType = "application/octet-stream"
	}
	var size int64 = -1
	if fi, err := os.Stat(f); err == nil {
		size = fi.Size()
	}
	body := fmt.Sprintf("# %s\n\nAttachment stored in the inbox (not parsed).\n\n- filename: %s\n- type: %s\n- size: %d bytes\n",
		base, base, mimeType, size)
	return okf.Concept{
		ID:          "inbox/attachments/" + slugify(base) + ".md",
		Type:        "Attachment",
		Title:       base,
		Description: fmt.Sprintf("%s attachment (%d bytes)", mimeType, size),
		Resource:    f,
		Tags:        []string{"inbox", "attachment"},
		SourceURI:   "inbox:" + base,
		Body:        body,
		ContentSHA:  okf.ComputeSHA(body),
	}
}

// looksLikeOpenAPI sniffs the first bytes of a JSON/YAML file for an openapi or
// swagger marker, so a generic .json/.yaml is not mis-parsed as a spec.
func looksLikeOpenAPI(f string) bool {
	fh, err := os.Open(f)
	if err != nil {
		return false
	}
	defer func() { _ = fh.Close() }()
	buf := make([]byte, 4096)
	n, _ := fh.Read(buf)
	head := strings.ToLower(string(buf[:n]))
	return strings.Contains(head, "openapi") || strings.Contains(head, "swagger")
}
