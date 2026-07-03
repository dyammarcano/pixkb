package okf

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)


func TestReadBundle(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, WriteConcept(dir, Concept{ID: "messages/pacs.008.md", Type: "PacsMessage", Body: "a\n"}))
	require.NoError(t, WriteConcept(dir, Concept{ID: "messages/camt.056.md", Type: "CamtMessage", Body: "b\n"}))
	require.NoError(t, WriteConcept(dir, Concept{ID: "repos/pix-api.md", Type: "Repo", Body: "c\n"}))

	got, err := ReadBundle(dir)
	require.NoError(t, err)

	ids := make([]string, 0, len(got))
	for _, c := range got {
		ids = append(ids, c.ID)
	}
	assert.ElementsMatch(t, []string{
		"messages/pacs.008.md",
		"messages/camt.056.md",
		"repos/pix-api.md",
	}, ids)
}

func TestReadBundleSortedDeterministic(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, WriteConcept(dir, Concept{ID: "b.md", Type: "Repo", Body: "b\n"}))
	require.NoError(t, WriteConcept(dir, Concept{ID: "a.md", Type: "Repo", Body: "a\n"}))
	require.NoError(t, WriteConcept(dir, Concept{ID: "messages/z.md", Type: "Repo", Body: "z\n"}))

	got, err := ReadBundle(dir)
	require.NoError(t, err)
	require.Len(t, got, 3)
	assert.Equal(t, "a.md", got[0].ID)
	assert.Equal(t, "b.md", got[1].ID)
	assert.Equal(t, "messages/z.md", got[2].ID)
}

func TestReadBundleSkipsLogIndexAndNonMarkdown(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, WriteConcept(dir, Concept{ID: "messages/pacs.008.md", Type: "PacsMessage", Body: "a\n"}))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "log.md"), []byte("epoch 0\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("ignore me"), 0o644))
	// Generated progressive-disclosure indexes (root + per-directory) are skipped.
	require.NoError(t, os.WriteFile(filepath.Join(dir, "index.md"), []byte("# Index\n"), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "messages"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "messages", "index.md"), []byte("# messages\n"), 0o644))

	got, err := ReadBundle(dir)
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "messages/pacs.008.md", got[0].ID)
}
