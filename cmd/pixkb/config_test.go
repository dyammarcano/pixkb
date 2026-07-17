package main

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestLoadConfig_Defaults confirms the built-in defaults apply when no
// pixkb.yaml and no PIXKB_* env vars are present.
func TestLoadConfig_Defaults(t *testing.T) {
	t.Chdir(t.TempDir())                      // empty dir: no pixkb.yaml
	t.Setenv("PIXKB_CONFIG_DIR", t.TempDir()) // isolate from any real global config
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
	t.Setenv("PIXKB_CONFIG_DIR", t.TempDir()) // isolate from any real global config
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
	t.Setenv("PIXKB_CONFIG_DIR", t.TempDir()) // isolate from any real global config
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

func TestLoadConfig_ScoutCrawlDir(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	t.Setenv("PIXKB_CONFIG_DIR", t.TempDir()) // isolate from any real global config
	yaml := "scout_crawl_dir: mirrors/bcb/knowledge/pages\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "pixkb.yaml"), []byte(yaml), 0o644))

	cfg := loadConfig()
	assert.Equal(t, "mirrors/bcb/knowledge/pages", cfg.ScoutCrawlDir)
}

func TestLoadConfig_ScoutCrawlDir_DefaultsEmpty(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("PIXKB_CONFIG_DIR", t.TempDir()) // isolate from any real global config
	cfg := loadConfig()
	assert.Empty(t, cfg.ScoutCrawlDir)
}

func TestLoadConfig_ScoutCrawlBaseURL(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	t.Setenv("PIXKB_CONFIG_DIR", t.TempDir()) // isolate from any real global config
	yaml := "scout_crawl_base_url: https://www.gov.br\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "pixkb.yaml"), []byte(yaml), 0o644))

	cfg := loadConfig()
	assert.Equal(t, "https://www.gov.br", cfg.ScoutCrawlBaseURL)
}

func TestLoadConfig_ScoutCrawlBaseURL_DefaultsEmpty(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("PIXKB_CONFIG_DIR", t.TempDir()) // isolate from any real global config
	cfg := loadConfig()
	assert.Empty(t, cfg.ScoutCrawlBaseURL) // buildSources falls back to defaultScoutCrawlBaseURL
}

// TestGlobalConfigPath_UsesConfigDirOverride confirms PIXKB_CONFIG_DIR is a
// full override of the OS-specific lookup, not a suffix added to it.
func TestGlobalConfigPath_UsesConfigDirOverride(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PIXKB_CONFIG_DIR", dir)
	assert.Equal(t, filepath.Join(dir, "config.yaml"), globalConfigPath())
}

// TestUserConfigDir_WindowsUsesLocalAppDataNotRoaming confirms the Windows
// path uses %LocalAppData% (a per-machine dir), not os.UserConfigDir()'s own
// answer on Windows (%AppData%, which roams with the user profile) — a real
// bug caught during manual verification: os.UserConfigDir() resolves to
// AppData\Roaming on Windows, not AppData\Local.
func TestUserConfigDir_WindowsUsesLocalAppDataNotRoaming(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows-specific: verifies %LocalAppData% is used, not os.UserConfigDir()'s %AppData%")
	}
	want := `C:\fake\local\appdata`
	t.Setenv("LocalAppData", want)
	assert.Equal(t, want, userConfigDir())
}

// TestLoadConfig_GlobalConfigAppliesWhenNoLocalFile confirms the global config
// (PIXKB_CONFIG_DIR/config.yaml) is picked up even with no project-local
// pixkb.yaml present.
func TestLoadConfig_GlobalConfigAppliesWhenNoLocalFile(t *testing.T) {
	globalDir := t.TempDir()
	t.Setenv("PIXKB_CONFIG_DIR", globalDir)
	require.NoError(t, os.WriteFile(filepath.Join(globalDir, "config.yaml"),
		[]byte("dsn: postgres://fromglobal@localhost/db\n"), 0o644))
	t.Chdir(t.TempDir()) // empty dir: no local pixkb.yaml
	t.Setenv("PIXKB_DSN", "")

	cfg := loadConfig()
	assert.Equal(t, "postgres://fromglobal@localhost/db", cfg.DSN)
}

// TestLoadConfig_LocalOverridesGlobal confirms project-local pixkb.yaml wins
// over the global config for fields both set, while fields only the global
// config sets survive (a local file's absence of a field must not erase it).
func TestLoadConfig_LocalOverridesGlobal(t *testing.T) {
	globalDir := t.TempDir()
	t.Setenv("PIXKB_CONFIG_DIR", globalDir)
	require.NoError(t, os.WriteFile(filepath.Join(globalDir, "config.yaml"),
		[]byte("dsn: postgres://fromglobal@localhost/db\nbundle_dir: global-kb\n"), 0o644))

	localDir := t.TempDir()
	t.Chdir(localDir)
	require.NoError(t, os.WriteFile(filepath.Join(localDir, "pixkb.yaml"),
		[]byte("dsn: postgres://fromlocal@localhost/db\n"), 0o644))
	t.Setenv("PIXKB_DSN", "")

	cfg := loadConfig()
	assert.Equal(t, "postgres://fromlocal@localhost/db", cfg.DSN, "local dsn overrides global")
	assert.Equal(t, "global-kb", cfg.BundleDir, "global bundle_dir survives since local doesn't set it")
}

func TestLoadConfig_OpenAPISpecs(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	t.Setenv("PIXKB_CONFIG_DIR", t.TempDir())
	yaml := "openapi_specs:\n" +
		"  - { file: mirror/openapi/tributos-consumo.json, domain: tax }\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "pixkb.yaml"), []byte(yaml), 0o644))

	cfg := loadConfig()
	require.Len(t, cfg.OpenAPISpecs, 1)
	assert.Equal(t, "mirror/openapi/tributos-consumo.json", cfg.OpenAPISpecs[0].File)
	assert.Equal(t, "tax", cfg.OpenAPISpecs[0].Domain)
}

func TestApplyConfigFileLegislation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "pixkb.yaml")
	require.NoError(t, os.WriteFile(path, []byte(
		"legislation:\n  - { file: mirror/legislation/LC214-2025.pdf, lei: lc-214-2025, domain: tax }\n"), 0o644))

	var cfg Config
	applyConfigFile(&cfg, path)

	require.Len(t, cfg.Legislation, 1)
	require.Equal(t, "mirror/legislation/LC214-2025.pdf", cfg.Legislation[0].File)
	require.Equal(t, "lc-214-2025", cfg.Legislation[0].Lei)
	require.Equal(t, "tax", cfg.Legislation[0].Domain)
}
