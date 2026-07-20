package main

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFeedbackServer_AppendAndList(t *testing.T) {
	t.Parallel()
	s := &feedbackServer{path: filepath.Join(t.TempDir(), "feedback.jsonl")}

	// Empty log lists as [].
	rec := httptest.NewRecorder()
	s.handle(rec, httptest.NewRequest(http.MethodGet, "/feedback", nil))
	require.Equal(t, 200, rec.Code)
	assert.Equal(t, "[]\n", rec.Body.String())

	// Append two entries.
	post := func(body string) int {
		r := httptest.NewRecorder()
		s.handle(r, httptest.NewRequest(http.MethodPost, "/feedback", strings.NewReader(body)))
		return r.Code
	}
	require.Equal(t, 204, post(`{"question":"o que é MED?","answer":"...","verdict":"up"}`))
	require.Equal(t, 204, post(`{"question":"chave pix?","answer":"...","verdict":"down","note":"faltou detalhe"}`))

	// List returns both, with a server-stamped ts.
	rec = httptest.NewRecorder()
	s.handle(rec, httptest.NewRequest(http.MethodGet, "/feedback", nil))
	require.Equal(t, 200, rec.Code)
	entries, err := readFeedback(s.path)
	require.NoError(t, err)
	require.Len(t, entries, 2)
	assert.Equal(t, "up", entries[0].Verdict)
	assert.Equal(t, "down", entries[1].Verdict)
	assert.Equal(t, "faltou detalhe", entries[1].Note)
	assert.NotEmpty(t, entries[0].TS, "server stamps a timestamp")
}

func TestFeedbackServer_Validation(t *testing.T) {
	t.Parallel()
	s := &feedbackServer{path: filepath.Join(t.TempDir(), "feedback.jsonl")}
	bad := func(body string) int {
		r := httptest.NewRecorder()
		s.handle(r, httptest.NewRequest(http.MethodPost, "/feedback", strings.NewReader(body)))
		return r.Code
	}
	assert.Equal(t, 400, bad(`{not json`), "invalid json")
	assert.Equal(t, 400, bad(`{"question":"","verdict":"up"}`), "missing question")
	assert.Equal(t, 400, bad(`{"question":"q","verdict":"maybe"}`), "invalid verdict")

	r := httptest.NewRecorder()
	s.handle(r, httptest.NewRequest(http.MethodPut, "/feedback", nil))
	assert.Equal(t, 405, r.Code, "only GET/POST allowed")
}
