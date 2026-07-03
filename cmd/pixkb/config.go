package main

import (
	"context"
	"fmt"
	"os"
	"strconv"

	"gopkg.in/yaml.v3"

	"pixkb/internal/embed"
	"pixkb/internal/epoch"
	"pixkb/internal/store/postgres"
	"pixkb/pkg/agents"
)

// Config holds pixkb runtime settings. Resolution order: built-in defaults <
// pixkb.yaml < environment variables (PIXKB_*). A --dsn flag overrides DSN.
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

func loadConfig() Config {
	cfg := Config{BundleDir: "kb", IngestDir: "ingest", Embedder: "hashing", MirrorDir: "mirrors"}
	if data, err := os.ReadFile("pixkb.yaml"); err == nil {
		var fromFile Config
		if yaml.Unmarshal(data, &fromFile) == nil {
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
			cfg.PDFs = fromFile.PDFs
			cfg.Markdown = fromFile.Markdown
			cfg.Repos = fromFile.Repos
			cfg.APIDocs = fromFile.APIDocs
			cfg.ScoutCrawlDir = fromFile.ScoutCrawlDir
		}
	}
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
