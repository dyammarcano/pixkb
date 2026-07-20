package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

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
