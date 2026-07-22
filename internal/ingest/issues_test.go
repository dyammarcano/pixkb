package ingest

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIssuesSource_FetchParsesAndSkipsPRs(t *testing.T) {
	t.Parallel()
	const payload = `[
	  {"number":445,"title":"DICT timeout","body":"body A","html_url":"https://github.com/bacen/pix-api/issues/445","state":"open"},
	  {"number":446,"title":"a PR not an issue","body":"pr","html_url":"https://github.com/bacen/pix-api/pull/446","state":"open","pull_request":{"url":"x"}}
	]`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Contains(t, r.URL.Path, "/repos/bacen/pix-api/issues")
		assert.Equal(t, "open", r.URL.Query().Get("state"))
		_, _ = w.Write([]byte(payload))
	}))
	defer srv.Close()

	s := &issuesSource{repos: []string{"bacen/pix-api"}, baseURL: srv.URL, client: srv.Client()}
	cs, err := s.Fetch(context.Background())
	require.NoError(t, err)
	require.Len(t, cs, 1, "the PR must be skipped, only the real issue kept")

	c := cs[0]
	assert.Equal(t, "issues/bacen-pix-api-445.md", c.ID)
	assert.Equal(t, "Issue", c.Type)
	assert.Equal(t, "#445 DICT timeout", c.Title)
	assert.Equal(t, "https://github.com/bacen/pix-api/issues/445", c.SourceURI)
	assert.Contains(t, c.Tags, "issue")
	assert.NotEmpty(t, c.ContentSHA)
}

func TestIssuesSource_TolerantOfFetchFailure(t *testing.T) {
	t.Parallel()
	// A server that always 500s → the repo is skipped, Fetch still succeeds
	// (so an offline/rate-limited gather never breaks the whole ingest).
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	s := &issuesSource{repos: []string{"bacen/pix-api"}, baseURL: srv.URL, client: srv.Client()}
	cs, err := s.Fetch(context.Background())
	require.NoError(t, err, "a failing repo must not fail the source")
	assert.Empty(t, cs)
}

func TestIssuesSource_RejectsBadRepoName(t *testing.T) {
	t.Parallel()
	s := &issuesSource{repos: []string{"not-owner-slash-name"}, baseURL: "http://unused"}
	cs, err := s.Fetch(context.Background())
	require.NoError(t, err) // tolerant: bad name is logged + skipped
	assert.Empty(t, cs)
}
