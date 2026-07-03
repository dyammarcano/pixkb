package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestLoadConfig_Defaults confirms the built-in defaults apply when no
// pixkb.yaml and no PIXKB_* env vars are present.
func TestLoadConfig_Defaults(t *testing.T) {
	t.Chdir(t.TempDir()) // empty dir: no pixkb.yaml
	t.Setenv("PIXKB_DSN", "")
	t.Setenv("PIXKB_BUNDLE", "")
	t.Setenv("PIXKB_INGEST", "")
	t.Setenv("PIXKB_EMBEDDER", "")

	cfg := loadConfig()
	assert.Equal(t, "kb", cfg.BundleDir)
	assert.Equal(t, "ingest", cfg.IngestDir)
	assert.Equal(t, "hashing", cfg.Embedder)
	assert.Equal(t, "mirrors", cfg.MirrorDir)
	assert.Empty(t, cfg.DSN)
}

// TestLoadConfig_EnvOverrides confirms PIXKB_* env vars override defaults.
func TestLoadConfig_EnvOverrides(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("PIXKB_DSN", "postgres://envuser@localhost/db")
	t.Setenv("PIXKB_BUNDLE", "envbundle")
	t.Setenv("PIXKB_INGEST", "envingest")
	t.Setenv("PIXKB_EMBEDDER", "openai")

	cfg := loadConfig()
	assert.Equal(t, "postgres://envuser@localhost/db", cfg.DSN)
	assert.Equal(t, "envbundle", cfg.BundleDir)
	assert.Equal(t, "envingest", cfg.IngestDir)
	assert.Equal(t, "openai", cfg.Embedder)
}

// TestLoadConfig_FileThenEnv confirms resolution order: env overrides the file,
// and file values override built-in defaults.
func TestLoadConfig_FileThenEnv(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	yaml := "dsn: postgres://fileuser@localhost/filedb\n" +
		"bundle_dir: filebundle\n" +
		"ingest_dir: fileingest\n" +
		"embedder: hashing\n" +
		"mirror_dir: filemirrors\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "pixkb.yaml"), []byte(yaml), 0o644))

	// Env unset for most keys -> file wins; one env override -> env wins.
	t.Setenv("PIXKB_DSN", "")
	t.Setenv("PIXKB_BUNDLE", "")
	t.Setenv("PIXKB_INGEST", "")
	t.Setenv("PIXKB_EMBEDDER", "")
	t.Setenv("PIXKB_BUNDLE", "envwins")

	cfg := loadConfig()
	assert.Equal(t, "postgres://fileuser@localhost/filedb", cfg.DSN, "file dsn applies")
	assert.Equal(t, "fileingest", cfg.IngestDir, "file ingest_dir applies")
	assert.Equal(t, "filemirrors", cfg.MirrorDir, "file mirror_dir applies")
	assert.Equal(t, "envwins", cfg.BundleDir, "env overrides file bundle_dir")
}

// TestNewEmbedder_HashingSelection covers the deterministic, offline embedder
// choices: empty/hashing/unknown all resolve to the hashing default.
func TestNewEmbedder_HashingSelection(t *testing.T) {
	tests := []struct {
		name     string
		embedder string
	}{
		{name: "empty selects hashing", embedder: ""},
		{name: "explicit hashing", embedder: "hashing"},
		{name: "unknown falls back to hashing", embedder: "bogus"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			emb, err := newEmbedder(Config{Embedder: tt.embedder})
			require.NoError(t, err)
			require.NotNil(t, emb)
			assert.Equal(t, "hashing", emb.Name())
		})
	}
}
