# KB Scope Expansion — Phase B (Reforma Tributária legislation) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ingest the full text of LC 214/2025 as article-level `LegalArticle` concepts (tagged `domain:tax` + structural tags), via a new offline-PDF `legislation` source and config key, with no store migration.

**Architecture:** New `internal/ingest/legislation.go` adds a pure statute sectioner (`parseStatute`) + a `legislationSource` that reuses the existing `extractPDFText` and emits `LegalArticle` article concepts (plus one ementa concept and one `Reference` per Anexo). A `legislation:` config key wires it through `applyConfigFile`/`buildSources`, mirroring Phase A's `openapi_specs:` exactly. The Phase A `tagDomain` pass leaves the source's `domain:tax` tag intact.

**Tech Stack:** Go, `github.com/ledongthuc/pdf` (already vendored), `gopkg.in/yaml.v3`, testify, local pgvector for integration.

## Global Constraints

- **Air-gap:** the source reads a local offline PDF only — never the network.
- **No store migration:** `LegalArticle` is a new `okf.Concept.Type` string value; do not add DB columns or frontmatter fields.
- **Domain as tag:** every emitted concept carries `domain:tax` (so Phase A's `tagDomain` leaves it untouched); structural position is namespaced tags (`lei:`, `livro:`, `titulo:`, `capitulo:`, `secao:`, `subsecao:`), never new struct fields.
- **Deterministic output:** article ordering, IDs, and tags must be stable across runs (sectioner is a pure function of its input text).
- **LF line endings; no AI attribution in commits; stage explicit paths only (never `git add -A`/`.`/a dir).**
- **Reuse, don't fork:** reuse `extractPDFText`, `slugify`, `firstLine`, `okf.ComputeSHA` verbatim; do NOT modify `splitSections` (the Pix manual still uses it).

---

### Task 1: Statute sectioner (pure function)

**Files:**
- Create: `internal/ingest/legislation.go`
- Test: `internal/ingest/legislation_test.go`

**Interfaces:**
- Consumes: nothing (pure text → structs).
- Produces: `type statuteSection struct { Kind, Number, Title, Body, Livro, Titulo, Capitulo, Secao, Subsecao string }` and `func parseStatute(text string) []statuteSection`. `Kind` is one of `"ementa"`, `"article"`, `"anexo"`. Task 2 consumes these.

- [ ] **Step 1: Write the failing test**

```go
package ingest

import (
	"testing"

	"github.com/stretchr/testify/require"
)

const sampleStatute = `LEI COMPLEMENTAR Nº 214, DE 16 DE JANEIRO DE 2025

Institui o Imposto sobre Bens e Serviços (IBS) e a Contribuição Social sobre Bens e Serviços (CBS).

LIVRO I
NORMAS GERAIS

TÍTULO I
DISPOSIÇÕES PRELIMINARES

CAPÍTULO I
DO FATO GERADOR

Art. 1º Ficam instituídos o IBS e a CBS.
§ 1º O disposto neste artigo aplica-se às operações.
I - com bens materiais;
II - com serviços.

Art. 2º Considera-se operação onerosa.
Parágrafo único. Aplica-se às importações.

CAPÍTULO II
DO SPLIT PAYMENT

Art. 31. O recolhimento ocorre na liquidação financeira da transação.

Art. 31-A. O split payment aplica-se ao arranjo Pix.

ANEXO I
TABELA DE ALÍQUOTAS
Produto A - 10%
`

func TestParseStatute(t *testing.T) {
	secs := parseStatute(sampleStatute)

	// Ementa is the leading section before Art. 1º.
	require.GreaterOrEqual(t, len(secs), 1)
	require.Equal(t, "ementa", secs[0].Kind)
	require.Contains(t, secs[0].Body, "Institui o Imposto")

	arts := map[string]statuteSection{}
	var anexos []statuteSection
	for _, s := range secs {
		switch s.Kind {
		case "article":
			arts[s.Number] = s
		case "anexo":
			anexos = append(anexos, s)
		}
	}

	// Four articles: 1º, 2º, 31, 31-A.
	require.Len(t, arts, 4)

	a1 := arts["1º"]
	require.Equal(t, "article", a1.Kind)
	require.Contains(t, a1.Body, "Ficam instituídos")
	require.Contains(t, a1.Body, "com serviços")          // inciso accumulated
	require.Equal(t, "I", a1.Livro)
	require.Equal(t, "I", a1.Titulo)
	require.Equal(t, "I", a1.Capitulo)

	a2 := arts["2º"]
	require.Contains(t, a2.Body, "Parágrafo único")       // parágrafo único folded in

	a31 := arts["31"]
	require.Equal(t, "II", a31.Capitulo)                  // capítulo context advanced
	require.Contains(t, a31.Body, "liquidação financeira")

	_, ok := arts["31-A"]
	require.True(t, ok, "letter-suffixed article Art. 31-A must be captured")

	// One Anexo, emitted as an anexo section.
	require.Len(t, anexos, 1)
	require.Contains(t, anexos[0].Title, "ANEXO I")
	require.Contains(t, anexos[0].Body, "TABELA DE ALÍQUOTAS")
}

func TestParseStatuteEmpty(t *testing.T) {
	require.Empty(t, parseStatute("just some prose with no articles at all"),
		"text with no Art. markers and no anexo yields no article/anexo sections")
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/ingest/ -run TestParseStatute -v`
Expected: FAIL — `parseStatute` / `statuteSection` undefined.

- [ ] **Step 3: Write the sectioner**

Create `internal/ingest/legislation.go`:

```go
package ingest

import (
	"regexp"
	"strings"
)

// statuteSection is one unit of a parsed statute: the leading ementa, one
// artigo (with its §§/incisos/alíneas folded into Body), or one Anexo. Livro..
// Subsecao carry the structural context (roman numeral or "ÚNICO") in force at
// the article; empty means that level is not set.
type statuteSection struct {
	Kind     string // "ementa" | "article" | "anexo"
	Number   string // article raw number ("1º", "31-A"); "" for ementa; anexo label for anexo
	Title    string // display title (e.g. "Art. 1º", "ANEXO I", "Ementa")
	Body     string
	Livro    string
	Titulo   string
	Capitulo string
	Secao    string
	Subsecao string
}

const romanOrUnico = `[IVXLCDM]+|[ÚUÙ]NIC[OA]`

var (
	reLivro    = regexp.MustCompile(`(?i)^LIVRO\s+(` + romanOrUnico + `)\b`)
	reTitulo   = regexp.MustCompile(`(?i)^T[ÍI]TULO\s+(` + romanOrUnico + `)\b`)
	reCapitulo = regexp.MustCompile(`(?i)^CAP[ÍI]TULO\s+(` + romanOrUnico + `)\b`)
	reSecao    = regexp.MustCompile(`(?i)^SE[ÇC][ÃA]O\s+(` + romanOrUnico + `)\b`)
	reSubsecao = regexp.MustCompile(`(?i)^SUBSE[ÇC][ÃA]O\s+(` + romanOrUnico + `)\b`)
	reArt      = regexp.MustCompile(`^Art\.\s*(\d+[º°]?(?:-[A-Za-z])?)`)
	reAnexo    = regexp.MustCompile(`(?i)^ANEXO\s+(\S+)`)
)

// parseStatute splits Brazilian statute plain text into sections. It tracks the
// running Livro/Título/Capítulo/Seção/Subseção context and splits on "Art. N"
// markers, folding each article's following §§/incisos/alíneas into its body.
// Everything before the first article (and before the first structural heading)
// is the ementa. "ANEXO N" starts an anexo section that runs to the next anexo
// or EOF. A pure function of its input — no I/O.
func parseStatute(text string) []statuteSection {
	var out []statuteSection
	var ctx statuteSection // holds current Livro..Subsecao
	var cur *statuteSection
	inAnexo := false

	flush := func() {
		if cur == nil {
			return
		}
		cur.Body = strings.TrimSpace(cur.Body)
		if cur.Kind == "ementa" && cur.Body == "" {
			cur = nil
			return
		}
		out = append(out, *cur)
		cur = nil
	}

	// Seed an ementa collector; it flushes at the first structural heading or Art.
	cur = &statuteSection{Kind: "ementa", Title: "Ementa"}

	for _, raw := range strings.Split(text, "\n") {
		ln := strings.TrimSpace(raw)

		// Anexo heading: starts a new anexo section (structural headings inside
		// anexo bodies are ignored — anexos are tables, not articles).
		if m := reAnexo.FindStringSubmatch(ln); m != nil {
			flush()
			inAnexo = true
			cur = &statuteSection{Kind: "anexo", Number: m[1], Title: ln}
			continue
		}
		if inAnexo {
			cur.Body += raw + "\n"
			continue
		}

		// Structural headings update context and end the current section. They
		// reset all lower levels.
		if m := reLivro.FindStringSubmatch(ln); m != nil {
			flush()
			ctx.Livro, ctx.Titulo, ctx.Capitulo, ctx.Secao, ctx.Subsecao = up(m[1]), "", "", "", ""
			continue
		}
		if m := reTitulo.FindStringSubmatch(ln); m != nil {
			flush()
			ctx.Titulo, ctx.Capitulo, ctx.Secao, ctx.Subsecao = up(m[1]), "", "", ""
			continue
		}
		if m := reCapitulo.FindStringSubmatch(ln); m != nil {
			flush()
			ctx.Capitulo, ctx.Secao, ctx.Subsecao = up(m[1]), "", ""
			continue
		}
		if m := reSubsecao.FindStringSubmatch(ln); m != nil {
			flush()
			ctx.Subsecao = up(m[1])
			continue
		}
		if m := reSecao.FindStringSubmatch(ln); m != nil {
			flush()
			ctx.Secao, ctx.Subsecao = up(m[1]), ""
			continue
		}

		// Article heading: start a new article, inheriting the current context.
		if m := reArt.FindStringSubmatch(ln); m != nil {
			flush()
			cur = &statuteSection{
				Kind:     "article",
				Number:   m[1],
				Title:    "Art. " + m[1],
				Livro:    ctx.Livro,
				Titulo:   ctx.Titulo,
				Capitulo: ctx.Capitulo,
				Secao:    ctx.Secao,
				Subsecao: ctx.Subsecao,
			}
			cur.Body += raw + "\n"
			continue
		}

		// Any other line accumulates into the current section (ementa or article).
		// Lines between a structural heading and the next article (e.g. the
		// heading's descriptive name) have cur == nil and are dropped.
		if cur != nil {
			cur.Body += raw + "\n"
		}
	}
	flush()
	return out
}

// up upper-cases a roman-numeral/ÚNICO context token for stable tag slugs.
func up(s string) string { return strings.ToUpper(strings.TrimSpace(s)) }
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./internal/ingest/ -run TestParseStatute -v`
Expected: PASS (both `TestParseStatute` and `TestParseStatuteEmpty`).

- [ ] **Step 5: Commit**

```bash
git add internal/ingest/legislation.go internal/ingest/legislation_test.go
git commit -m "feat(ingest): statute-aware sectioner for legislation PDFs"
```

---

### Task 2: `legislation` source (concepts from articles)

**Files:**
- Modify: `internal/ingest/legislation.go` (append the source type + `Fetch`)
- Test: `internal/ingest/legislation_test.go` (append source tests)

**Interfaces:**
- Consumes: `parseStatute` (Task 1); `extractPDFText`, `slugify`, `firstLine`, `okf.ComputeSHA` (existing). `Source` interface (`Fetch(ctx) ([]okf.Concept, error)`, `Name() string`).
- Produces: `func NewLegislationSource(files []string, lei, domain string) Source`. Emits `LegalArticle` concepts (Kind=="article"), one ementa `LegalArticle`, and one `Reference` per anexo. `Name()` returns `"legislation"`.

- [ ] **Step 1: Write the failing test**

Append to `internal/ingest/legislation_test.go`:

```go
func TestLegislationConceptsFromText(t *testing.T) {
	// Exercise concept-building directly from parsed sections (no PDF needed).
	concepts := legislationConcepts(parseStatute(sampleStatute), "res.pdf", "lc-214-2025", "tax")

	byID := map[string]okfConceptView{}
	for _, c := range concepts {
		byID[c.ID] = okfConceptView{Type: c.Type, Title: c.Title, Tags: c.Tags, Body: c.Body}
	}

	// Article 1º → padded id, LegalArticle type, domain:tax + structural tags.
	a1, ok := byID["legislation/lc-214-2025/art-0001.md"]
	require.True(t, ok, "art 0001 concept must exist; got ids %v", keysOf(byID))
	require.Equal(t, "LegalArticle", a1.Type)
	require.Equal(t, "Art. 1º", a1.Title)
	require.Subset(t, a1.Tags, []string{"legislacao", "domain:tax", "lei:lc-214-2025", "livro:i", "titulo:i", "capitulo:i"})

	// Letter-suffixed article keeps its suffix in the id.
	_, ok = byID["legislation/lc-214-2025/art-0031-a.md"]
	require.True(t, ok, "art 0031-a concept must exist; got ids %v", keysOf(byID))

	// Ementa concept.
	_, ok = byID["legislation/lc-214-2025/art-0000-ementa.md"]
	require.True(t, ok, "ementa concept must exist")

	// Anexo → Reference type, anexo tag.
	var anexo *okfConceptView
	for id, c := range byID {
		if strings.Contains(id, "/anexo-") {
			cc := c
			anexo = &cc
		}
	}
	require.NotNil(t, anexo, "anexo Reference concept must exist")
	require.Equal(t, "Reference", anexo.Type)
	require.Subset(t, anexo.Tags, []string{"domain:tax", "lei:lc-214-2025", "anexo"})
}

func TestNewLegislationSourceName(t *testing.T) {
	require.Equal(t, "legislation", NewLegislationSource(nil, "lc-214-2025", "tax").Name())
}

type okfConceptView struct {
	Type, Title string
	Tags        []string
	Body        string
}

func keysOf(m map[string]okfConceptView) []string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	return ks
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/ingest/ -run 'TestLegislation|TestNewLegislation' -v`
Expected: FAIL — `legislationConcepts` / `NewLegislationSource` undefined.

- [ ] **Step 3: Append the source implementation**

Append to `internal/ingest/legislation.go` (add `context`, `fmt`, `path/filepath`, `strconv` to imports, plus `pixkb/internal/okf`):

```go
type legislationSource struct {
	files  []string
	lei    string // statute slug, e.g. "lc-214-2025"
	domain string // e.g. "tax"
}

// NewLegislationSource builds a Source that extracts each statute PDF and emits
// one LegalArticle concept per artigo (plus a leading ementa concept and one
// Reference per Anexo), tagged with the statute (lei:), domain, and structural
// position. Offline: reads local PDFs only.
func NewLegislationSource(files []string, lei, domain string) Source {
	return &legislationSource{files: files, lei: lei, domain: domain}
}

func (s *legislationSource) Name() string { return "legislation" }

func (s *legislationSource) Fetch(_ context.Context) ([]okf.Concept, error) {
	var out []okf.Concept
	for _, f := range s.files {
		text, err := extractPDFText(f)
		if err != nil {
			return nil, fmt.Errorf("legislation %s: %w", f, err)
		}
		out = append(out, legislationConcepts(parseStatute(text), f, s.lei, s.domain)...)
	}
	return out, nil
}

// legislationConcepts maps parsed statute sections to OKF concepts. Split out
// from Fetch so it is unit-testable without a real PDF.
func legislationConcepts(secs []statuteSection, resource, lei, domain string) []okf.Concept {
	if lei == "" {
		lei = "lei"
	}
	seen := map[string]bool{}
	var out []okf.Concept
	for _, sec := range secs {
		var id, typ string
		tags := []string{"legislacao", "lei:" + lei}
		if domain != "" {
			tags = append(tags, "domain:"+domain)
		}
		switch sec.Kind {
		case "ementa":
			id = fmt.Sprintf("legislation/%s/art-0000-ementa.md", lei)
			typ = "LegalArticle"
		case "article":
			id = fmt.Sprintf("legislation/%s/art-%s.md", lei, articleIDNum(sec.Number))
			typ = "LegalArticle"
			tags = appendStructuralTags(tags, sec)
		case "anexo":
			id = fmt.Sprintf("legislation/%s/anexo-%s.md", lei, slugify(sec.Number))
			typ = "Reference"
			tags = append(tags, "anexo")
		default:
			continue
		}
		// Disambiguate any accidental duplicate id (e.g. a mis-extracted number).
		if seen[id] {
			base := strings.TrimSuffix(id, ".md")
			for n := 2; ; n++ {
				alt := fmt.Sprintf("%s-dup%d.md", base, n)
				if !seen[alt] {
					id = alt
					break
				}
			}
		}
		seen[id] = true

		title := sec.Title
		body := "# " + title + "\n\n" + sec.Body
		out = append(out, okf.Concept{
			ID:          id,
			Type:        typ,
			Title:       title,
			Description: firstLine(sec.Body),
			Resource:    resource,
			Tags:        tags,
			Language:    "pt",
			SourceURI:   fmt.Sprintf("legislation:%s#%s", filepath.Base(resource), strings.TrimPrefix(strings.TrimPrefix(id, "legislation/"+lei+"/"), "art-")),
			Body:        body,
			ContentSHA:  okf.ComputeSHA(body),
		})
	}
	return out
}

func appendStructuralTags(tags []string, sec statuteSection) []string {
	for _, kv := range []struct{ k, v string }{
		{"livro", sec.Livro}, {"titulo", sec.Titulo}, {"capitulo", sec.Capitulo},
		{"secao", sec.Secao}, {"subsecao", sec.Subsecao},
	} {
		if kv.v != "" {
			tags = append(tags, kv.k+":"+strings.ToLower(kv.v))
		}
	}
	return tags
}

// articleIDNum turns a raw article number ("1º", "22", "31-A") into a
// zero-padded, sortable id fragment ("0001", "0022", "0031-a"). Non-numeric
// input falls back to a slug.
func articleIDNum(raw string) string {
	s := strings.ToLower(strings.TrimSpace(raw))
	num, suffix := s, ""
	if i := strings.IndexByte(s, '-'); i >= 0 {
		num, suffix = s[:i], s[i:]
	}
	num = strings.TrimRight(num, "º°.")
	n, err := strconv.Atoi(num)
	if err != nil {
		return slugify(raw)
	}
	return fmt.Sprintf("%04d%s", n, suffix)
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/ingest/ -run 'Statute|Legislation' -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/ingest/legislation.go internal/ingest/legislation_test.go
git commit -m "feat(ingest): legislation source emitting LegalArticle concepts"
```

---

### Task 3: Config key + buildSources wiring + example doc

**Files:**
- Modify: `cmd/pixkb/config.go` (add `LegislationConf`, `Config.Legislation`, merge)
- Modify: `cmd/pixkb/commands.go` (`buildSources` loop)
- Modify: `pixkb.yaml.example` (document the key)
- Test: `cmd/pixkb/config_test.go` (append a load test)

**Interfaces:**
- Consumes: `NewLegislationSource` (Task 2).
- Produces: `Config.Legislation []LegislationConf`; `buildSources` appends a legislation source per entry.

- [ ] **Step 1: Write the failing test**

Append to `cmd/pixkb/config_test.go`:

```go
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
```

(If `config_test.go` lacks the imports, add `os`, `path/filepath`, and testify `require`.)

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./cmd/pixkb/ -run TestApplyConfigFileLegislation -v`
Expected: FAIL — `cfg.Legislation` / `LegislationConf` undefined.

- [ ] **Step 3: Add the config type, field, and merge**

In `cmd/pixkb/config.go`, add the field to the `Config` struct (after `OpenAPISpecs`):

```go
	Legislation       []LegislationConf   `yaml:"legislation"`          // offline statute PDFs (e.g. LC 214/2025), each with a lei slug + domain
```

Add the type (after `OpenAPISpecConf`):

```go
// LegislationConf names an offline statute PDF to ingest as LegalArticle
// concepts, the statute slug (lei:) tagged onto every article, and the KB domain.
type LegislationConf struct {
	File   string `yaml:"file"`
	Lei    string `yaml:"lei"`
	Domain string `yaml:"domain"`
}
```

Add the merge in `applyConfigFile` (after the `OpenAPISpecs` merge):

```go
	if len(fromFile.Legislation) > 0 {
		cfg.Legislation = fromFile.Legislation
	}
```

- [ ] **Step 4: Wire it into `buildSources`**

In `cmd/pixkb/commands.go`, in `buildSources`, add before `return srcs` (after the `OpenAPISpecs` loop):

```go
	for _, l := range cfg.Legislation {
		srcs = append(srcs, ingest.NewLegislationSource([]string{l.File}, l.Lei, l.Domain))
	}
```

- [ ] **Step 5: Document the key in `pixkb.yaml.example`**

Append after the `openapi_specs:` block:

```yaml
# Offline statute PDFs ingested as article-level LegalArticle concepts (one per
# artigo), tagged with the statute (lei:) and a KB domain. Place the PDF under
# the per-user mirror dir (see pdfs: note). LC 214/2025 is the Reforma Tributária
# consumption-tax law (CBS/IBS/IS + split payment). domain must be pix or tax.
legislation:
  - { file: "C:/Users/you/AppData/Local/PixKB/mirror/legislation/LC214-2025.pdf", lei: lc-214-2025, domain: tax }
```

- [ ] **Step 6: Run tests to verify they pass**

Run: `go test ./cmd/pixkb/ -run TestApplyConfigFile -v && go build ./...`
Expected: PASS; build clean.

- [ ] **Step 7: Commit**

```bash
git add cmd/pixkb/config.go cmd/pixkb/commands.go cmd/pixkb/config_test.go pixkb.yaml.example
git commit -m "feat(config): legislation source config key + buildSources wiring"
```

---

### Task 4: Local-DB integration validation

**Files:**
- Create: `internal/ingest/testdata/sample-statute.txt` (a small statute fixture — NOT a PDF; drives a text-level integration assertion)
- Test: `internal/ingest/legislation_test.go` (append an end-to-end concept-shape assertion over the fixture)

**Interfaces:**
- Consumes: `legislationConcepts`, `parseStatute` (Tasks 1-2).
- Produces: a committed fixture + a test proving the full section→concept mapping over realistic multi-título text, and a manual validation note.

**Rationale:** A real LC 214 PDF is not present on a fresh checkout (it lives in the operator's mirror dir), so the automated gate uses a committed text fixture that exercises the sectioner + concept mapping deterministically. A documented manual step covers the real PDF against the local throwaway DB.

- [ ] **Step 1: Create the fixture**

Create `internal/ingest/testdata/sample-statute.txt` with representative content: an ementa, two Títulos, a Capítulo split, at least 5 articles including one `§`, one inciso list, one `Parágrafo único`, one letter-suffixed article, and one Anexo. (Model it on `sampleStatute` in the test, expanded to two Títulos so the título-reset is exercised.)

- [ ] **Step 2: Write the failing test**

Append:

```go
func TestLegislationFixtureEndToEnd(t *testing.T) {
	data, err := os.ReadFile("testdata/sample-statute.txt")
	require.NoError(t, err)

	concepts := legislationConcepts(parseStatute(string(data)), "LC214-2025.pdf", "lc-214-2025", "tax")
	require.NotEmpty(t, concepts)

	var articles, references int
	for _, c := range concepts {
		require.Contains(t, c.Tags, "domain:tax", "every legislation concept carries domain:tax")
		require.Contains(t, c.Tags, "lei:lc-214-2025")
		require.True(t, strings.HasPrefix(c.ID, "legislation/lc-214-2025/"))
		switch c.Type {
		case "LegalArticle":
			articles++
		case "Reference":
			references++
		}
	}
	require.GreaterOrEqual(t, articles, 5, "ementa + at least 5 articles")
	require.GreaterOrEqual(t, references, 1, "at least one anexo Reference")

	// No duplicate ids (the gather layer rejects them; catch it earlier here).
	ids := map[string]bool{}
	for _, c := range concepts {
		require.False(t, ids[c.ID], "duplicate concept id %q", c.ID)
		ids[c.ID] = true
	}
}
```

(Add `os` to the test imports if not already present.)

- [ ] **Step 3: Run to verify it fails, then passes**

Run: `go test ./internal/ingest/ -run TestLegislationFixtureEndToEnd -v`
Expected: FAIL first (missing fixture / assertion), then PASS once the fixture is in place and content matches.

- [ ] **Step 4: Full package test + build**

Run: `go test ./internal/ingest/... ./cmd/pixkb/... && go build ./...`
Expected: PASS; build clean.

- [ ] **Step 5: Manual validation note (no code)**

Add a short note to the PR/commit body documenting the real-PDF validation recipe (run by the operator, not CI), mirroring Phase A's `.scripts/02-*_validate` pattern:

```
# With LC214-2025.pdf placed in %LocalAppData%\PixKB\mirror\legislation\ and a
# legislation: entry in the LOCAL pixkb.yaml, against the throwaway DB (:5433):
#   task testdb:up
#   $env:PIXKB_DSN = <PIXKB_TEST_DSN>
#   go run ./cmd/pixkb db up
#   go run ./cmd/pixkb ingest                       # expect +N LegalArticle concepts
#   go run ./cmd/pixkb search "recolhimento na liquidação financeira split payment"
#   go run ./cmd/pixkb search "CBS IBS" --type LegalArticle   # isolates the statute
#   go run ./cmd/pixkb search "cálculo CBS IBS" --tag domain:tax  # law + tax endpoints together
#   task testdb:down
```

- [ ] **Step 6: Commit**

```bash
git add internal/ingest/testdata/sample-statute.txt internal/ingest/legislation_test.go
git commit -m "test(ingest): end-to-end legislation fixture + concept-shape gate"
```

---

## Self-Review

- **Spec coverage:** Decision 1 (LegalArticle type) → Task 2; Decision 2 (statute sectioner reusing extractPDFText) → Tasks 1-2; Decision 3 (structural tags) → Task 2 `appendStructuralTags`; Decision 4 (config, offline, mirror) → Task 3. Ementa + Anexo handling → Tasks 1-2. Testing section → Tasks 1,2,4. Open questions: (1) ID zero-padding → `articleIDNum` (4 digits); (2) Parágrafo único folded → asserted in Task 1; (3) Anexo per-Anexo `Reference` → Task 2; (4) fixture-vs-real-PDF → Task 4 (committed text fixture + manual real-PDF recipe). All covered.
- **Placeholder scan:** the only prose-only step is Task 4 Step 1 (fixture authoring) and Step 5 (manual note) — both are content-authoring, not code, and are described concretely. No TBDs.
- **Type consistency:** `statuteSection` fields, `parseStatute`, `legislationConcepts`, `NewLegislationSource`, `articleIDNum`, `appendStructuralTags`, `LegislationConf`/`Config.Legislation` names are identical across all tasks.
