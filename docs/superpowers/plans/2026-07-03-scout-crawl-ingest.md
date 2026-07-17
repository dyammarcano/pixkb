# Scout Crawl Ingest Source Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add `internal/ingest/scoutcrawl.go`, a new `ingest.Source` that turns a Scout `knowledge` crawl snapshot of `bcb.gov.br` into one `okf.Concept` per page, and wire it into `pixkb`'s existing config/ingest pipeline.

**Architecture:** A single pure-function extraction (first `# ` heading through the literal `"Siga o BC"` footer marker) turns each crawled markdown page into a concept, skipping failed captures (no H1) and content-free stub pages. Wired in behind a new `pixkb.yaml` field, following the exact pattern of the existing `pdfs:`/`markdown:`/`api_docs:` fields.

**Tech Stack:** Go 1.25.0, stdlib only (`path/filepath`, `strings`, `os`), `github.com/stretchr/testify`.

**Design spec:** `docs/superpowers/specs/2026-07-03-scout-crawl-ingest-design.md`

## Global Constraints

- Module is `pixkb`; Go 1.25.0; `CGO_ENABLED=0` pure-Go only.
- All tests use testify (`require`/`assert`), co-located as `*_test.go` in the same package, matching existing files (`internal/ingest/markdown_test.go`, `cmd/pixkb/config_test.go`).
- Reuse the existing unexported helpers `slugify` and `firstLine` (`internal/ingest/pdf.go:237,256`) and `detectMarkdownLang` (`internal/ingest/markdown.go:108`) — do not redefine them.
- **Extraction rule (exact, empirically verified against a real re-crawl of bcb.gov.br — not illustrative):** a page's real content starts at the first line with prefix `"# "` (that line's text, minus the prefix, is the title) and ends at the first later line containing the literal substring `"Siga o BC"` (a fixed footer marker); if no such line exists, take content through end-of-file instead. If no line has prefix `"# "` at all, the page's capture failed — skip it, emit no concept, no error. If the extracted body (excluding the H1 line) has fewer than 40 non-whitespace characters after collapsing all whitespace, it's a content-free stub page — skip it, emit no concept, no error.
- New concept `Type` value: `"WebPage"`.
- `NewScoutCrawlSource(dir, baseURL string) Source` takes `baseURL` as a constructor parameter — never hardcode a URL inside `scoutcrawl.go` itself. The CLI wiring in `buildSources` passes the literal `"https://www.bcb.gov.br"`.
- New `pixkb.yaml` field `scout_crawl_dir` is **yaml-only, no `PIXKB_*` env override** — matches the existing `pdfs`/`markdown`/`api_docs` fields' convention (as opposed to `dsn`/`bundle_dir`/`ingest_dir`/`embedder`, which are env-overridable).
- **Git: this branch (`build/scout-crawl-ingest`, off `master` at `5ec5625`) shares its working tree with unrelated, uncommitted work-in-progress** (a modified `docs/antigravity-kb.md` and an untracked spec file under `docs/superpowers/specs/`, neither related to this feature). **Every commit in this plan must stage files by explicit path — never `git add -A` or `git add .`** — so that WIP is never touched or swept into a commit.

---

### Task 1: `internal/ingest/scoutcrawl.go` — the extraction source

**Files:**
- Create: `internal/ingest/scoutcrawl.go`
- Test: `internal/ingest/scoutcrawl_test.go`

**Interfaces:**
- Consumes: `ingest.Source` interface (`internal/ingest/source.go:14`: `Fetch(ctx context.Context) ([]okf.Concept, error)`, `Name() string`); `okf.Concept` (`internal/okf/concept.go`); package-local helpers `slugify(s string) string`, `firstLine(s string) string` (`pdf.go`), `detectMarkdownLang(body string) string` (`markdown.go`).
- Produces: `ingest.NewScoutCrawlSource(dir, baseURL string) Source` — consumed by Task 2's `buildSources`.

- [ ] **Step 1: Write the failing tests**

```go
// internal/ingest/scoutcrawl_test.go
package ingest

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeCrawlPage(t *testing.T, dir, relPath, content string) {
	t.Helper()
	full := filepath.Join(dir, filepath.FromSlash(relPath))
	require.NoError(t, os.MkdirAll(filepath.Dir(full), 0o755))
	require.NoError(t, os.WriteFile(full, []byte(content), 0o644))
}

const scoutCrawlNavPreamble = `Banco Central do Brasil
![](https://www.bcb.gov.br/assets/img/logo_bacen_preto.png)

- [Ir para o conteúdo 1](https://www.bcb.gov.br/#inicioConteudo)

1. [Home](https://www.bcb.gov.br/)
2. Some Page
`

const scoutCrawlFooterChrome = `
Siga o BC
[Instagram](http://www.instagram.com/bancocentraldobrasil/)

© Banco Central do Brasil - [Todos os direitos reservados](https://www.bcb.gov.br/acessoinformacao/direitosautorais)
`

func TestScoutCrawlSource_NormalPage(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	page := scoutCrawlNavPreamble + `
# Pagamento em moeda local

Sistemas de pagamentos internacionais são infraestruturas que possibilitam a transferência de recursos financeiros entre usuários de diferentes países.

### Sistema de Pagamentos em Moeda Local

O Sistema de Pagamentos em Moeda Local (SML) é administrado pelo Banco Central do Brasil em parceria com os bancos centrais da Argentina, Uruguai e Paraguai.
` + scoutCrawlFooterChrome
	writeCrawlPage(t, dir, "estabilidadefinanceira/sml.md", page)

	got, err := NewScoutCrawlSource(dir, "https://www.bcb.gov.br").Fetch(context.Background())
	require.NoError(t, err)
	require.Len(t, got, 1)

	c := got[0]
	assert.Equal(t, "WebPage", c.Type)
	assert.Equal(t, "Pagamento em moeda local", c.Title)
	assert.Equal(t, "web/estabilidadefinanceira-sml.md", c.ID)
	assert.Equal(t, "https://www.bcb.gov.br/estabilidadefinanceira/sml", c.SourceURI)
	assert.Contains(t, c.Body, "Sistema de Pagamentos em Moeda Local")
	assert.Contains(t, c.Tags, "estabilidadefinanceira")
	assert.NotContains(t, c.Body, "Siga o BC")
	assert.NotEmpty(t, c.ContentSHA)
}

func TestScoutCrawlSource_RootIndexUsesBareBaseURL(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	page := scoutCrawlNavPreamble + `
# Banco Central do Brasil

Garantir a estabilidade de preços, zelar por um sistema financeiro sólido e eficiente, e fomentar o bem-estar econômico da sociedade e do país inteiro.
` + scoutCrawlFooterChrome
	writeCrawlPage(t, dir, "index.md", page)

	got, err := NewScoutCrawlSource(dir, "https://www.bcb.gov.br").Fetch(context.Background())
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "https://www.bcb.gov.br/", got[0].SourceURI)
}

func TestScoutCrawlSource_SkipsPageWithNoH1(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeCrawlPage(t, dir, "en.md", scoutCrawlNavPreamble+"\nNo heading anywhere in this failed capture.\n")

	got, err := NewScoutCrawlSource(dir, "https://www.bcb.gov.br").Fetch(context.Background())
	require.NoError(t, err)
	assert.Empty(t, got)
}

func TestScoutCrawlSource_SkipsStubPageWithNoRealContent(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	page := scoutCrawlNavPreamble + `
# Atas das reuniões do CMN
` + scoutCrawlFooterChrome
	writeCrawlPage(t, dir, "acessoinformacao/cmnatasreun.md", page)

	got, err := NewScoutCrawlSource(dir, "https://www.bcb.gov.br").Fetch(context.Background())
	require.NoError(t, err)
	assert.Empty(t, got)
}

func TestScoutCrawlSource_NoFooterMarkerFallsBackToEOF(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	page := scoutCrawlNavPreamble + `
# Octavio Gouvêa de Bulhões

Octavio Gouvêa de Bulhões nasceu no Rio de Janeiro e formou-se na Faculdade de Direito do Rio de Janeiro, mesma escola onde obteve seu grau de doutor em ciências jurídicas e sociais.
`
	writeCrawlPage(t, dir, "historiacontada/index.html.md", page)

	got, err := NewScoutCrawlSource(dir, "https://www.bcb.gov.br").Fetch(context.Background())
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Contains(t, got[0].Body, "Faculdade de Direito")
	assert.Equal(t, "https://www.bcb.gov.br/historiacontada/index.html", got[0].SourceURI)
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/ingest/... -run TestScoutCrawlSource -v`
Expected: FAIL — `undefined: NewScoutCrawlSource`

- [ ] **Step 3: Write the implementation**

```go
// internal/ingest/scoutcrawl.go
package ingest

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"pixkb/internal/okf"
)

const scoutCrawlMinBodyChars = 40

type scoutCrawlSource struct {
	dir     string
	baseURL string
}

// NewScoutCrawlSource builds a Source that ingests a Scout `knowledge` crawl
// snapshot's markdown pages (dir should point at the crawl's pages/
// directory). Each page's real content is bracketed by its first H1 heading
// and the literal footer marker "Siga o BC"; pages with no H1 (failed
// capture) or too little content between H1 and footer (list-only stub
// pages) are skipped.
func NewScoutCrawlSource(dir, baseURL string) Source {
	return &scoutCrawlSource{dir: dir, baseURL: strings.TrimSuffix(baseURL, "/")}
}

func (s *scoutCrawlSource) Name() string { return "scout-crawl" }

func (s *scoutCrawlSource) Fetch(_ context.Context) ([]okf.Concept, error) {
	var out []okf.Concept
	err := filepath.WalkDir(s.dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".md") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("scout crawl %s: %w", path, err)
		}
		c, ok, err := s.extract(path, string(data))
		if err != nil {
			return err
		}
		if ok {
			out = append(out, c)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (s *scoutCrawlSource) extract(path, data string) (okf.Concept, bool, error) {
	lines := strings.Split(data, "\n")

	h1Idx := -1
	for i, l := range lines {
		if strings.HasPrefix(l, "# ") {
			h1Idx = i
			break
		}
	}
	if h1Idx < 0 {
		return okf.Concept{}, false, nil // failed capture: no H1 at all
	}
	title := strings.TrimSpace(lines[h1Idx][2:])

	footerIdx := len(lines)
	for i := h1Idx + 1; i < len(lines); i++ {
		if strings.Contains(lines[i], "Siga o BC") {
			footerIdx = i
			break
		}
	}

	body := strings.TrimSpace(strings.Join(lines[h1Idx+1:footerIdx], "\n"))
	meaningful := len(strings.Join(strings.Fields(body), ""))
	if meaningful < scoutCrawlMinBodyChars {
		return okf.Concept{}, false, nil // list-only stub page, no real content
	}

	relPath, err := filepath.Rel(s.dir, path)
	if err != nil {
		return okf.Concept{}, false, fmt.Errorf("scout crawl %s: %w", path, err)
	}
	relPath = filepath.ToSlash(strings.TrimSuffix(relPath, ".md"))

	sourceURL := s.baseURL + "/" + relPath
	if relPath == "index" {
		sourceURL = s.baseURL + "/"
	}

	slug := slugify(relPath)
	fullBody := "# " + title + "\n\n" + body

	tags := []string{"web", "bcb"}
	if i := strings.Index(relPath, "/"); i >= 0 {
		tags = append(tags, relPath[:i])
	}

	return okf.Concept{
		ID:          "web/" + slug + ".md",
		Type:        "WebPage",
		Title:       title,
		Description: firstLine(body),
		Resource:    path,
		Tags:        tags,
		Language:    detectMarkdownLang(body),
		SourceURI:   sourceURL,
		Body:        fullBody,
		ContentSHA:  okf.ComputeSHA(fullBody),
	}, true, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/ingest/... -v`
Expected: PASS (all `TestScoutCrawlSource_*` tests, plus every pre-existing test in the package — confirms no collision with `slugify`/`firstLine`/`detectMarkdownLang`)

- [ ] **Step 5: Commit (explicit paths only — see Global Constraints)**

```bash
git add internal/ingest/scoutcrawl.go internal/ingest/scoutcrawl_test.go
git commit -m "feat: add scout-crawl ingest source for BCB website pages"
```

---

### Task 2: Wire into config + `buildSources`

**Files:**
- Modify: `cmd/pixkb/config.go` (the `Config` struct at line 19 and `loadConfig()` at line 44)
- Modify: `cmd/pixkb/commands.go` (`buildSources` at line 23)
- Test: `cmd/pixkb/config_test.go` (append), new file `cmd/pixkb/commands_test.go`

**Interfaces:**
- Consumes: `ingest.NewScoutCrawlSource(dir, baseURL string) Source` (Task 1).
- Produces: `Config.ScoutCrawlDir string` — no other task depends on this; it's the terminal wiring point.

- [ ] **Step 1: Write the failing tests**

```go
// cmd/pixkb/config_test.go — append these two functions to the existing file
func TestLoadConfig_ScoutCrawlDir(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	yaml := "scout_crawl_dir: mirrors/bcb/knowledge/pages\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "pixkb.yaml"), []byte(yaml), 0o644))

	cfg := loadConfig()
	assert.Equal(t, "mirrors/bcb/knowledge/pages", cfg.ScoutCrawlDir)
}

func TestLoadConfig_ScoutCrawlDir_DefaultsEmpty(t *testing.T) {
	t.Chdir(t.TempDir())
	cfg := loadConfig()
	assert.Empty(t, cfg.ScoutCrawlDir)
}
```

```go
// cmd/pixkb/commands_test.go — new file
package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestBuildSources_IncludesScoutCrawlWhenConfigured(t *testing.T) {
	t.Parallel()
	cfg := Config{ScoutCrawlDir: "mirrors/bcb/knowledge/pages"}
	srcs := buildSources(cfg)

	names := map[string]bool{}
	for _, s := range srcs {
		names[s.Name()] = true
	}
	assert.True(t, names["scout-crawl"], "expected scout-crawl source when ScoutCrawlDir is set")
}

func TestBuildSources_OmitsScoutCrawlWhenNotConfigured(t *testing.T) {
	t.Parallel()
	cfg := Config{}
	srcs := buildSources(cfg)

	for _, s := range srcs {
		assert.NotEqual(t, "scout-crawl", s.Name())
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./cmd/pixkb/... -run 'TestLoadConfig_ScoutCrawlDir|TestBuildSources' -v`
Expected: FAIL — `Config` has no field `ScoutCrawlDir` (compile error)

- [ ] **Step 3: Write the implementation**

Modify `cmd/pixkb/config.go` — add the field to the `Config` struct:

```go
// cmd/pixkb/config.go — before:
type Config struct {
	DSN       string     `yaml:"dsn"`
	BundleDir string     `yaml:"bundle_dir"`
	IngestDir string     `yaml:"ingest_dir"`
	Embedder  string     `yaml:"embedder"`
	PDFs      []string   `yaml:"pdfs"`       // PDF files to ingest as ManualSection concepts
	Markdown  []string   `yaml:"markdown"`   // curated Markdown reference docs (H2 → Reference concepts)
	MirrorDir string     `yaml:"mirror_dir"` // dir holding pre-staged repo mirrors
	Repos     []RepoConf `yaml:"repos"`      // git repos (mirror under MirrorDir/<name>)
	APIDocs   []string   `yaml:"api_docs"`   // local API-DICT HTML files
}
```

```go
// cmd/pixkb/config.go — after:
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
```

And in `loadConfig()`, inside the `if yaml.Unmarshal(data, &fromFile) == nil { ... }` block, alongside the other file-only fields:

```go
// cmd/pixkb/config.go — before (inside loadConfig's yaml.Unmarshal block):
			cfg.PDFs = fromFile.PDFs
			cfg.Markdown = fromFile.Markdown
			cfg.Repos = fromFile.Repos
			cfg.APIDocs = fromFile.APIDocs
```

```go
// cmd/pixkb/config.go — after:
			cfg.PDFs = fromFile.PDFs
			cfg.Markdown = fromFile.Markdown
			cfg.Repos = fromFile.Repos
			cfg.APIDocs = fromFile.APIDocs
			cfg.ScoutCrawlDir = fromFile.ScoutCrawlDir
```

Modify `cmd/pixkb/commands.go` — wire into `buildSources`:

```go
// cmd/pixkb/commands.go — before:
	if len(cfg.APIDocs) > 0 {
		srcs = append(srcs, ingest.NewAPIDocSource(cfg.APIDocs))
	}
	return srcs
}
```

```go
// cmd/pixkb/commands.go — after:
	if len(cfg.APIDocs) > 0 {
		srcs = append(srcs, ingest.NewAPIDocSource(cfg.APIDocs))
	}
	if cfg.ScoutCrawlDir != "" {
		srcs = append(srcs, ingest.NewScoutCrawlSource(cfg.ScoutCrawlDir, "https://www.bcb.gov.br"))
	}
	return srcs
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./cmd/pixkb/... -v`
Expected: PASS (all tests in the package, including pre-existing ones — confirms the wiring didn't break anything)

- [ ] **Step 5: Build the whole module and run the full test suite**

Run: `go build ./...`
Expected: exits 0, no errors.

Run: `go vet ./...`
Expected: clean, no issues.

Run: `go test ./... -short`
Expected: PASS (Postgres integration tests SKIP under `-short` by design; everything else runs and passes).

- [ ] **Step 6: Commit (explicit paths only — see Global Constraints)**

```bash
git add cmd/pixkb/config.go cmd/pixkb/commands.go cmd/pixkb/config_test.go cmd/pixkb/commands_test.go
git commit -m "feat: wire scout-crawl source into pixkb.yaml and buildSources"
```

---

## Manual smoke test (optional, requires network + a real Postgres)

```bash
go run ./cmd/pixkb db up
# pixkb.yaml: add `scout_crawl_dir: mirrors/bcb/knowledge/pages`
go run ./cmd/pixkb ingest
go run ./cmd/pixkb search "pagamento em moeda local"
```
