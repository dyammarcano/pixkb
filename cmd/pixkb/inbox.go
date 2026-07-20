package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"pixkb/internal/ingest"
)

// inboxServer backs the Dump/Ingest UI: it stages ad-hoc files and fetched URLs
// under <ingest_dir>/inbox and, on explicit request, runs the same ingest
// pipeline as `pixkb ingest` (which now includes the inbox source). It is only
// mounted by `serve --ask`.
type inboxServer struct {
	cfg Config
	mu  sync.Mutex // serialize ingest runs (one epoch cut at a time)
}

func (s *inboxServer) dir() string { return filepath.Join(s.cfg.IngestDir, "inbox") }

// safeName reduces a client-supplied name to a single path element inside the
// inbox, rejecting empties and traversal attempts.
func safeName(name string) (string, bool) {
	base := filepath.Base(filepath.FromSlash(name))
	if base == "" || base == "." || base == ".." || strings.ContainsAny(base, `/\`) {
		return "", false
	}
	return base, true
}

type inboxItem struct {
	Name string `json:"name"`
	Size int64  `json:"size"`
}

// list returns the staged files, newest-name-sorted, for GET /inbox.
func (s *inboxServer) list() ([]inboxItem, error) {
	entries, err := os.ReadDir(s.dir())
	if err != nil {
		if os.IsNotExist(err) {
			return []inboxItem{}, nil
		}
		return nil, err
	}
	items := make([]inboxItem, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		items = append(items, inboxItem{Name: e.Name(), Size: info.Size()})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Name < items[j].Name })
	return items, nil
}

func (s *inboxServer) handleList(w http.ResponseWriter, req *http.Request) {
	switch req.Method {
	case http.MethodGet:
		items, err := s.list()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, items)
	case http.MethodDelete:
		name, ok := safeName(req.URL.Query().Get("name"))
		if !ok {
			http.Error(w, "invalid name", http.StatusBadRequest)
			return
		}
		if err := os.Remove(filepath.Join(s.dir(), name)); err != nil && !os.IsNotExist(err) {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *inboxServer) handleUpload(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := req.ParseMultipartForm(64 << 20); err != nil {
		http.Error(w, "invalid multipart form: "+err.Error(), http.StatusBadRequest)
		return
	}
	if err := os.MkdirAll(s.dir(), 0o755); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	saved := []string{}
	for _, fhs := range req.MultipartForm.File {
		for _, fh := range fhs {
			name, ok := safeName(fh.Filename)
			if !ok {
				continue
			}
			if err := saveMultipart(fh, filepath.Join(s.dir(), name)); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			saved = append(saved, name)
		}
	}
	writeJSON(w, map[string]any{"saved": saved})
}

// saveMultipart copies one uploaded file to dst.
func saveMultipart(fh *multipart.FileHeader, dst string) error {
	src, err := fh.Open()
	if err != nil {
		return err
	}
	defer func() { _ = src.Close() }()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer func() { _ = out.Close() }()
	_, err = io.Copy(out, src)
	return err
}

// urlRequest is the POST /inbox/url body.
type urlRequest struct {
	URL   string `json:"url"`
	Title string `json:"title"`
}

func (s *inboxServer) handleURL(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body urlRequest
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		http.Error(w, "invalid JSON body", http.StatusBadRequest)
		return
	}
	u := strings.TrimSpace(body.URL)
	if u == "" || !(strings.HasPrefix(u, "http://") || strings.HasPrefix(u, "https://")) {
		http.Error(w, "url must be http(s)", http.StatusBadRequest)
		return
	}
	if err := os.MkdirAll(s.dir(), 0o755); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	name, content, fetched := fetchURL(req.Context(), u, body.Title)
	if err := os.WriteFile(filepath.Join(s.dir(), name), content, 0o644); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]any{"saved": name, "fetched": fetched})
}

func (s *inboxServer) handleIngest(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	ctx := req.Context()
	r, st, err := newRunner(ctx, s.cfg)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer st.Close()
	concepts, err := ingest.GatherAll(ctx, buildSources(s.cfg))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	concepts = ingest.CrossLink(concepts)
	res, err := r.Run(ctx, concepts, "inbox")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]any{
		"epoch": res.Epoch, "added": res.Added, "changed": res.Changed,
		"removed": res.Removed, "commit": short(res.Commit),
	})
}

// fetchURL GETs u and returns (filename, file-bytes, fetched). It routes by
// content type so binary responses are never mangled into a text/markdown file:
// a PDF is saved as a real .pdf (parsed by the inbox PDF source), HTML/text is
// converted to UTF-8-sanitized markdown, and any other binary is kept as-is (an
// attachment). On any failure it returns a link-only markdown stub, so a URL is
// always staged (the operator's "fetch when online, else store link" choice).
func fetchURL(ctx context.Context, u, title string) (string, []byte, bool) {
	linkOnly := func(note string) (string, []byte, bool) {
		t := title
		if t == "" {
			t = u
		}
		return ingest.Slugify(u) + ".md", fmt.Appendf(nil, "# %s\n\nSource: %s\n\n%s\n", t, u, note), false
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	reqh, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return linkOnly("(link only — request could not be built)")
	}
	resp, err := http.DefaultClient.Do(reqh)
	if err != nil {
		return linkOnly("(link only — fetch failed offline or blocked)")
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 400 {
		return linkOnly(fmt.Sprintf("(link only — server returned %d)", resp.StatusCode))
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return linkOnly("(link only — response could not be read)")
	}
	name, content := classifyFetched(u, title, resp.Header.Get("Content-Type"), raw)
	return name, content, true
}

// classifyFetched decides how to stage a successfully-fetched URL body, keyed by
// content type (falling back to the URL's extension). PDFs stay binary as .pdf;
// HTML/text becomes UTF-8-sanitized markdown (invalid bytes stripped so the
// Postgres UTF8 upsert never fails); anything else is kept raw as an attachment.
// Pure (no I/O) so it is unit-testable.
func classifyFetched(u, title, contentType string, raw []byte) (string, []byte) {
	slug := ingest.Slugify(u)
	ct := strings.ToLower(contentType)
	clean := u
	if i := strings.IndexAny(clean, "?#"); i >= 0 {
		clean = clean[:i]
	}
	ext := strings.ToLower(filepath.Ext(clean))

	switch {
	case strings.Contains(ct, "pdf") || ext == ".pdf":
		return slug + ".pdf", raw
	case strings.Contains(ct, "html") || strings.Contains(ct, "xml") || strings.HasPrefix(ct, "text/") ||
		(ct == "" && (ext == "" || ext == ".html" || ext == ".htm" || ext == ".txt" || ext == ".md")):
		t := title
		if t == "" {
			t = u
		}
		text := string(raw)
		if strings.Contains(ct, "html") || strings.Contains(ct, "xml") || ext == ".html" || ext == ".htm" {
			text = htmlToText(text)
		}
		text = strings.ToValidUTF8(text, "") // drop invalid bytes — Postgres upsert requires valid UTF-8
		return slug + ".md", fmt.Appendf(nil, "# %s\n\nSource: %s\n\n%s\n", t, u, strings.TrimSpace(text))
	default:
		if ext == "" {
			ext = ".bin"
		}
		return slug + ext, raw
	}
}

var (
	htmlScriptStyle = regexp.MustCompile(`(?is)<(script|style)[^>]*>.*?</(script|style)>`)
	htmlTag         = regexp.MustCompile(`(?is)<[^>]*>`)
	htmlBlanks      = regexp.MustCompile(`\n{3,}`)
)

// htmlToText is a minimal, dependency-free HTML-to-text reduction for staged
// URLs (the ingest pipeline re-parses the markdown later).
func htmlToText(s string) string {
	s = htmlScriptStyle.ReplaceAllString(s, "")
	s = htmlTag.ReplaceAllString(s, "")
	s = strings.ReplaceAll(s, "&nbsp;", " ")
	s = strings.ReplaceAll(s, "&amp;", "&")
	s = strings.ReplaceAll(s, "&lt;", "<")
	s = strings.ReplaceAll(s, "&gt;", ">")
	s = htmlBlanks.ReplaceAllString(s, "\n\n")
	return strings.TrimSpace(s)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
