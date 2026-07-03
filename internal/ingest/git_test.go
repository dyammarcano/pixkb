package ingest

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGitSource_ReadsMirror(t *testing.T) {
	t.Parallel()
	mirror := t.TempDir()
	repoDir := filepath.Join(mirror, "pix-api")
	require.NoError(t, os.MkdirAll(repoDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(repoDir, "README.md"),
		[]byte("# pix-api\n\nOfficial Pix API specifications."), 0o644))

	src := NewGitSource([]RepoSpec{{Owner: "bacen", Name: "pix-api"}}, mirror)
	assert.Equal(t, "git", src.Name())

	cs, err := src.Fetch(context.Background())
	require.NoError(t, err)
	require.Len(t, cs, 1)
	c := cs[0]
	assert.Equal(t, "repos/pix-api.md", c.ID)
	assert.Equal(t, "Repo", c.Type)
	assert.Equal(t, "bacen/pix-api", c.Title)
	assert.Contains(t, c.Body, "Official Pix API specifications")
	assert.NotEmpty(t, c.ContentSHA)
}

func TestGitSource_SkipsMissingMirror(t *testing.T) {
	t.Parallel()
	src := NewGitSource([]RepoSpec{{Owner: "bacen", Name: "absent"}}, t.TempDir())
	cs, err := src.Fetch(context.Background())
	require.NoError(t, err)
	assert.Empty(t, cs)
}
