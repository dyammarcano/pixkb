package main

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
