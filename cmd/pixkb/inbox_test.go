package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSafeName(t *testing.T) {
	t.Parallel()
	ok := []struct{ in, want string }{
		{"file.pdf", "file.pdf"},
		{"a/b/c.txt", "c.txt"},
		{`x\y\z.md`, "z.md"},
	}
	for _, c := range ok {
		got, valid := safeName(c.in)
		assert.True(t, valid, "%q should be valid", c.in)
		assert.Equal(t, c.want, got)
	}
	// Traversal is neutralized to the base element (stays inside the inbox), not
	// an escape.
	got, valid := safeName("../../etc/passwd")
	assert.True(t, valid)
	assert.Equal(t, "passwd", got, "traversal must reduce to the bare filename")
	for _, bad := range []string{"", ".", ".."} {
		_, valid := safeName(bad)
		assert.False(t, valid, "%q must be rejected", bad)
	}
}

func TestInboxServer_ListAndDelete(t *testing.T) {
	t.Parallel()
	base := t.TempDir()
	s := &inboxServer{cfg: Config{IngestDir: base}}
	require.NoError(t, os.MkdirAll(s.dir(), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(s.dir(), "a.txt"), []byte("x"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(s.dir(), "b.md"), []byte("yy"), 0o644))

	// GET /inbox lists staged files.
	rec := httptest.NewRecorder()
	s.handleList(rec, httptest.NewRequest(http.MethodGet, "/inbox", nil))
	require.Equal(t, 200, rec.Code)
	assert.Contains(t, rec.Body.String(), "a.txt")
	assert.Contains(t, rec.Body.String(), "b.md")

	// DELETE removes one.
	rec = httptest.NewRecorder()
	s.handleList(rec, httptest.NewRequest(http.MethodDelete, "/inbox?name=a.txt", nil))
	require.Equal(t, 204, rec.Code)
	_, err := os.Stat(filepath.Join(s.dir(), "a.txt"))
	assert.True(t, os.IsNotExist(err), "a.txt should be gone")

	// DELETE with an invalid name (bare "..") is rejected.
	rec = httptest.NewRecorder()
	s.handleList(rec, httptest.NewRequest(http.MethodDelete, "/inbox?name=..", nil))
	assert.Equal(t, 400, rec.Code)
}

func TestInboxServer_ListMissingDirIsEmpty(t *testing.T) {
	t.Parallel()
	s := &inboxServer{cfg: Config{IngestDir: filepath.Join(t.TempDir(), "nope")}}
	items, err := s.list()
	require.NoError(t, err)
	assert.Empty(t, items)
}

func TestInboxServer_URLRejectsNonHTTP(t *testing.T) {
	t.Parallel()
	s := &inboxServer{cfg: Config{IngestDir: t.TempDir()}}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/inbox/url", nil)
	// no body -> decode error -> 400
	s.handleURL(rec, req)
	assert.Equal(t, 400, rec.Code)
}

func TestClassifyFetched(t *testing.T) {
	t.Parallel()

	// A PDF (by content-type) is kept as raw bytes in a .pdf file, never mangled.
	pdfBytes := []byte("%PDF-1.7\n\xe2\xe3\xcf\xd3 binary")
	name, content := classifyFetched("https://x/informe.pdf", "", "application/pdf", pdfBytes)
	assert.Equal(t, ".pdf", filepath.Ext(name))
	assert.Equal(t, pdfBytes, content, "PDF bytes must be preserved verbatim")

	// A PDF by URL extension even when the server sends octet-stream.
	name, _ = classifyFetched("https://x/doc.pdf", "", "application/octet-stream", pdfBytes)
	assert.Equal(t, ".pdf", filepath.Ext(name))

	// HTML is converted to text and any invalid UTF-8 is stripped.
	html := []byte("<html><body><h1>Ol\xe1</h1><p>t\xe3xto</p></body></html>")
	name, content = classifyFetched("https://x/page", "T", "text/html; charset=utf-8", html)
	assert.Equal(t, ".md", filepath.Ext(name))
	assert.True(t, utf8.Valid(content), "html->md output must be valid UTF-8")
	assert.NotContains(t, string(content), "<h1>")

	// An unknown binary keeps its bytes as an attachment (not a .md).
	name, content = classifyFetched("https://x/pic.png", "", "image/png", []byte{0x89, 'P', 'N', 'G'})
	assert.Equal(t, ".png", filepath.Ext(name))
	assert.Equal(t, []byte{0x89, 'P', 'N', 'G'}, content)
}

func TestHTMLToText(t *testing.T) {
	t.Parallel()
	html := `<html><head><style>.x{}</style><script>bad()</script></head>` +
		`<body><h1>Hi&amp;Bye</h1><p>Line&nbsp;one</p></body></html>`
	got := htmlToText(html)
	assert.NotContains(t, got, "<")
	assert.NotContains(t, got, "bad()")
	assert.Contains(t, got, "Hi&Bye")
	assert.Contains(t, got, "Line one")
}
