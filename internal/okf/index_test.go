package okf

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWriteIndexes_RootAndPerDir(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, WriteConcept(dir, Concept{ID: "messages/pacs.008.md", Type: "PacsMessage", Title: "pacs.008", Body: "a\n"}))
	require.NoError(t, WriteConcept(dir, Concept{ID: "messages/camt.056.md", Type: "CamtMessage", Title: "camt.056", Body: "b\n"}))
	require.NoError(t, WriteConcept(dir, Concept{ID: "apis/dict/lookup.md", Type: "ApiEndpoint", Title: "lookup", Body: "c\n"}))

	require.NoError(t, WriteIndexes(dir))

	// Root index links to child directories/concepts.
	root, err := os.ReadFile(filepath.Join(dir, "index.md"))
	require.NoError(t, err)
	assert.Contains(t, string(root), "# ")
	assert.Contains(t, string(root), "messages")
	assert.Contains(t, string(root), "apis")
	// Assert correctly-formatted forward-slash markdown links to subdirectories.
	assert.Contains(t, string(root), "[messages](messages/index.md)")
	assert.Contains(t, string(root), "[apis](apis/index.md)")

	// Per-directory index lists its concepts.
	msgIdx, err := os.ReadFile(filepath.Join(dir, "messages", "index.md"))
	require.NoError(t, err)
	assert.Contains(t, string(msgIdx), "pacs.008.md")
	assert.Contains(t, string(msgIdx), "camt.056.md")
	// Assert correctly-formatted forward-slash relative path links to child concepts.
	assert.Contains(t, string(msgIdx), "[pacs.008.md](pacs.008.md)")
	assert.Contains(t, string(msgIdx), "[camt.056.md](camt.056.md)")

	// Generated indexes are NOT treated as concepts.
	concepts, err := ReadBundle(dir)
	require.NoError(t, err)
	for _, c := range concepts {
		assert.False(t, strings.HasSuffix(c.ID, "index.md"), "index.md must not be a concept: %s", c.ID)
	}
	require.Len(t, concepts, 3)
}
