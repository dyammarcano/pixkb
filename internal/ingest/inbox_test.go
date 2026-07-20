package ingest

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"pixkb/internal/okf"
)

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644))
}

func conceptByID(cs []okf.Concept, id string) (okf.Concept, bool) {
	for _, c := range cs {
		if c.ID == id {
			return c, true
		}
	}
	return okf.Concept{}, false
}

func TestInboxSource_MissingDirIsEmpty(t *testing.T) {
	t.Parallel()
	cs, err := NewInboxSource(filepath.Join(t.TempDir(), "does-not-exist")).Fetch(context.Background())
	require.NoError(t, err)
	assert.Empty(t, cs)
}

func TestInboxSource_EmptyDirNameIsEmpty(t *testing.T) {
	t.Parallel()
	cs, err := NewInboxSource("").Fetch(context.Background())
	require.NoError(t, err)
	assert.Empty(t, cs)
}

func TestInboxSource_TextBecomesReference(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeFile(t, dir, "note.md", "# My Note\n\nSome body about Pix devolução.\n")
	writeFile(t, dir, "plain.txt", "just plain text without heading")

	cs, err := NewInboxSource(dir).Fetch(context.Background())
	require.NoError(t, err)
	require.Len(t, cs, 2)

	note, ok := conceptByID(cs, "inbox/note.md")
	require.True(t, ok, "expected a concept for note.md, got %v", cs)
	assert.Equal(t, "Reference", note.Type)
	assert.Equal(t, "My Note", note.Title)
	assert.Contains(t, note.Tags, "inbox")
	assert.NotEmpty(t, note.ContentSHA)

	plain, ok := conceptByID(cs, "inbox/plain.md")
	require.True(t, ok)
	assert.Equal(t, "Reference", plain.Type)
	assert.Equal(t, "plain", plain.Title, "no heading -> title falls back to the filename stem")
	assert.Contains(t, plain.Body, "# plain", "a heading is synthesized when the text has none")
}

func TestInboxSource_UnknownTypeBecomesAttachment(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeFile(t, dir, "photo.png", "\x89PNG\r\n\x1a\n binary-ish")
	writeFile(t, dir, "archive.zip", "PK\x03\x04 zip-ish")

	cs, err := NewInboxSource(dir).Fetch(context.Background())
	require.NoError(t, err)
	require.Len(t, cs, 2)

	png, ok := conceptByID(cs, "inbox/attachments/"+slugify("photo.png")+".md")
	require.True(t, ok, "expected an attachment concept for photo.png, got %v", cs)
	assert.Equal(t, "Attachment", png.Type)
	assert.Equal(t, "photo.png", png.Title)
	assert.Contains(t, png.Tags, "attachment")
	assert.Contains(t, png.Body, "not parsed")
	assert.Contains(t, png.Body, "image/png", "mime type is recorded in the body")
}

func TestInboxSource_GenericJSONIsAttachmentNotOpenAPI(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeFile(t, dir, "data.json", `{"hello":"world","numbers":[1,2,3]}`)

	cs, err := NewInboxSource(dir).Fetch(context.Background())
	require.NoError(t, err)
	require.Len(t, cs, 1)
	assert.Equal(t, "Attachment", cs[0].Type, "a non-openapi json must not be parsed as a spec")
}

func TestLooksLikeOpenAPI(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	spec := filepath.Join(dir, "spec.json")
	require.NoError(t, os.WriteFile(spec, []byte(`{"openapi":"3.0.0","paths":{}}`), 0o644))
	plain := filepath.Join(dir, "plain.json")
	require.NoError(t, os.WriteFile(plain, []byte(`{"just":"data"}`), 0o644))

	assert.True(t, looksLikeOpenAPI(spec))
	assert.False(t, looksLikeOpenAPI(plain))
}
