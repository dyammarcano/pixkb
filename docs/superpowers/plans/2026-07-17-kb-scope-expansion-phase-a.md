# KB Scope Expansion — Phase A Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Turn pixkb into a unified, domain-tagged KB and ingest the Receita Federal consumption-tax OpenAPI contract (CBS/IBS/IS) alongside the existing Pix/SPB corpus, so the two domains are retrievable together and separable by domain.

**Architecture:** Represent domain as a namespaced tag (`domain:pix` / `domain:tax`) on every concept — no schema migration. A single post-gather `tagDomain` pass backfills `domain:pix` on any concept a source did not tag. The tax contract is ingested through the existing air-gap `NewOpenAPISource`, extended with a domain, and fed by a new `openapi_specs` config key for standalone specs.

**Tech Stack:** Go 1.26, `gopkg.in/yaml.v3` (already used by the OpenAPI source; parses JSON too), Postgres + pgvector (local throwaway for validation via `task testdb:up`).

## Global Constraints

- Go module `pixkb`; run with `go run ./cmd/pixkb ...`, never build-then-run.
- LF line endings only. Stage files by explicit path — never `git add -A`/`.`/`<dir>`. After each commit, confirm the stat is proportionate (no CRLF phantom churn).
- Conventional commits; no AI attribution / `Co-Authored-By`.
- Air-gap: no network at ingest time. All specs are read from local files.
- `go build ./...`, `go vet ./...`, and `go test -short ./...` must be green at each task's end. Postgres integration is validated against the local throwaway DB (`PIXKB_TEST_DSN`, `:5433`), never prod.
- Domain vocabulary is exactly two values: `pix`, `tax`. Tag form is `domain:<value>`. Default (unset) domain is `pix`.

---

### Task 1: `tagDomain` post-gather pass

**Files:**
- Modify: `internal/ingest/gather.go`
- Test: `internal/ingest/gather_test.go`

**Interfaces:**
- Produces: `tagDomain(concepts []okf.Concept) []okf.Concept` — ensures every concept carries exactly one `domain:*` tag, defaulting to `domain:pix`; idempotent. Wired into `GatherAll` so all gathered concepts are domain-tagged.

- [ ] **Step 1: Write the failing test**

Add to `internal/ingest/gather_test.go`:

```go
func TestTagDomain(t *testing.T) {
	in := []okf.Concept{
		{ID: "a", Tags: []string{"manual", "ii-manual"}},          // no domain -> default pix
		{ID: "b", Tags: []string{"api", "tributos", "domain:tax"}}, // already tagged -> kept
		{ID: "c", Tags: nil},                                       // nil tags -> pix
	}
	out := tagDomain(in)

	assert.Contains(t, out[0].Tags, "domain:pix")
	assert.Contains(t, out[1].Tags, "domain:tax")
	assert.NotContains(t, out[1].Tags, "domain:pix", "must not double-tag an already-domained concept")
	assert.Contains(t, out[2].Tags, "domain:pix")

	// Idempotent: a second pass adds nothing.
	again := tagDomain(out)
	for i := range again {
		n := 0
		for _, tg := range again[i].Tags {
			if strings.HasPrefix(tg, "domain:") {
				n++
			}
		}
		assert.Equalf(t, 1, n, "concept %s must have exactly one domain tag", again[i].ID)
	}
}
```

Ensure the test file imports `"strings"` and `"github.com/stretchr/testify/assert"` (add to the existing import block if missing).

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/ingest/ -run TestTagDomain`
Expected: FAIL — `undefined: tagDomain`.

- [ ] **Step 3: Write minimal implementation**

In `internal/ingest/gather.go`, add `"strings"` to the import block, then add:

```go
// domainTagPrefix marks a concept's KB domain (domain:pix, domain:tax).
const domainTagPrefix = "domain:"

// defaultDomain is applied to any concept no source tagged with a domain.
const defaultDomain = "pix"

// tagDomain ensures every concept carries exactly one domain:* tag. A concept a
// source already tagged (e.g. domain:tax) keeps it; all others get domain:pix.
// Idempotent — a concept that already has a domain tag is left untouched.
func tagDomain(concepts []okf.Concept) []okf.Concept {
	for i := range concepts {
		hasDomain := false
		for _, t := range concepts[i].Tags {
			if strings.HasPrefix(t, domainTagPrefix) {
				hasDomain = true
				break
			}
		}
		if !hasDomain {
			concepts[i].Tags = append(concepts[i].Tags, domainTagPrefix+defaultDomain)
		}
	}
	return concepts
}
```

Wire it into `GatherAll` by changing its final `return`:

```go
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return tagDomain(out), nil
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/ingest/ -run 'TagDomain|GatherAll'`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/ingest/gather.go internal/ingest/gather_test.go
git commit -m "feat(ingest): tag every gathered concept with a domain (default domain:pix)"
```

---

### Task 2: OpenAPI source domain tagging

**Files:**
- Modify: `internal/ingest/openapi.go`
- Test: `internal/ingest/openapi_test.go`

**Interfaces:**
- Consumes: nothing from Task 1 (independent).
- Produces: `NewOpenAPISourceWithDomain(files []string, domain string) Source` — same as `NewOpenAPISource` but appends a `domain:<domain>` tag to every emitted endpoint concept when `domain != ""`. `NewOpenAPISource(files)` keeps its current behavior (no domain tag; Task 1's pass will default it to `domain:pix`).

- [ ] **Step 1: Write the failing test**

Add to `internal/ingest/openapi_test.go` (reuse the file's existing spec-fixture style; if it writes a temp spec file, mirror that — otherwise use this minimal inline JSON spec, which the YAML decoder parses):

```go
func TestOpenAPISource_WithDomainTagsEndpoints(t *testing.T) {
	dir := t.TempDir()
	spec := `{"openapi":"3.0.0","info":{"title":"Tributos","version":"1"},` +
		`"paths":{"/calcular":{"post":{"summary":"Calcula CBS/IBS"}}}}`
	path := filepath.Join(dir, "tributos-consumo.json")
	require.NoError(t, os.WriteFile(path, []byte(spec), 0o644))

	cs, err := NewOpenAPISourceWithDomain([]string{path}, "tax").Fetch(context.Background())
	require.NoError(t, err)
	require.NotEmpty(t, cs)
	assert.Equal(t, "ApiEndpoint", cs[0].Type)
	assert.Contains(t, cs[0].Tags, "domain:tax")
	assert.Contains(t, cs[0].Tags, "api")
}
```

Ensure the test imports `context`, `os`, `path/filepath`, and testify `assert`/`require`.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/ingest/ -run TestOpenAPISource_WithDomain`
Expected: FAIL — `undefined: NewOpenAPISourceWithDomain`.

- [ ] **Step 3: Write minimal implementation**

In `internal/ingest/openapi.go`:

Add a `domain` field to the struct:

```go
type openAPISource struct {
	files  []string
	domain string
}
```

Keep the existing constructor and add the domain variant:

```go
func NewOpenAPISource(files []string) Source { return &openAPISource{files: files} }

// NewOpenAPISourceWithDomain is NewOpenAPISource plus a domain: every emitted
// ApiEndpoint concept gets a domain:<domain> tag. Empty domain == NewOpenAPISource.
func NewOpenAPISourceWithDomain(files []string, domain string) Source {
	return &openAPISource{files: files, domain: domain}
}
```

In `Fetch`, where tags are built (`tags := append([]string{"api", slug}, op.Tags...)`), append the domain tag:

```go
				tags := append([]string{"api", slug}, op.Tags...)
				if s.domain != "" {
					tags = append(tags, "domain:"+s.domain)
				}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/ingest/ -run OpenAPI`
Expected: PASS (both the existing OpenAPI tests and the new one).

- [ ] **Step 5: Commit**

```bash
git add internal/ingest/openapi.go internal/ingest/openapi_test.go
git commit -m "feat(ingest): NewOpenAPISourceWithDomain tags endpoints with a domain"
```

---

### Task 3: `openapi_specs` config key + buildSources wiring

**Files:**
- Modify: `cmd/pixkb/config.go`
- Modify: `cmd/pixkb/commands.go`
- Test: `cmd/pixkb/config_test.go`
- Test: `cmd/pixkb/commands_test.go`

**Interfaces:**
- Consumes: `ingest.NewOpenAPISourceWithDomain` (Task 2).
- Produces: `Config.OpenAPISpecs []OpenAPISpecConf` (`yaml:"openapi_specs"`), where `OpenAPISpecConf{ File string; Domain string }`; `buildSources` adds one `NewOpenAPISourceWithDomain` per entry.

- [ ] **Step 1: Write the failing tests**

Add to `cmd/pixkb/config_test.go`:

```go
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
```

Add to `cmd/pixkb/commands_test.go`:

```go
func TestBuildSources_IncludesOpenAPISpecsWhenConfigured(t *testing.T) {
	cfg := Config{OpenAPISpecs: []OpenAPISpecConf{{File: "mirror/openapi/x.json", Domain: "tax"}}}
	names := map[string]bool{}
	for _, s := range buildSources(cfg) {
		names[s.Name()] = true
	}
	assert.True(t, names["openapi"], "expected an openapi source when openapi_specs is set")
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./cmd/pixkb/ -run 'OpenAPISpecs'`
Expected: FAIL — `undefined: OpenAPISpecConf` / `cfg.OpenAPISpecs`.

- [ ] **Step 3: Write minimal implementation**

In `cmd/pixkb/config.go`, add the config type near `RepoConf`:

```go
// OpenAPISpecConf names a standalone OpenAPI/Swagger spec file to ingest (one
// outside the staged repo mirrors) and the KB domain its endpoints belong to.
type OpenAPISpecConf struct {
	File   string `yaml:"file"`
	Domain string `yaml:"domain"`
}
```

Add the field to `Config` (after `ScoutCrawlBaseURL`):

```go
	OpenAPISpecs []OpenAPISpecConf `yaml:"openapi_specs"` // standalone OpenAPI specs (e.g. the tax calculator), each with a domain tag
```

In `applyConfigFile`, add the merge (after the `ScoutCrawlBaseURL` block):

```go
	if len(fromFile.OpenAPISpecs) > 0 {
		cfg.OpenAPISpecs = fromFile.OpenAPISpecs
	}
```

In `cmd/pixkb/commands.go` `buildSources`, before `return srcs`, add:

```go
	for _, spec := range cfg.OpenAPISpecs {
		srcs = append(srcs, ingest.NewOpenAPISourceWithDomain([]string{spec.File}, spec.Domain))
	}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./cmd/pixkb/ -run 'OpenAPISpecs' && go build ./... && go vet ./...`
Expected: PASS, build clean, vet clean.

- [ ] **Step 5: Commit**

```bash
git add cmd/pixkb/config.go cmd/pixkb/config_test.go cmd/pixkb/commands.go cmd/pixkb/commands_test.go
git commit -m "feat(cli): openapi_specs config key wires standalone specs with a domain"
```

---

### Task 4: End-to-end validation + operator setup

**Files:**
- Modify: `pixkb.yaml.example`
- Test: local-DB integration run (documented below; no new Go test — the seams are unit-covered by Tasks 1-3, and the store-backed ingest has no fake-injection seam).

**Interfaces:**
- Consumes: everything from Tasks 1-3.

- [ ] **Step 1: Document the operator convention in `pixkb.yaml.example`**

Add, after the `api_docs`/scout-crawl entries:

```yaml
# Standalone OpenAPI/Swagger specs (outside the repo mirrors), each tagged with a
# KB domain. Place the spec file under the per-user mirror dir (see pdfs: note).
# The Receita Federal consumption-tax calculator's spec is at
# https://consumo.tributos.gov.br/servico/calcular-tributos-consumo/api/api-docs
# (also inside the offline calculadora distribution). domain must be pix or tax.
openapi_specs:
  - { file: "C:/Users/you/AppData/Local/PixKB/mirror/openapi/tributos-consumo.json", domain: tax }
```

- [ ] **Step 2: Place the tax spec + point local config at it (operator step)**

```bash
# Save the OpenAPI JSON to the per-user mirror dir (offline copy from calculadora.zip,
# or the api-docs endpoint). Windows path shown; adjust per OS.
mkdir -p "$LOCALAPPDATA/PixKB/mirror/openapi"
# copy tributos-consumo.json there, then add to the LOCAL (gitignored) pixkb.yaml:
#   openapi_specs:
#     - { file: "C:/Users/dyamm/AppData/Local/PixKB/mirror/openapi/tributos-consumo.json", domain: tax }
```

- [ ] **Step 3: Ingest against the local throwaway DB and assert cross-domain state**

```bash
task testdb:up
export PIXKB_DSN="$PIXKB_TEST_DSN"   # PowerShell: $env:PIXKB_DSN = <PIXKB_TEST_DSN>
go run ./cmd/pixkb db up
go run ./cmd/pixkb ingest
```

Expected: `ingest` completes with an `epoch 0: +N ...` line where N exceeds the pix-only count (the tax endpoints are added).

- [ ] **Step 4: Verify domain separation and cross-domain retrieval**

```bash
go run ./cmd/pixkb search "" --tag domain:tax --limit 20   # only tax ApiEndpoint concepts
go run ./cmd/pixkb search "" --tag domain:pix --limit 5    # existing pix concepts, now tagged
go run ./cmd/pixkb search "calculo CBS IBS" --limit 5      # a tax endpoint surfaces via hybrid RRF
```

Expected: `--tag domain:tax` returns only the tax endpoints; `--tag domain:pix` returns pix concepts; the free-text tax query surfaces a tax endpoint. Tear down: `task testdb:down`.

- [ ] **Step 5: Commit the example**

```bash
git add pixkb.yaml.example
git commit -m "docs(config): document openapi_specs + the tax calculator source"
```

---

## Notes for the executor

- **Do not mutate prod** (`192.168.15.100`). All validation is against the local throwaway DB. Re-baselining prod via ingest is a separate decision the user makes after seeing local results.
- If `search ""` (empty query with a tag filter) is not supported by the current `search` command, substitute a broad term that all tax endpoints share (e.g. their common path prefix) — the goal is only to confirm the `domain:tax` tag filters correctly; adjust the command, not the design.
- The tax OpenAPI spec is JSON; `NewOpenAPISource` decodes it via `yaml.v3`, which accepts JSON. No format conversion needed.
