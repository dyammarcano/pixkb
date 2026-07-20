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
	md, fetched := fetchAsMarkdown(req.Context(), u, body.Title)
	name := ingest.Slugify(u) + ".md"
	if err := os.WriteFile(filepath.Join(s.dir(), name), []byte(md), 0o644); err != nil {
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

// fetchAsMarkdown GETs u and converts an HTML/text response to a markdown
// concept body. On any failure it falls back to a link-only stub, so a URL is
// always staged (the operator's "fetch when online, else store link" choice).
// The bool reports whether the fetch succeeded.
func fetchAsMarkdown(ctx context.Context, u, title string) (string, bool) {
	link := func(note string) string {
		t := title
		if t == "" {
			t = u
		}
		return fmt.Sprintf("# %s\n\nSource: %s\n\n%s\n", t, u, note)
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	reqh, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return link("(link only — request could not be built)"), false
	}
	resp, err := http.DefaultClient.Do(reqh)
	if err != nil {
		return link("(link only — fetch failed offline or blocked)"), false
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 400 {
		return link(fmt.Sprintf("(link only — server returned %d)", resp.StatusCode)), false
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return link("(link only — response could not be read)"), false
	}
	text := htmlToText(string(raw))
	t := title
	if t == "" {
		t = u
	}
	return fmt.Sprintf("# %s\n\nSource: %s\n\n%s\n", t, u, text), true
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
