package main

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"pixkb/internal/okf"
)

// readTar opens a tar.gz and returns the set of entry names it contains.
func readTar(t *testing.T, path string) map[string]bool {
	t.Helper()
	f, err := os.Open(path)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()
	gz, err := gzip.NewReader(f)
	require.NoError(t, err)
	defer func() { _ = gz.Close() }()
	tr := tar.NewReader(gz)
	names := map[string]bool{}
	for {
		hdr, err := tr.Next()
		if err != nil {
			break
		}
		names[hdr.Name] = true
	}
	return names
}

func TestTarDir(t *testing.T) {
	tests := []struct {
		name      string
		files     map[string]string // relpath -> content
		wantNames []string
	}{
		{
			name:      "flat files",
			files:     map[string]string{"a.md": "alpha", "b.md": "beta"},
			wantNames: []string{"a.md", "b.md"},
		},
		{
			name: "nested tree uses slash paths",
			files: map[string]string{
				"index.md":             "idx",
				"messages/pacs.008.md": "credit transfer",
				"messages/pacs.002.md": "status",
			},
			wantNames: []string{"index.md", "messages/pacs.008.md", "messages/pacs.002.md"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			for rel, content := range tt.files {
				full := filepath.Join(dir, filepath.FromSlash(rel))
				require.NoError(t, os.MkdirAll(filepath.Dir(full), 0o755))
				require.NoError(t, os.WriteFile(full, []byte(content), 0o644))
			}
			// Output lives outside the source dir so it cannot be re-included.
			out := filepath.Join(t.TempDir(), "bundle.tar.gz")

			require.NoError(t, tarDir(dir, out))

			names := readTar(t, out)
			for _, want := range tt.wantNames {
				assert.True(t, names[want], "tar missing entry %q", want)
			}
			assert.Len(t, names, len(tt.wantNames))
		})
	}
}

// TestSearchHandler exercises `pixkb serve`'s /search handler directly via
// httptest — added for the format=/explain= HTTP surface (/steps:next item
// 10, 2026-07-04). Shares the same shared-uncleaned-test-DB discipline as
// every other internal/store/postgres-backed test in this repo: a
// unique-per-run term guarantees an unambiguous FTS hit regardless of what
// other rows already exist.
func TestSearchHandler(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PIXKB_TEST_DSN postgres")
	}
	dsn := os.Getenv("PIXKB_TEST_DSN")
	if dsn == "" {
		t.Skip("PIXKB_TEST_DSN not set")
	}
	if prod := os.Getenv("PIXKB_DSN"); prod != "" && prod == dsn {
		t.Fatal("PIXKB_TEST_DSN equals PIXKB_DSN (prod KB) — use a throwaway database")
	}

	root := NewRootCmd()
	root.SetArgs([]string{"db", "up", "--dsn", dsn})
	require.NoError(t, root.ExecuteContext(context.Background()))

	ctx := context.Background()
	st, err := openStore(ctx, Config{DSN: dsn})
	require.NoError(t, err)
	defer st.Close()
	emb, err := newEmbedder(Config{})
	require.NoError(t, err)

	term := fmt.Sprintf("httptest-marker-%d", time.Now().UnixNano())
	id := fmt.Sprintf("httptest/%s.md", term)
	title := "Search Handler Test Concept"
	body := "Body mentioning " + term + " for an unambiguous FTS hit."
	require.NoError(t, st.UpsertConcept(ctx, okf.Concept{
		ID: id, Type: "Reference", Title: title, Body: body,
		ContentSHA: "sha-" + term, Epoch: 1, Timestamp: time.Now(),
	}))

	// A bundle copy of the same concept, so printExplain's matched-fields
	// lookup (which reads from the bundle, not Postgres) succeeds too.
	bundleDir := t.TempDir()
	require.NoError(t, okf.WriteConcept(bundleDir, okf.Concept{
		ID: id, Type: "Reference", Title: title, Body: body, ContentSHA: "sha-" + term,
	}))

	handler := newSearchHandler(st, emb, bundleDir)

	t.Run("default format is json", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/search?q="+term, nil)
		rec := httptest.NewRecorder()
		handler(rec, req)
		require.Equal(t, 200, rec.Code)
		assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))
		assert.Contains(t, rec.Body.String(), id)
	})

	t.Run("format=md renders a markdown table", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/search?q="+term+"&format=md", nil)
		rec := httptest.NewRecorder()
		handler(rec, req)
		require.Equal(t, 200, rec.Code)
		assert.Contains(t, rec.Header().Get("Content-Type"), "text/markdown")
		assert.Contains(t, rec.Body.String(), "| rank | id | title | type | score |")
		assert.Contains(t, rec.Body.String(), id)
	})

	t.Run("explain=true always returns json with matched fields", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/search?q="+term+"&format=md&explain=true", nil)
		rec := httptest.NewRecorder()
		handler(rec, req)
		require.Equal(t, 200, rec.Code, rec.Body.String())
		assert.Equal(t, "application/json", rec.Header().Get("Content-Type"),
			"explain=true must always be JSON regardless of format=")
		body := rec.Body.String()
		assert.Contains(t, body, id)
		assert.True(t, strings.Contains(body, "matched_tokens") && strings.Contains(body, strings.Split(term, "-")[0]),
			"explain output should include the matched-tokens annotation: %s", body)
	})

	t.Run("missing q is a 400", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/search", nil)
		rec := httptest.NewRecorder()
		handler(rec, req)
		assert.Equal(t, 400, rec.Code)
	})
}

// TestTarDir_SkipsOutputFile verifies the archive never contains itself, even
// when the output path is created inside the directory being tarred.
func TestTarDir_SkipsOutputFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "a.md"), []byte("alpha"), 0o644))
	out := filepath.Join(dir, "bundle.tar.gz")

	require.NoError(t, tarDir(dir, out))

	names := readTar(t, out)
	assert.True(t, names["a.md"], "expected a.md in archive")
	assert.False(t, names["bundle.tar.gz"], "archive must not contain the output file")
	assert.Len(t, names, 1)
}

// TestTarDir_SkipsGitDir verifies entries under a .git directory are excluded.
func TestTarDir_SkipsGitDir(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, ".git"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".git", "HEAD"), []byte("ref"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "keep.md"), []byte("keep"), 0o644))
	out := filepath.Join(t.TempDir(), "bundle.tar.gz")

	require.NoError(t, tarDir(dir, out))

	names := readTar(t, out)
	assert.True(t, names["keep.md"])
	assert.False(t, names[".git/HEAD"], ".git contents must be skipped")
	assert.Len(t, names, 1)
}

func TestDoctorCmdWiring(t *testing.T) {
	t.Parallel()

	root := NewRootCmd()
	doctor, _, err := root.Find([]string{"doctor"})
	require.NoError(t, err)
	require.Equal(t, "doctor", doctor.Name())
	require.NotNil(t, doctor.RunE)

	// The --dsn flag is wired.
	require.NotNil(t, doctor.Flags().Lookup("dsn"))
}

func TestDoctorCmd_FailsWithoutDSN(t *testing.T) {
	// With no DSN configured, doctor's "dsn configured" check fails, so RunE
	// must return an error. This is fully offline (no live DB required).
	t.Setenv("PIXKB_CONFIG_DIR", t.TempDir()) // isolate from any real global config
	t.Setenv("PIXKB_DSN", "")
	t.Setenv("PIXKB_BUNDLE", filepath.Join(t.TempDir(), "kb"))

	dir := t.TempDir()
	t.Chdir(dir) // avoid picking up a stray pixkb.yaml from the repo

	root := NewRootCmd()
	root.SetArgs([]string{"doctor"})
	root.SetOut(os.NewFile(0, os.DevNull))
	err := root.ExecuteContext(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "doctor")
}

func TestOpsCmdWiring(t *testing.T) {
	t.Parallel()

	root := NewRootCmd()
	for _, name := range []string{"watch", "serve", "doctor", "export-bundle"} {
		cmd, _, err := root.Find([]string{name})
		require.NoError(t, err, "Find %q", name)
		assert.Equal(t, name, cmd.Name())
	}
}
