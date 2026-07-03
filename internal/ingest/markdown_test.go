package ingest

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMarkdownSource(t *testing.T) {
	t.Parallel()
	doc := `# Pix ID Vocabulary
<!-- rev:001 -->

Lead paragraph under the H1.

## end_to_end_id

The canonical BACEN identifier. Prefix ` + "`E`" + `.

### Format

32 chars.

## rtr_id

The return identifier. Prefix ` + "`D`" + `.
`
	dir := t.TempDir()
	f := filepath.Join(dir, "bacen-pix-id-vocabulary.md")
	require.NoError(t, os.WriteFile(f, []byte(doc), 0o644))

	got, err := NewMarkdownSource([]string{f}).Fetch(context.Background())
	require.NoError(t, err)
	require.Len(t, got, 3) // Overview + 2 H2 sections

	byTitle := map[string]int{}
	for i, c := range got {
		assert.Equal(t, "Reference", c.Type)
		assert.NotEmpty(t, c.ContentSHA)
		assert.NotContains(t, c.Body, "rev:001") // revision tag stripped
		byTitle[c.Title] = i
	}

	// Lead paragraph preserved as an Overview concept.
	ov, ok := byTitle["Overview"]
	require.True(t, ok)
	assert.Contains(t, got[ov].Body, "Lead paragraph")

	// H2 section: nested H3 stays in the body; id/anchor stable + ordered.
	e2e, ok := byTitle["end_to_end_id"]
	require.True(t, ok)
	assert.Contains(t, got[e2e].Body, "### Format")
	assert.Equal(t, "reference/bacen-pix-id-vocabulary/01-end-to-end-id.md", got[e2e].ID)
}
