package okf

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAppendLogCreatesAndAppends(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, AppendLog(dir, "epoch 0: initial gather"))
	require.NoError(t, AppendLog(dir, "epoch 1: +pacs.009"))

	raw, err := os.ReadFile(filepath.Join(dir, "log.md"))
	require.NoError(t, err)
	lines := strings.Split(strings.TrimRight(string(raw), "\n"), "\n")
	require.Len(t, lines, 2)
	assert.Equal(t, "epoch 0: initial gather", lines[0])
	assert.Equal(t, "epoch 1: +pacs.009", lines[1])
}

func TestAppendLogTrimsTrailingNewlineInInput(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, AppendLog(dir, "line with newline\n"))
	raw, err := os.ReadFile(filepath.Join(dir, "log.md"))
	require.NoError(t, err)
	assert.Equal(t, "line with newline\n", string(raw))
}

func TestAppendLogCreatesParentDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "kb")
	require.NoError(t, AppendLog(dir, "first"))
	_, err := os.Stat(filepath.Join(dir, "log.md"))
	require.NoError(t, err)
}
