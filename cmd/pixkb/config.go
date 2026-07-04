package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"gopkg.in/yaml.v3"

	"pixkb/internal/embed"
	"pixkb/internal/epoch"
	"pixkb/internal/store/postgres"
	"pixkb/pkg/agents"
)

// Config holds pixkb runtime settings. Resolution order: built-in defaults <
// global user config (globalConfigPath()) < pixkb.yaml (project-local) <
// environment variables (PIXKB_*). A --dsn flag overrides DSN on commands
// that expose one.
type Config struct {
	DSN           string     `yaml:"dsn"`
	BundleDir     string     `yaml:"bundle_dir"`
	IngestDir     string     `yaml:"ingest_dir"`
	Embedder      string     `yaml:"embedder"`
	PDFs          []string   `yaml:"pdfs"`            // PDF files to ingest as ManualSection concepts
	Markdown      []string   `yaml:"markdown"`        // curated Markdown reference docs (H2 → Reference concepts)
	MirrorDir     string     `yaml:"mirror_dir"`      // dir holding pre-staged repo mirrors
	Repos         []RepoConf `yaml:"repos"`           // git repos (mirror under MirrorDir/<name>)
	APIDocs       []string   `yaml:"api_docs"`        // local API-DICT HTML files
	ScoutCrawlDir string     `yaml:"scout_crawl_dir"` // dir holding a Scout knowledge-crawl's pages/ tree (WebPage concepts)
}

// RepoConf names a repository whose staged mirror is ingested.
type RepoConf struct {
	Owner string `yaml:"owner"`
	Name  string `yaml:"name"`
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// globalConfigPath returns the per-user config file path: PIXKB_CONFIG_DIR's
// config.yaml if that env var is set (a power-user override, and how tests
// isolate themselves from whatever global config exists on the machine
// running them), else <os.UserConfigDir()>/PixKB/config.yaml — which resolves
// to %LocalAppData%\PixKB\config.yaml on Windows, ~/.config/PixKB/config.yaml
// on Linux, and ~/Library/Application Support/PixKB/config.yaml on macOS.
// Returns "" if neither resolves (os.UserConfigDir can fail on some systems).
func globalConfigPath() string {
	if dir := os.Getenv("PIXKB_CONFIG_DIR"); dir != "" {
		return filepath.Join(dir, "config.yaml")
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "PixKB", "config.yaml")
}

// applyConfigFile reads the YAML file at path, if it exists, and merges its
// non-empty/non-nil fields into cfg. A field left unset in the file never
// clobbers a value cfg already holds, which lets applyConfigFile be called
// repeatedly with increasing precedence (global config, then project-local
// pixkb.yaml) — a later call's absent fields don't erase an earlier call's
// values.
func applyConfigFile(cfg *Config, path string) {
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	var fromFile Config
	if yaml.Unmarshal(data, &fromFile) != nil {
		return
	}
	if fromFile.DSN != "" {
		cfg.DSN = fromFile.DSN
	}
	if fromFile.BundleDir != "" {
		cfg.BundleDir = fromFile.BundleDir
	}
	if fromFile.IngestDir != "" {
		cfg.IngestDir = fromFile.IngestDir
	}
	if fromFile.Embedder != "" {
		cfg.Embedder = fromFile.Embedder
	}
	if fromFile.MirrorDir != "" {
		cfg.MirrorDir = fromFile.MirrorDir
	}
	if len(fromFile.PDFs) > 0 {
		cfg.PDFs = fromFile.PDFs
	}
	if len(fromFile.Markdown) > 0 {
		cfg.Markdown = fromFile.Markdown
	}
	if len(fromFile.Repos) > 0 {
		cfg.Repos = fromFile.Repos
	}
	if len(fromFile.APIDocs) > 0 {
		cfg.APIDocs = fromFile.APIDocs
	}
	if fromFile.ScoutCrawlDir != "" {
		cfg.ScoutCrawlDir = fromFile.ScoutCrawlDir
	}
}

func loadConfig() Config {
	cfg := Config{BundleDir: "kb", IngestDir: "ingest", Embedder: "hashing", MirrorDir: "mirrors"}
	if path := globalConfigPath(); path != "" {
		applyConfigFile(&cfg, path)
	}
	applyConfigFile(&cfg, "pixkb.yaml")
	// Environment overrides file + defaults.
	cfg.DSN = envOr("PIXKB_DSN", cfg.DSN)
	cfg.BundleDir = envOr("PIXKB_BUNDLE", cfg.BundleDir)
	cfg.IngestDir = envOr("PIXKB_INGEST", cfg.IngestDir)
	cfg.Embedder = envOr("PIXKB_EMBEDDER", cfg.Embedder)
	return cfg
}

func newEmbedder(cfg Config) (embed.Embedder, error) {
	switch cfg.Embedder {
	case "", "hashing":
		return embed.NewHashing(256), nil
	case "openai":
		// Optional high-recall embeddings via an OpenAI-compatible API. NOT the
		// default: the project drives quality through the agy agent fleet over
		// pixdb (read/curate/write-back), not a metered embedding API. Kept as
		// an opt-in for deployments that want it (point OPENAI_BASE_URL at a
		// local server to stay offline).
		return agents.NewOpenAIEmbedder(envOr("PIXKB_EMBED_MODEL", ""), embedDims(cfg))
	default:
		return embed.NewHashing(256), nil
	}
}

// embedDims resolves the embedding dimensionality (PIXKB_EMBED_DIMS env, else 0
// for the embedder's default).
func embedDims(_ Config) int {
	if v := os.Getenv("PIXKB_EMBED_DIMS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return 0
}

func openStore(ctx context.Context, cfg Config) (*postgres.Store, error) {
	if cfg.DSN == "" {
		return nil, fmt.Errorf("no database DSN: set PIXKB_DSN, pixkb.yaml dsn, or --dsn")
	}
	return postgres.Open(ctx, cfg.DSN)
}

// newRunner opens the store and builds an epoch.Runner. The caller must Close
// the returned store.
func newRunner(ctx context.Context, cfg Config) (*epoch.Runner, *postgres.Store, error) {
	st, err := openStore(ctx, cfg)
	if err != nil {
		return nil, nil, err
	}
	emb, err := newEmbedder(cfg)
	if err != nil {
		st.Close()
		return nil, nil, err
	}
	r := &epoch.Runner{Bundle: cfg.BundleDir, Store: st, Emb: emb, Git: epoch.NewGitCommitter(cfg.BundleDir)}
	return r, st, nil
}
