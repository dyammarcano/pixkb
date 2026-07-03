package epoch

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGitCommitter_InitsAndCommits(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "index.md"), []byte("# kb\n"), 0o644))

	c := NewGitCommitter(dir)
	sha, err := c.Commit(context.Background(), "epoch 0")
	require.NoError(t, err)
	assert.Len(t, sha, 40, "full git sha")

	// An epoch with no file changes still commits (AllowEmptyCommits).
	sha2, err := c.Commit(context.Background(), "epoch 1")
	require.NoError(t, err)
	assert.NotEqual(t, sha, sha2)
}
