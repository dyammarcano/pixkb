# Migrate pkg/agents onto github.com/inovacc/corral — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace pixkb's hand-maintained `pkg/agents` (agent-runtime core + vendor CLIs) with the published `github.com/inovacc/corral` module, keeping only pixkb-specific content (the BACEN-charter agent roster, the pixkb-branded host-plugin installer) as pixkb-owned packages.

**Architecture:** `corral` is confirmed (byte-level diff) to be pixkb's own `pkg/agents`, generalized and published. This is a full replace: add `corral` (+ its `agy`/`codex`/`claude`/`all` subpackages) as a dependency, relocate pixkb-specific content (`roster.go` → `internal/roster`, `host/` → `internal/agenthost`, `embed.go`'s `OpenAIEmbedder` → `internal/embed`), mechanically repoint every consumer's imports/qualifiers from `agents.*` to `corral.*`, then delete `pkg/agents/` entirely.

**Tech Stack:** Go 1.26.3 (bumped from 1.25.0), `github.com/inovacc/corral` (+ `agy`/`codex`/`claude`/`all` subpackages), existing pixkb deps unchanged.

## Global Constraints

- Full replace, no compatibility shim — `pkg/agents/` is deleted once nothing references it (see spec: this is an internal implementation swap, not a public API pixkb exposes).
- Only 3 providers: `claude`, `codex`, `agy`. Do NOT register `corral`'s `grok`/`kimi`.
- Bump pixkb's `go.mod` `go` directive to `1.26.3`; rely on `GOTOOLCHAIN=auto` (Go's default) to fetch it.
- No `Co-Authored-By: Claude` / AI attribution in commits.
- Zero behavior change to the BACEN-charter agent content (system prompts, schemas) — this is a structural move only.
- `pixkb agents install`'s target directory must stay `~/.claude/pixkb/` (etc.) — never rename to `~/.claude/corral/`. This is why `host/` becomes pixkb-owned (`internal/agenthost`) rather than switching to `corral`'s own `host` package (which hardcodes `installDir = "corral"`).
- Stage only the files each task actually touches when committing (never `git add -A`).
- Spec: `docs/superpowers/specs/2026-07-04-corral-agents-migration-design.md` — read it for the full rationale and file-by-file parity findings; this plan assumes it.

---

### Task 1: Add the corral dependency, bump go.mod

**Files:**
- Modify: `go.mod`, `go.sum`

**Interfaces:**
- Produces: `github.com/inovacc/corral` (and its `agy`/`codex`/`claude`/`all` subpackages) resolvable as a dependency for every later task.

- [ ] **Step 1: Bump the go directive**

In `go.mod`, change:
```
module pixkb

go 1.25.0
```
to:
```
module pixkb

go 1.26.3
```

- [ ] **Step 2: Add the corral dependency**

Run:
```bash
go get github.com/inovacc/corral@latest
```
This adds `github.com/inovacc/corral` (a single module — its `agy`/`codex`/`claude`/`all` subpackages resolve from the same module, no separate `go get` needed) to `go.mod`/`go.sum`.

- [ ] **Step 3: Verify the toolchain resolves and the existing codebase still builds**

Run: `go build ./...`
Expected: succeeds (may take longer than usual on first run if `GOTOOLCHAIN=auto` fetches `go1.26.3`). If this fails because `go1.26.3` cannot be fetched (no network / air-gapped build image), STOP and escalate — this is the go/no-go gate the spec flagged.

Run: `go vet ./...`
Expected: clean.

- [ ] **Step 4: Commit**

```bash
git add go.mod go.sum
git commit -m "build: add github.com/inovacc/corral dependency, bump go to 1.26.3"
```

---

### Task 2: Relocate OpenAIEmbedder to internal/embed

**Files:**
- Create: `internal/embed/openai.go`
- Create: `internal/embed/openai_test.go`
- Modify: `cmd/pixkb/config.go` (one call site)
- Delete: `pkg/agents/embed.go`
- Modify: `pkg/agents/core_test.go` (remove the two OpenAIEmbedder tests, ported below)

**Interfaces:**
- Produces: `embed.OpenAIEmbedder` (struct), `embed.NewOpenAIEmbedder(model string, dims int) (*OpenAIEmbedder, error)` — same names, same package-qualifier-free field names, just package `embed` instead of `agents`.
- Consumes: nothing from corral — this task is fully independent of Task 1's dependency (though Task 1 should land first per plan order, this task doesn't use it).

This is a pure relocation — the file's own content doesn't change except its `package` clause and import path. `pkg/agents/embed.go` already imports `pixkb/internal/embed` (for the `embed.Embedder` interface it implements), so moving it INTO that package removes that import entirely (no more need to reference itself by import).

- [ ] **Step 1: Create internal/embed/openai.go**

```go
package embed

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"sort"
	"time"
)

// OpenAIEmbedder implements Embedder against the OpenAI embeddings API
// (text-embedding-3-*): the same model embeds both concepts (at ingest) and
// queries (at search), so the vector arm is not near-random hashing noise.
//
// OPT-IN DEV-ONLY. This is a METERED API and therefore VIOLATES the air-gap
// rule (subscription agents only, no metered API). It is NOT the default and
// NOT the air-gap recall path — pointing BaseURL at a local OpenAI-compatible
// server is the only way to use it offline. The air-gap-compliant path to
// stronger recall is the agy agent fleet curating over pixdb (BACKLOG P2).
// See docs/agents-usage-signals.md and the air-gap memory.
type OpenAIEmbedder struct {
	APIKey  string
	Model   string
	Dims    int
	BaseURL string
	HTTP    *http.Client
}

// NewOpenAIEmbedder builds an embedder. model "" defaults to
// text-embedding-3-small; dims <= 0 defaults to 1536. The key comes from
// OPENAI_API_KEY; BaseURL from OPENAI_BASE_URL (default api.openai.com) so a
// local compatible server can be substituted.
func NewOpenAIEmbedder(model string, dims int) (*OpenAIEmbedder, error) {
	key := os.Getenv("OPENAI_API_KEY")
	if key == "" {
		return nil, fmt.Errorf("openai embedder: OPENAI_API_KEY not set")
	}
	if model == "" {
		model = "text-embedding-3-small"
	}
	if dims <= 0 {
		dims = 1536
	}
	base := os.Getenv("OPENAI_BASE_URL")
	if base == "" {
		base = "https://api.openai.com/v1"
	}
	return &OpenAIEmbedder{
		APIKey:  key,
		Model:   model,
		Dims:    dims,
		BaseURL: base,
		HTTP:    &http.Client{Timeout: 60 * time.Second},
	}, nil
}

func (e *OpenAIEmbedder) Name() string { return "openai:" + e.Model }
func (e *OpenAIEmbedder) Dim() int     { return e.Dims }

type openaiEmbedReq struct {
	Model      string   `json:"model"`
	Input      []string `json:"input"`
	Dimensions int      `json:"dimensions,omitempty"`
}

type openaiEmbedResp struct {
	Data []struct {
		Index     int       `json:"index"`
		Embedding []float32 `json:"embedding"`
	} `json:"data"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

// Embed returns one vector per input text, preserving input order.
func (e *OpenAIEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}
	body, err := json.Marshal(openaiEmbedReq{Model: e.Model, Input: texts, Dimensions: e.Dims})
	if err != nil {
		return nil, fmt.Errorf("openai embed: marshal: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.BaseURL+"/embeddings", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("openai embed: request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+e.APIKey)

	resp, err := e.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("openai embed: do: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	var er openaiEmbedResp
	if err := json.NewDecoder(resp.Body).Decode(&er); err != nil {
		return nil, fmt.Errorf("openai embed: decode (status %d): %w", resp.StatusCode, err)
	}
	if er.Error != nil {
		return nil, fmt.Errorf("openai embed: api error (status %d): %s", resp.StatusCode, er.Error.Message)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("openai embed: status %d", resp.StatusCode)
	}
	if len(er.Data) != len(texts) {
		return nil, fmt.Errorf("openai embed: got %d vectors for %d inputs", len(er.Data), len(texts))
	}
	// The API may return data out of order; sort by index to match inputs.
	sort.Slice(er.Data, func(i, j int) bool { return er.Data[i].Index < er.Data[j].Index })
	out := make([][]float32, len(er.Data))
	for i := range er.Data {
		out[i] = er.Data[i].Embedding
	}
	return out, nil
}

var _ Embedder = (*OpenAIEmbedder)(nil)
```

Note: `embedReq`/`embedResp` are renamed to `openaiEmbedReq`/`openaiEmbedResp` (prefixed) since they now live in the shared `internal/embed` package alongside the hashing embedder's own types — avoids any future name collision. `var _ embed.Embedder` becomes `var _ Embedder` (same package now).

- [ ] **Step 2: Create internal/embed/openai_test.go**

```go
package embed

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestOpenAIEmbedder(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-key" {
			http.Error(w, "no auth", http.StatusUnauthorized)
			return
		}
		var body openaiEmbedReq
		_ = json.NewDecoder(r.Body).Decode(&body)
		resp := openaiEmbedResp{}
		for i := len(body.Input) - 1; i >= 0; i-- { // out of order to test sorting
			resp.Data = append(resp.Data, struct {
				Index     int       `json:"index"`
				Embedding []float32 `json:"embedding"`
			}{Index: i, Embedding: []float32{float32(i), 0.5}})
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	t.Setenv("OPENAI_API_KEY", "test-key")
	t.Setenv("OPENAI_BASE_URL", srv.URL)
	e, err := NewOpenAIEmbedder("", 2)
	if err != nil {
		t.Fatalf("new embedder: %v", err)
	}
	vecs, err := e.Embed(context.Background(), []string{"a", "b", "c"})
	if err != nil {
		t.Fatalf("embed: %v", err)
	}
	if len(vecs) != 3 {
		t.Fatalf("want 3 vecs, got %d", len(vecs))
	}
	for i := range vecs {
		if vecs[i][0] != float32(i) {
			t.Errorf("vec %d out of input order: %v", i, vecs[i])
		}
	}
}

func TestOpenAIEmbedderNoKey(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "")
	if _, err := NewOpenAIEmbedder("", 0); err == nil {
		t.Error("want error when OPENAI_API_KEY unset")
	}
}
```

- [ ] **Step 3: Run the new tests**

Run: `go test ./internal/embed/... -run TestOpenAIEmbedder -v`
Expected: both tests PASS.

- [ ] **Step 4: Update cmd/pixkb/config.go's call site**

In `cmd/pixkb/config.go`, find:
```go
	case "openai":
		// Optional high-recall embeddings via an OpenAI-compatible API. NOT the
		// default: the project drives quality through the agy agent fleet over
		// pixdb (read/curate/write-back), not a metered embedding API. Kept as
		// an opt-in for deployments that want it (point OPENAI_BASE_URL at a
		// local server to stay offline).
		return agents.NewOpenAIEmbedder(envOr("PIXKB_EMBED_MODEL", ""), embedDims(cfg))
```
Change the call to:
```go
	case "openai":
		// Optional high-recall embeddings via an OpenAI-compatible API. NOT the
		// default: the project drives quality through the agy agent fleet over
		// pixdb (read/curate/write-back), not a metered embedding API. Kept as
		// an opt-in for deployments that want it (point OPENAI_BASE_URL at a
		// local server to stay offline).
		return embed.NewOpenAIEmbedder(envOr("PIXKB_EMBED_MODEL", ""), embedDims(cfg))
```
Check the top of `cmd/pixkb/config.go`'s import block: it already imports `"pixkb/internal/embed"` (used by the `"hashing"` case, `embed.NewHashing(256)`), so no import changes are needed — only the qualifier `agents.`→`embed.` on this one line. If `"pixkb/pkg/agents"` was imported ONLY for this call (grep to confirm: `grep -n '"pixkb/pkg/agents"' cmd/pixkb/config.go` and `grep -n 'agents\.' cmd/pixkb/config.go` — the second should show zero matches after this edit), remove that now-unused import line.

- [ ] **Step 5: Delete pkg/agents/embed.go**

```bash
rm pkg/agents/embed.go
```

- [ ] **Step 6: Remove the ported tests from pkg/agents/core_test.go**

In `pkg/agents/core_test.go`, delete the `TestOpenAIEmbedder` and `TestOpenAIEmbedderNoKey` functions (now duplicated in `internal/embed/openai_test.go`) and remove the now-unused `"encoding/json"`, `"net/http"`, `"net/http/httptest"` imports from that file if nothing else in it uses them (check: `TestRosterRegistered` and `TestComposePromptEmbedsSchema` and `TestDoctor` remain in this file for now — they get handled in later tasks — verify which imports they still need before trimming).

- [ ] **Step 7: Verify the whole repo still builds and tests pass**

Run: `go build ./...` — expected clean.
Run: `go vet ./...` — expected clean.
Run: `go test ./internal/embed/... ./cmd/pixkb/... ./pkg/agents/...` — expected all pass (pkg/agents still functions fully at this point; only the embedder moved out of it).

- [ ] **Step 8: Commit**

```bash
git add internal/embed/openai.go internal/embed/openai_test.go cmd/pixkb/config.go pkg/agents/embed.go pkg/agents/core_test.go
git commit -m "refactor: relocate OpenAIEmbedder from pkg/agents to internal/embed"
```

---

### Task 3: Create internal/roster (the BACEN-charter agent roster on corral)

**Files:**
- Create: `internal/roster/roster.go`
- Create: `internal/roster/roster_test.go`

**Interfaces:**
- Consumes: `corral.Agent`, `corral.Kind`, `corral.Register` (from Task 1's dependency).
- Produces: 13 registered agents (`control`, `gather`, `scraper`, `normalization`, `quality`, `governance`, `research`, `diagram`, `hygiene`, `deviation`, `enrich`, `answerer`, `judge`) available via `corral.All()`/`corral.ByName(name)` once this package is imported (blank or explicit) anywhere in the running binary.

This task does NOT touch `pkg/agents/roster.go` yet (it stays in place, inert, until Task 9's deletion) — `internal/roster` is purely additive at this point.

- [ ] **Step 1: Create internal/roster/roster.go**

```go
// Package roster is pixkb's BACEN-charter agent roster: the normative,
// Pix/SPB-domain-expert system prompts for every agent in the KB-lifecycle
// fleet, registered against github.com/inovacc/corral's global agent
// registry. Blank-import this package (`_ "pixkb/internal/roster"`) anywhere
// the fleet needs to be populated — mirrors how corral/all is blank-imported
// to populate the provider registry.
package roster

import "github.com/inovacc/corral"

// judgeSchema mirrors eval/judge-schema.json so the judge agent emits the same
// structured verdict the eval harness already aggregates.
const judgeSchema = `{
  "type": "object",
  "additionalProperties": false,
  "required": ["case_id","query","top_hit","relevance","precision","verdict","critique","enhancements"],
  "properties": {
    "case_id":     {"type": "string"},
    "query":       {"type": "string"},
    "top_hit":     {"type": "string"},
    "relevance":   {"type": "integer", "minimum": 0, "maximum": 5},
    "precision":   {"type": "integer", "minimum": 0, "maximum": 5},
    "verdict":     {"type": "string", "enum": ["pass","weak","fail"]},
    "critique":    {"type": "string"},
    "enhancements":{"type": "array", "items": {"type": "string"}}
  }
}`

// conceptSchema is the structured shape the scraper/normalization agents emit:
// clean OKF concepts ready for the bundle.
const conceptSchema = `{
  "type": "object",
  "additionalProperties": false,
  "required": ["concepts"],
  "properties": {
    "concepts": {
      "type": "array",
      "items": {
        "type": "object",
        "additionalProperties": false,
        "required": ["id","type","title","body","tags","language","source_uri"],
        "properties": {
          "id":         {"type": "string"},
          "type":       {"type": "string"},
          "title":      {"type": "string"},
          "body":       {"type": "string"},
          "tags":       {"type": "array", "items": {"type": "string"}},
          "language":   {"type": "string", "enum": ["pt","en"]},
          "source_uri": {"type": "string"}
        }
      }
    }
  }
}`

// enrichSchema is the MINIMAL shape the enrich agent emits: a concept id and its
// generated intent_terms (recall synonyms / alternate phrasings). It deliberately
// carries NO body/title/tags — the curate enrich loop MERGES these terms onto the
// existing concept, so the agent can never mangle or wipe the canonical content.
// OpenAI-strict: every property is required and additionalProperties is false.
const enrichSchema = `{
  "type": "object",
  "additionalProperties": false,
  "required": ["concepts"],
  "properties": {
    "concepts": {
      "type": "array",
      "items": {
        "type": "object",
        "additionalProperties": false,
        "required": ["id","intent_terms"],
        "properties": {
          "id":           {"type": "string"},
          "intent_terms": {"type": "string"}
        }
      }
    }
  }
}`

// answerSchema is the RAG answerer's structured reply: a grounded answer, the
// concept ids it cites, and an explicit refusal flag for when the context does
// not support an answer. OpenAI-strict: every property required, no extras.
const answerSchema = `{
  "type": "object",
  "additionalProperties": false,
  "required": ["answer","citations","refused"],
  "properties": {
    "answer":    {"type": "string"},
    "citations": {"type": "array", "items": {"type": "string"}},
    "refused":   {"type": "boolean"}
  }
}`

// qualitySchema is the structured verdict the quality/governance agents emit.
const qualitySchema = `{
  "type": "object",
  "additionalProperties": false,
  "required": ["id","score","issues","admit"],
  "properties": {
    "id":     {"type": "string"},
    "score":  {"type": "integer", "minimum": 0, "maximum": 5},
    "issues": {"type": "array", "items": {"type": "string"}},
    "admit":  {"type": "boolean"}
  }
}`

// pixkbContract is appended to every agent's system prompt: the KB is reached
// ONLY through pixkb's MCP verbs, never the database or bundle files directly.
// This makes pixkb the agent's self-contained operating surface.
const pixkbContract = "\n\n--- pixkb operating contract ---\n" +
	"The Pix/SPB knowledge base is reached EXCLUSIVELY through pixkb's MCP verbs — " +
	"never touch Postgres or the bundle files directly:\n" +
	"  query/verify: search, related, stats, concept_get\n" +
	"  enrich:       concept_upsert (write curated concepts back to pixdb)\n" +
	"  rebuild:      reindex\n" +
	"Operate autonomously in a loop: search -> inspect (concept_get/related) -> " +
	"verify (stats) -> enrich (concept_upsert) -> reindex -> re-search. Every written " +
	"concept must carry provenance (source_uri) and must not be fabricated."

// domainCharter is the enforced scope of the KB: the BACEN normative view of
// Pix/SPB ONLY — never a single participant's implementation. Every agent is a
// Pix domain expert bound by it.
const domainCharter = "\n\n--- BACEN domain charter (ENFORCED) ---\n" +
	"You are a domain expert in Brazil's Pix / SPB arrangement as defined by BACEN " +
	"(Banco Central do Brasil). The KB holds the NORMATIVE, specification-level view ONLY.\n" +
	"IN SCOPE: the Pix arrangement and its rules; SPI settlement (liquidação, reservas); " +
	"DICT (directory of keys, reivindicação/claims); ISO 20022 messages (pacs.008, pacs.002, " +
	"pacs.004, camt.*); cobrança (cob/cobv/cobr, QR BR Code/EMV, Pix Copia e Cola); devolução; " +
	"Pix Automático; BACEN-defined identifiers (EndToEndId/E2EID, RtrId — prefix, ISPB, " +
	"timestamp, 32-char format); regulation (Resoluções BCB, Manuais, prazos); security per " +
	"BACEN (mTLS, certificates); LGPD.\n" +
	"OUT OF SCOPE — REJECT or STRIP: any single participant's IMPLEMENTATION. Never admit " +
	"app/microservice names, internal database schemas or columns, message brokers/topics " +
	"(Pulsar/Kafka), deployment or infra (ArgoCD, Kubernetes, namespaces), internal correlation " +
	"IDs, or company-specific contracts/protos. Those describe HOW one project implements Pix, " +
	"not WHAT BACEN defines.\n" +
	"RULE: when a source mixes specification with implementation, KEEP the BACEN concept and " +
	"DROP the implementation detail. A concept that only makes sense inside one project does " +
	"NOT belong in this KB. Describe the flow from BACEN's view, never a particular project's."

// register appends the BACEN domain charter and the pixkb operating contract,
// then adds the agent to corral's global roster.
func register(a corral.Agent) {
	a.System += domainCharter + pixkbContract
	corral.Register(func() corral.Agent { return a })
}

func init() {
	register(corral.Agent{
		Name: "control", Kind: corral.KindControl,
		Description: "Orchestrates the KB lifecycle: plans which agents to run and in what order.",
		Tools:       []string{"pixkb"},
		System: "You are the control agent for pixkb, the de-facto knowledge base for Brazil Pix/SPB " +
			"(BACEN). You orchestrate specialist agents — gather, scraper, normalization, quality, " +
			"governance, research, judge — to keep the KB complete, accurate, well-segmented, and " +
			"searchable. Given a goal, decide the minimal sequence of agents to run, run them, and " +
			"report what changed. Prefer deterministic adapters (gather) for structured sources and " +
			"reserve scraping/normalization for messy or JS-rendered content. Never fabricate Pix facts.",
	})

	register(corral.Agent{
		Name: "gather", Kind: corral.KindGather,
		Description: "Runs the deterministic source adapters (ISO, PDF, OpenAPI, git, markdown, apidoc).",
		Tools:       []string{"pixkb"},
		System: "You are the gather agent. Trigger pixkb's deterministic ingest over the configured " +
			"sources, then verify with stats and report the concepts produced (counts by type). Do not " +
			"edit content; gathering is mechanical and reproducible.",
	})

	register(corral.Agent{
		Name: "scraper", Kind: corral.KindScraper,
		Description: "Fetches and renders web pages — including JS-rendered BACEN SPAs — into clean concepts.",
		Tools:       []string{"pixkb", "web"},
		Schema:      conceptSchema,
		System: "You are the scraper agent. Fetch the given BACEN/gov URL, render it if it is a " +
			"JavaScript SPA (the bcb.gov.br estabilidadefinanceira pages are Angular apps that return " +
			"an empty shell to static fetchers), and extract only the substantive Pix/SPB specification " +
			"content — drop nav, headers, footers, cookie banners. Emit one concept per coherent section " +
			"with a meaningful title, cleaned body, language (pt/en), and the source URL as source_uri, " +
			"then write them via concept_upsert. Never invent content not present on the page.",
	})

	register(corral.Agent{
		Name: "normalization", Kind: corral.KindNormalization,
		Description: "Turns raw/extracted text into clean, well-titled OKF concepts.",
		Tools:       []string{"pixkb"},
		Schema:      conceptSchema,
		System: "You are the normalization agent. Given raw extracted text (often noisy PDF or HTML), " +
			"produce clean OKF concepts: derive a meaningful, specific title for each section (never a " +
			"fragment like 'ANEXO IV' or 'CONCLUÍDA é'), keep the substantive body, set the correct " +
			"language, and preserve provenance. Split overly long sections and merge orphan fragments. " +
			"Do not summarize away technical detail (field names, codes, status values). Per the BACEN " +
			"domain charter, STRIP every implementation-specific reference and re-express it as the " +
			"canonical BACEN concept (e.g. 'orchestration-go-pix-in' -> 'Recebedor PSP / pix-in flow'; an " +
			"internal table/column -> the BACEN identifier it stores). Write via concept_upsert.",
	})

	register(corral.Agent{
		Name: "quality", Kind: corral.KindQuality,
		Description: "Scores concept quality and flags weak concepts for fix or removal.",
		Tools:       []string{"pixkb"},
		Schema:      qualitySchema,
		System: "You are the quality agent. Read a concept with concept_get, score it 0–5 on title " +
			"clarity, body completeness, searchability for a Pix/SPB engineer, and BACEN-canonical purity. " +
			"List concrete issues. Set admit=false for junk (fragment titles, empty/duplicate bodies, OCR " +
			"noise) AND for any concept coupled to a particular participant's implementation rather than " +
			"the BACEN view (per the domain charter).",
	})

	register(corral.Agent{
		Name: "governance", Kind: corral.KindGovernance,
		Description: "Enforces OKF/provenance rules and gates what enters the canonical bundle.",
		Tools:       []string{"pixkb"},
		Schema:      qualitySchema,
		System: "You are the governance agent for the BACEN KB and the last gate before the bundle. " +
			"Enforce the rules: every concept must have provenance (source_uri), must not fabricate Pix " +
			"facts, must be OKF-compliant (concept-per-file, stable id), must respect LGPD (no personal " +
			"data) and license terms, and must not duplicate an existing concept (check with search/" +
			"related). CRITICALLY, enforce the BACEN domain charter: set admit=false for ANY concept that " +
			"carries a single participant's implementation — app/service names, internal DB schemas/" +
			"columns, brokers/topics, infra (ArgoCD/k8s), internal correlation IDs, or company protos. " +
			"List the violated rule for each rejection.",
	})

	register(corral.Agent{
		Name: "research", Kind: corral.KindResearch,
		Description: "Fills gaps surfaced by the judge by researching topics and proposing concepts.",
		Tools:       []string{"pixkb", "web"},
		Schema:      conceptSchema,
		System: "You are the research agent. Given a weak or failing judge case (a query the KB answers " +
			"poorly per search), research the topic in authoritative BACEN/ISO sources and write new " +
			"concepts via concept_upsert that would satisfy the query, with provenance. Only propose " +
			"content grounded in real sources; never fabricate.\n" +
			"Language note: write your own commentary/summaries in English. When a source is in " +
			"Portuguese, keep the new concept's body/title faithful to that source language — do not " +
			"force-translate canonical BACEN/Pix regulatory text.",
	})

	register(corral.Agent{
		Name: "diagram", Kind: corral.KindDiagram,
		Description: "Renders BACEN Pix flows as mermaid (preferred) or draw.io diagrams.",
		Tools:       []string{"pixkb", "mermaid", "drawio"},
		Schema:      conceptSchema,
		System: "You are the diagram agent. Visualize the canonical BACEN Pix flows — pix-in " +
			"(pacs.008/pacs.002), devolução (pacs.004/camt.056), DICT key resolution, cobrança " +
			"lifecycle, SPI settlement — using the mermaid and draw.io plugin workflows.\n" +
			"Prefer MERMAID (diagrams-as-code: embeds in the concept markdown, renders in git, OKF-" +
			"native). Use `sequenceDiagram` for message flows between actors and `flowchart`/`stateDiagram` " +
			"for lifecycles. Follow the mermaid plugin: write the .mmd, VALIDATE with `mmdc` (or the Kroki " +
			"API) before emitting, then embed the validated fenced ```mermaid block in the concept body. " +
			"Use draw.io (.drawio XML -> SVG/PNG via the desktop CLI) only when an exportable, richly-styled " +
			"architecture picture is needed.\n" +
			"Actors are BACEN-canonical ONLY (Pagador PSP, Recebedor PSP, SPI, DICT, PSP API) — per the " +
			"domain charter, NEVER a particular project's app/service/DB names. Write each diagram as a " +
			"concept via concept_upsert with provenance.",
	})

	register(corral.Agent{
		Name: "hygiene", Kind: corral.KindHygiene,
		Description: "Fixes mechanical KB problems flagged by hygiene_scan: junk titles, broken links, duplicates, stubs.",
		Tools:       []string{"pixkb"},
		Schema:      conceptSchema,
		System: "You are the hygiene agent. Call hygiene_scan to get the deterministic KB health report, " +
			"then fix the MECHANICAL findings — never invent facts:\n" +
			"  junk-title  -> concept_get the concept and rewrite a specific, meaningful title (never a " +
			"fragment like 'ANEXO IV'); keep the body.\n" +
			"  broken-link -> repair or drop the dangling [[id]] cross-link (verify the target with search).\n" +
			"  duplicate   -> merge: keep the richer concept, redirect/remove the lesser (confirm with concept_get).\n" +
			"  stub-body   -> enrich from an authoritative BACEN source ONLY if one exists (else leave for research).\n" +
			"Write each repaired concept via concept_upsert with its original provenance preserved. Do NOT " +
			"touch deviation findings — those belong to the deviation agent.\n" +
			"Language note: write any notes or commentary of your own in English. The concept's own " +
			"body/title stays in its source language — never force-translate canonical BACEN Portuguese " +
			"content.",
	})

	register(corral.Agent{
		Name: "deviation", Kind: corral.KindDeviation,
		Description: "Corrects BACEN-charter deviations: strips implementation specifics, re-expresses as canonical.",
		Tools:       []string{"pixkb"},
		Schema:      conceptSchema,
		System: "You are the deviation-correction agent — the enforcer of the BACEN domain charter. Call " +
			"hygiene_scan and take ONLY the 'deviation' findings (implementation-specific content: app/" +
			"service names, brokers like Pulsar/Kafka, infra like ArgoCD/Kubernetes, DB schemas/columns, " +
			"internal correlation IDs, company protos). For each: concept_get the concept and REWRITE it to " +
			"the NORMATIVE BACEN view — keep the Pix/SPB specification meaning, DROP every implementation " +
			"detail, re-express participant specifics as the canonical role (e.g. 'orchestration-go-pix-in' " +
			"-> 'Recebedor PSP / pix-in'; an internal table/column -> the BACEN identifier it stores). If a " +
			"concept ONLY makes sense inside one project (no BACEN concept survives the strip), mark it for " +
			"removal in your critique instead of upserting. Write corrected concepts via concept_upsert; the " +
			"deterministic gate will re-scan and reject any that still deviate.\n" +
			"Language note: write your critique and any commentary of your own in English. Strip only " +
			"implementation detail — never translate the concept's surviving BACEN body/title out of its " +
			"source language.",
	})

	register(corral.Agent{
		Name: "enrich", Kind: corral.KindEnrich,
		Description: "Generates intent_terms (recall synonyms / alternate phrasings) for un-enriched concepts.",
		Tools:       []string{"pixkb"},
		Schema:      enrichSchema,
		System: "You are the enrich agent. Your ONE job is to raise SEARCH RECALL by generating " +
			"intent_terms for a concept — the alternate ways a Pix/SPB engineer or layperson might " +
			"phrase a query that should land on this concept. You are given the concept's id, title, " +
			"and body. Derive terms STRICTLY from that content — never invent facts, never add a term " +
			"the concept does not actually cover.\n" +
			"Produce a single space-separated string of lowercase terms covering: SIGLA expansions and " +
			"their abbreviations (e.g. 'EVP' <-> 'chave aleatória', 'E2E' <-> 'EndToEndId', 'DICT' <-> " +
			"'diretório de identificadores'), Portuguese synonyms and common phrasings (e.g. 'estorno' " +
			"for devolução, 'cobrança' for cob/cobv), ISO message ids spelled out (e.g. 'pacs.008' -> " +
			"'pagamento crédito'), and frequent layperson wording — but ONLY where faithful to the " +
			"concept. Do NOT repeat the exact title verbatim (it is already indexed). Do NOT include " +
			"stopwords, punctuation, or duplicates. Keep it tight: roughly 8–20 high-value terms.\n" +
			"Return ONE concepts[] entry: the SAME id and the intent_terms string. Emit nothing else — " +
			"no body, no title. Per the BACEN charter, intent_terms must NEVER contain implementation " +
			"specifics (app/service names, brokers, infra, DB columns); a deterministic gate re-scans " +
			"them and rejects any that do.\n" +
			"Language note: write any commentary of your own in English. Do not translate the concept's " +
			"title/body or the BACEN/Pix Portuguese terminology you surface in intent_terms — that content " +
			"stays exactly as sourced.",
	})

	register(corral.Agent{
		Name: "answerer", Kind: corral.KindAnswerer,
		Description: "RAG: synthesizes a grounded, citation-backed answer STRICTLY from retrieved KB context.",
		Tools:       []string{"pixkb"},
		Schema:      answerSchema,
		System: "You are the answerer agent for pixkb — the BACEN Pix/SPB knowledge base. You are given a " +
			"QUESTION and a CONTEXT block of retrieved concepts, each fenced with its concept id and " +
			"source. Answer the question STRICTLY and ONLY from that context. Rules, in priority order:\n" +
			"  1. FAITHFULNESS above all — never state a Pix fact that is not supported by the provided " +
			"context. A wrong fact about a normative arrangement is worse than no answer.\n" +
			"  2. CITE every claim: put the concept id(s) you used in `citations` (use the exact ids shown " +
			"in the context fences). Do not cite an id that is not in the context.\n" +
			"  3. REFUSE when the context does not contain the answer: set refused=true and put a short " +
			"'não consta na base de conhecimento' note in `answer` with empty citations. Do the same for " +
			"an empty/off-topic (out-of-domain) context — never pad with outside knowledge.\n" +
			"  4. Stay BACEN-normative (the specification view, never one participant's implementation) and " +
			"LGPD-safe (never emit personal data). Answer in the question's language (pt/en).\n" +
			"Return ONLY the JSON: answer, citations (concept ids), refused.",
	})

	register(corral.Agent{
		Name: "judge", Kind: corral.KindJudge,
		Description: "Evaluates search quality: runs the search verb and scores relevance/precision.",
		Tools:       []string{"pixkb"},
		Schema:      judgeSchema,
		System: "You are a STRICT evaluator of the pixkb knowledge base for Brazil Pix/SPB. For the given " +
			"case, call the search verb for the query (optionally with a type filter, or related to inspect " +
			"the graph), judge whether the TOP results satisfy the intent, score relevance (0–5) and " +
			"precision (0–5, penalising noisy top hits), identify the top hit id, and propose concrete KB " +
			"enhancements. Return only the JSON verdict.",
	})
}
```

- [ ] **Step 2: Create internal/roster/roster_test.go**

This ports and expands `pkg/agents/core_test.go`'s `TestRosterRegistered` — the original only checked 8 of the 13 agents (predates `diagram`/`hygiene`/`deviation`/`enrich`/`answerer` being added); this version checks all 13.

```go
package roster_test

import (
	"strings"
	"testing"

	"github.com/inovacc/corral"

	_ "pixkb/internal/roster" // populate corral's registry
)

func TestRosterRegistered(t *testing.T) {
	all := corral.All()
	if len(all) < 13 {
		t.Fatalf("want >=13 agents, got %d", len(all))
	}
	names := []string{
		"control", "gather", "scraper", "normalization", "quality", "governance",
		"research", "diagram", "hygiene", "deviation", "enrich", "answerer", "judge",
	}
	for _, name := range names {
		a, ok := corral.ByName(name)
		if !ok {
			t.Errorf("agent %q not registered", name)
			continue
		}
		if !strings.Contains(a.System, "pixkb operating contract") {
			t.Errorf("agent %q missing pixkb contract", name)
		}
		if !strings.Contains(a.System, "BACEN domain charter") {
			t.Errorf("agent %q missing BACEN domain charter", name)
		}
	}

	j, _ := corral.ByName("judge")
	if !strings.Contains(j.Schema, "relevance") {
		t.Error("judge agent missing structured schema")
	}

	e, _ := corral.ByName("enrich")
	if !strings.Contains(e.Schema, "intent_terms") {
		t.Error("enrich agent missing intent_terms schema")
	}

	ans, _ := corral.ByName("answerer")
	if !strings.Contains(ans.Schema, "refused") {
		t.Error("answerer agent missing refused field in schema")
	}
}
```

- [ ] **Step 3: Run the test**

Run: `go build ./... && go test ./internal/roster/... -v`
Expected: `TestRosterRegistered` PASS.

- [ ] **Step 4: Commit**

```bash
git add internal/roster/roster.go internal/roster/roster_test.go
git commit -m "feat: add internal/roster — BACEN-charter agent fleet on corral"
```

---

### Task 4: Create internal/agenthost (pixkb-branded host-plugin installer)

**Files:**
- Create: `internal/agenthost/host.go`
- Create: `internal/agenthost/hosts.go`
- Create: `internal/agenthost/host_test.go`
- Delete: `pkg/agents/host/host.go`, `pkg/agents/host/hosts.go`, `pkg/agents/host/host_test.go`

**Interfaces:**
- Consumes: `corral.Agent`, `corral.All()` (Task 1's dependency); `internal/roster` (Task 3, needed only for the tests that assert real agent content, e.g. `agents/judge.md` existing).
- Produces: `agenthost.Host`, `agenthost.All()`, `agenthost.ByName(name)`, `agenthost.Install(h, base, dryRun)`, `agenthost.MCPManifest(bin)`, `agenthost.AgentMarkdown(a corral.Agent)` — same names as today's `host` package, package renamed to `agenthost`. `installDir` stays `"pixkb"` — this is the whole point of not using corral's own `host` package.

- [ ] **Step 1: Create internal/agenthost/host.go**

```go
// Package agenthost installs pixkb as a self-contained agent plugin across
// multiple coding-agent hosts (Claude Code, Codex, Antigravity). It mirrors
// lensr's pkg/aihost: a lazy-factory Host registry whose members generate a
// plugin tree (agent definitions + an .mcp.json that registers `pixkb mcp
// serve`) and write it atomically into each host's config. Loading the host
// then surfaces pixkb's verbs as the agent's self-contained tool set.
//
// This package is pixkb-owned (not corral's own `host` package) specifically
// so installDir stays "pixkb" — corral's host package hardcodes "corral",
// which would silently rename every existing install's target directory.
package agenthost

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/inovacc/corral"
)

// Host is one coding-agent target. The minimal surface is Name/Root/Files;
// Doctor is provided by every built-in host.
type Host interface {
	// Name is the short host id ("claude", "codex", "agy").
	Name() string
	// Root returns the host's config dir under base (base "" => default,
	// resolved from the user's home dir).
	Root(base string) (string, error)
	// Files returns plugin-tree files keyed by path relative to the install
	// dir (Root/pixkb).
	Files() (map[string][]byte, error)
	// Doctor reports install readiness.
	Doctor(base string) Report
}

// Check is one health-check line; Report rolls them up.
type Check struct {
	Name    string `json:"name"`
	Verdict string `json:"verdict"` // PASS | WARN | FAIL
	Detail  string `json:"detail,omitempty"`
}

// Report is a host's aggregate health.
type Report struct {
	Host    string  `json:"host"`
	Target  string  `json:"target"`
	Verdict string  `json:"verdict"` // OK | DEGRADED | FAILED
	Checks  []Check `json:"checks"`
}

// InstallResult summarizes one install.
type InstallResult struct {
	Host    string
	Target  string
	Written int
	Planned []string // dry-run: relative paths that would be written
}

var factories []func() Host

// register adds a host factory (called from init()).
func register(f func() Host) { factories = append(factories, f) }

// All returns every registered host, sorted by name.
func All() []Host {
	out := make([]Host, 0, len(factories))
	for _, f := range factories {
		out = append(out, f())
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name() < out[j].Name() })
	return out
}

// ByName returns the host with the given name.
func ByName(name string) (Host, bool) {
	for _, h := range All() {
		if h.Name() == name {
			return h, true
		}
	}
	return nil, false
}

// installDir is the namespaced subdir pixkb writes into under a host root, so
// installs never clobber the user's own host config.
const installDir = "pixkb"

// Install writes a host's plugin tree atomically. base overrides the host root
// ("" = default). When dryRun is set, no files are written; the planned paths
// are returned instead.
func Install(h Host, base string, dryRun bool) (InstallResult, error) {
	root, err := h.Root(base)
	if err != nil {
		return InstallResult{}, err
	}
	target := filepath.Join(root, installDir)
	files, err := h.Files()
	if err != nil {
		return InstallResult{}, err
	}
	res := InstallResult{Host: h.Name(), Target: target}

	rels := make([]string, 0, len(files))
	for rel := range files {
		rels = append(rels, rel)
	}
	sort.Strings(rels)

	for _, rel := range rels {
		if dryRun {
			res.Planned = append(res.Planned, rel)
			continue
		}
		if err := writeFileAtomic(filepath.Join(target, filepath.FromSlash(rel)), files[rel]); err != nil {
			return res, err
		}
		res.Written++
	}
	return res, nil
}

// writeFileAtomic writes data to path via tmp+rename, creating parents.
func writeFileAtomic(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("mkdir %q: %w", filepath.Dir(path), err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("write %q: %w", tmp, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("rename %q: %w", path, err)
	}
	return nil
}

// --- shared plugin-tree generation -------------------------------------------

// MCPManifest is the .mcp.json registering `pixkb mcp serve` (Codex/Claude
// share this format). bin "" defaults to "pixkb".
func MCPManifest(bin string) []byte {
	if bin == "" {
		bin = "pixkb"
	}
	// Hand-built so the output is stable and dependency-free.
	return []byte(`{
  "mcpServers": {
    "pixkb": {
      "command": "` + jsonEscape(bin) + `",
      "args": ["mcp", "serve"]
    }
  }
}
`)
}

// AgentMarkdown renders one corral.Agent as a host agent-definition file: YAML
// frontmatter (name/description/kind/tools) followed by the system prompt.
func AgentMarkdown(a corral.Agent) []byte {
	var b strings.Builder
	b.WriteString("---\n")
	fmt.Fprintf(&b, "name: %s\n", a.Name)
	fmt.Fprintf(&b, "description: %s\n", yamlScalar(a.Description))
	fmt.Fprintf(&b, "kind: %s\n", a.Kind)
	b.WriteString("tools: [" + strings.Join(a.Tools, ", ") + "]\n")
	b.WriteString("---\n\n")
	b.WriteString(a.System)
	b.WriteString("\n")
	return []byte(b.String())
}

// readme explains how to enable the generated plugin for a host.
func readme(host string) []byte {
	return []byte("# pixkb plugin for " + host + "\n\n" +
		"Generated by `pixkb agents install`. This bundle makes pixkb the agent's\n" +
		"self-contained tool surface.\n\n" +
		"- `.mcp.json` registers `pixkb mcp serve` (search/related/stats/concept_get/\n" +
		"  concept_upsert/reindex). Point your " + host + " config at it.\n" +
		"- `agents/*.md` are the agent definitions (control, gather, scraper,\n" +
		"  normalization, quality, governance, research, judge).\n\n" +
		"The agent reaches the KB ONLY through the pixkb verbs.\n")
}

// sharedFiles builds the plugin tree common to every host.
func sharedFiles(host string) map[string][]byte {
	files := map[string][]byte{
		".mcp.json": MCPManifest(""),
		"README.md": readme(host),
	}
	for _, a := range corral.All() {
		files["agents/"+a.Name+".md"] = AgentMarkdown(a)
	}
	return files
}

// homeRoot resolves base (or the user's home dir) joined with sub.
func homeRoot(base, sub string) (string, error) {
	if base != "" {
		return filepath.Join(base, sub), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home: %w", err)
	}
	return filepath.Join(home, sub), nil
}

func jsonEscape(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	return strings.ReplaceAll(s, `"`, `\"`)
}

func yamlScalar(s string) string {
	if strings.ContainsAny(s, ":#") {
		return `"` + strings.ReplaceAll(s, `"`, `\"`) + `"`
	}
	return s
}
```

- [ ] **Step 2: Create internal/agenthost/hosts.go**

```go
package agenthost

import (
	"os"
	"os/exec"
	"path/filepath"
)

func init() {
	register(func() Host { return claudeHost{} })
	register(func() Host { return codexHost{} })
	register(func() Host { return antigravityHost{} })
}

// doctorFor is the shared host health check: install root resolvable + the host
// CLI present on PATH.
func doctorFor(name, sub, bin string, base string) Report {
	r := Report{Host: name}
	root, err := homeRoot(base, sub)
	if err != nil {
		return Report{Host: name, Verdict: "FAILED", Checks: []Check{{Name: "root", Verdict: "FAIL", Detail: err.Error()}}}
	}
	r.Target = filepath.Join(root, installDir)

	parent := Check{Name: "config-dir"}
	if fi, err := os.Stat(root); err == nil && fi.IsDir() {
		parent.Verdict, parent.Detail = "PASS", root
	} else {
		// Not fatal: Install creates it. Flag as a heads-up.
		parent.Verdict, parent.Detail = "WARN", root+" (will be created)"
	}
	r.Checks = append(r.Checks, parent)

	cli := Check{Name: "cli:" + bin}
	if p, err := exec.LookPath(bin); err == nil {
		cli.Verdict, cli.Detail = "PASS", p
	} else {
		cli.Verdict, cli.Detail = "WARN", bin+" not on PATH"
	}
	r.Checks = append(r.Checks, cli)

	r.Verdict = "OK"
	for _, c := range r.Checks {
		if c.Verdict == "FAIL" {
			r.Verdict = "FAILED"
		}
	}
	return r
}

// --- Claude Code -------------------------------------------------------------

type claudeHost struct{}

func (claudeHost) Name() string                      { return "claude" }
func (claudeHost) Root(base string) (string, error)  { return homeRoot(base, ".claude") }
func (claudeHost) Files() (map[string][]byte, error) {
	return sharedFiles("Claude Code"), nil
}
func (claudeHost) Doctor(base string) Report { return doctorFor("claude", ".claude", "claude", base) }

// --- Codex -------------------------------------------------------------------

type codexHost struct{}

func (codexHost) Name() string                      { return "codex" }
func (codexHost) Root(base string) (string, error)  { return homeRoot(base, ".codex") }
func (codexHost) Files() (map[string][]byte, error) {
	return sharedFiles("Codex"), nil
}
func (codexHost) Doctor(base string) Report { return doctorFor("codex", ".codex", "codex", base) }

// --- Antigravity (agy) -------------------------------------------------------

type antigravityHost struct{}

func (antigravityHost) Name() string                     { return "agy" }
func (antigravityHost) Root(base string) (string, error) { return homeRoot(base, ".antigravity") }
func (antigravityHost) Files() (map[string][]byte, error) {
	return sharedFiles("Antigravity"), nil
}
func (antigravityHost) Doctor(base string) Report {
	return doctorFor("agy", ".antigravity", "agy", base)
}
```

- [ ] **Step 3: Create internal/agenthost/host_test.go**

Ported from `pkg/agents/host/host_test.go`, package renamed, needs `internal/roster` blank-imported so `corral.All()` (used inside `sharedFiles`) has real agents to enumerate (the original relied on `pkg/agents/roster.go`'s `init()` running implicitly since it was the SAME package; now roster lives in a separate package that must be imported).

```go
package agenthost_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"pixkb/internal/agenthost"
	_ "pixkb/internal/roster" // populate corral's registry (judge, control, ...)
)

func TestRegistry(t *testing.T) {
	hosts := agenthost.All()
	if len(hosts) != 3 {
		t.Fatalf("want 3 hosts, got %d", len(hosts))
	}
	for _, name := range []string{"claude", "codex", "agy"} {
		if _, ok := agenthost.ByName(name); !ok {
			t.Errorf("host %q not registered", name)
		}
	}
}

func TestMCPManifest(t *testing.T) {
	m := string(agenthost.MCPManifest("C:/bin/pixkb.exe"))
	for _, want := range []string{`"pixkb"`, `"mcp", "serve"`, `C:/bin/pixkb.exe`} {
		if !strings.Contains(m, want) {
			t.Errorf("manifest missing %q:\n%s", want, m)
		}
	}
}

func TestSharedFilesHaveAgentsAndManifest(t *testing.T) {
	h, _ := agenthost.ByName("codex")
	files, err := h.Files()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := files[".mcp.json"]; !ok {
		t.Error("missing .mcp.json")
	}
	if _, ok := files["agents/judge.md"]; !ok {
		t.Error("missing agents/judge.md")
	}
	// Agent md carries frontmatter + the pixkb contract.
	jb := string(files["agents/judge.md"])
	if !strings.HasPrefix(jb, "---\nname: judge\n") {
		t.Errorf("judge.md frontmatter wrong:\n%s", jb[:min(80, len(jb))])
	}
	if !strings.Contains(jb, "pixkb operating contract") {
		t.Error("judge.md missing pixkb contract")
	}
}

func TestInstallWritesTree(t *testing.T) {
	base := t.TempDir()
	h, _ := agenthost.ByName("claude")

	// Dry-run writes nothing but plans files.
	dr, err := agenthost.Install(h, base, true)
	if err != nil {
		t.Fatal(err)
	}
	if dr.Written != 0 || len(dr.Planned) == 0 {
		t.Fatalf("dry-run wrote=%d planned=%d", dr.Written, len(dr.Planned))
	}
	if _, err := os.Stat(filepath.Join(base, ".claude", "pixkb")); !os.IsNotExist(err) {
		t.Error("dry-run created files")
	}

	// Real install writes the tree under base/.claude/pixkb — confirms
	// installDir is still "pixkb", not corral's own "corral".
	res, err := agenthost.Install(h, base, false)
	if err != nil {
		t.Fatal(err)
	}
	if res.Written == 0 {
		t.Fatal("install wrote nothing")
	}
	for _, rel := range []string{".mcp.json", "README.md", "agents/control.md", "agents/judge.md"} {
		p := filepath.Join(base, ".claude", "pixkb", filepath.FromSlash(rel))
		if _, err := os.Stat(p); err != nil {
			t.Errorf("expected %s: %v", rel, err)
		}
	}
}

func TestDoctor(t *testing.T) {
	base := t.TempDir()
	// Doctor is exercised through the registered host, not the unexported
	// claudeHost type directly (that type is package-private to agenthost).
	h, _ := agenthost.ByName("claude")
	r := h.Doctor(base)
	if r.Host != "claude" || r.Verdict == "" || len(r.Checks) == 0 {
		t.Fatalf("bad report: %+v", r)
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
```

Note: the original `TestDoctor` called `claudeHost{}.Doctor(base)` directly since it was an internal test (`package host`, not `host_test`). This ported version uses `package agenthost_test` (external) per the plan's preference for testing through the public API where the internal test isn't exercising anything unexported-specific — `claudeHost` is unexported so an external test can't construct it directly; going through `ByName("claude")` exercises the same `Doctor` method and is equivalent. `min` is defined locally since this may run on a Go version where a package doesn't have it in scope for test files depending on build tags — but since go.mod is now `1.26.3`, the builtin `min` (from Go 1.21+) is available; DELETE the local `min` func below and use the builtin instead:

Remove the trailing `func min(a, b int) int { ... }` from the test file above — Go 1.26.3 has a builtin `min`.

- [ ] **Step 4: Run the tests**

Run: `go build ./... && go test ./internal/agenthost/... -v`
Expected: all 5 tests PASS.

- [ ] **Step 5: Delete the old host package**

```bash
rm -r pkg/agents/host
```

- [ ] **Step 6: Verify nothing else references pkg/agents/host yet**

Run: `grep -rl "pixkb/pkg/agents/host" --include=*.go .`
Expected: only `cmd/pixkb/agents.go` (handled in Task 7).

- [ ] **Step 7: Commit**

```bash
git add internal/agenthost pkg/agents/host
git commit -m "refactor: relocate host-plugin installer from pkg/agents/host to internal/agenthost"
```

---

### Task 5: Swap internal/rag and internal/kbmcp onto corral.Agency

**Files:**
- Modify: `internal/rag/adapters.go`
- Modify: `internal/kbmcp/server.go`

**Interfaces:**
- Consumes: `corral.Agency` (Task 1).

- [ ] **Step 1: Update internal/rag/adapters.go**

Find:
```go
import (
	"context"
	"path/filepath"

	"pixkb/internal/embed"
	"pixkb/internal/okf"
	"pixkb/internal/query"
	"pixkb/internal/store/postgres"
	"pixkb/pkg/agents"
)
```
Replace the `"pixkb/pkg/agents"` import with `"github.com/inovacc/corral"`.

Find:
```go
type AgentGenerator struct{ Agency *agents.Agency }
```
Replace with:
```go
type AgentGenerator struct{ Agency *corral.Agency }
```

- [ ] **Step 2: Update internal/kbmcp/server.go**

Find:
```go
	Agency *agents.Agency // may be nil; enables the kb_ask (RAG) tool when set
```
Replace with:
```go
	Agency *corral.Agency // may be nil; enables the kb_ask (RAG) tool when set
```
Update the import block: replace `"pixkb/pkg/agents"` with `"github.com/inovacc/corral"` (check the exact current import path first — `grep -n '"pixkb/pkg/agents"' internal/kbmcp/server.go` — this file's `Deps` struct is the only known reference from earlier investigation, but double check there isn't a second `agents.` usage elsewhere in the file before removing the import).

- [ ] **Step 3: Verify build**

Run: `go build ./internal/rag/... ./internal/kbmcp/...`
Expected: FAILS at this point — other files (`internal/curate`, `cmd/pixkb`) still construct `*agents.Agency` and pass it into these now-`*corral.Agency`-typed fields, and `pkg/agents` itself still exists and compiles standalone. This is expected and fine: Go type-checks per package, so `internal/rag`/`internal/kbmcp` alone will build clean; the REPO-WIDE build breaks until Task 7 finishes the last construction sites. Run the narrower command above (not `go build ./...`) to confirm THESE two packages compile in isolation.

Run: `go vet ./internal/rag/... ./internal/kbmcp/...`
Expected: clean.

- [ ] **Step 4: Commit**

```bash
git add internal/rag/adapters.go internal/kbmcp/server.go
git commit -m "refactor: swap internal/rag and internal/kbmcp onto corral.Agency"
```

---

### Task 6: Swap internal/curate onto corral

**Files:**
- Modify: `internal/curate/fixer.go`
- Modify: `internal/curate/e2e_test.go`

**Interfaces:**
- Consumes: `corral.Agency`, `corral.Agent` (Task 1).

- [ ] **Step 1: Update internal/curate/fixer.go**

Find:
```go
import (
	"context"
	"fmt"
	"strings"

	"pixkb/internal/hygiene"
	"pixkb/internal/okf"
	"pixkb/pkg/agents"
)
```
Replace `"pixkb/pkg/agents"` with `"github.com/inovacc/corral"`.

Find:
```go
type AgencyFixer struct {
	Agency *agents.Agency
}
```
Replace with:
```go
type AgencyFixer struct {
	Agency *corral.Agency
}
```
(No other `agents.` reference exists in this file per the earlier grep — `Agency.Run` is a method call, not a qualified type reference.)

- [ ] **Step 2: Update internal/curate/e2e_test.go**

Run `grep -n 'agents\.' internal/curate/e2e_test.go` and `grep -n '"pixkb/pkg/agents' internal/curate/e2e_test.go` first to see the exact current usages (not fully captured during spec research — this file wasn't read in full). Apply the same mechanical swap: `"pixkb/pkg/agents"` → `"github.com/inovacc/corral"` (and `"pixkb/pkg/agents/all"` → `"github.com/inovacc/corral/all"` if present), every `agents.` qualifier → `corral.`. If the test constructs an `AgencyFixer{Agency: ...}` via `agents.NewAgency(...)`, that becomes `corral.NewAgency(...)`. If the test invokes any ROSTER agent by name (e.g. `ag.Run(ctx, "hygiene", ...)` or similar), add `_ "pixkb/internal/roster"` to its imports (blank import) so the named agent resolves — `pkg/agents/roster.go` populated this implicitly today since it was the same binary/package tree; after the split, the roster must be explicitly imported wherever a named agent is looked up by string.

- [ ] **Step 3: Verify build**

Run: `go build ./internal/curate/...`
Expected: clean (this package doesn't depend on the CLI-layer files still mid-migration).

Run: `go vet ./internal/curate/...`
Expected: clean.

Run: `go test ./internal/curate/... -short`
Expected: all non-e2e tests pass unchanged (the e2e test itself is `-short`-skipped, matching its existing convention — confirm this convention still applies after your edit by checking the test's skip guard).

- [ ] **Step 4: Commit**

```bash
git add internal/curate/fixer.go internal/curate/e2e_test.go
git commit -m "refactor: swap internal/curate onto corral.Agency"
```

---

### Task 7: Swap the cmd/pixkb CLI layer onto corral

**Files:**
- Modify: `cmd/pixkb/agents.go`
- Modify: `cmd/pixkb/ask.go`
- Modify: `cmd/pixkb/curate.go`
- Modify: `cmd/pixkb/eval.go`
- Modify: `cmd/pixkb/mcp.go`

**Interfaces:**
- Consumes: `corral.NewAgency`, `corral.All`, `corral.ByName`, `corral.Agent`, `corral.Doctor`, `corral.ProviderByName`, `corral.ProviderUsage`, `corral/codex.StatusRefs`/`CheckDrift`/`FetchCurrentSHAs`/`UpstreamMirrors`, `internal/agenthost` (Task 4), `internal/roster` (Task 3), `github.com/inovacc/corral/all`.

- [ ] **Step 1: Update cmd/pixkb/agents.go**

Find:
```go
import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"pixkb/pkg/agents"
	_ "pixkb/pkg/agents/all" // registers codex/claude/agy providers
	"pixkb/pkg/agents/codex"
	"pixkb/pkg/agents/host"
)
```
Replace with:
```go
import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/inovacc/corral"
	_ "github.com/inovacc/corral/all" // registers codex/claude/agy providers
	"github.com/inovacc/corral/codex"

	"pixkb/internal/agenthost"
	_ "pixkb/internal/roster" // populates corral's roster (control, gather, ..., judge)
)
```

Then, throughout the file, replace every qualifier:
- `agents.All()` → `corral.All()`
- `agents.Doctor()` → `corral.Doctor()`
- `agents.NewAgency(...)` → `corral.NewAgency(...)`
- `agents.ProviderByName(...)` → `corral.ProviderByName(...)`
- `agents.ProviderUsage(...)` → `corral.ProviderUsage(...)`
- `host.All()`, `host.ByName(...)`, `host.Install(...)` → `agenthost.All()`, `agenthost.ByName(...)`, `agenthost.Install(...)`
- `host.Host` (the type, if referenced, e.g. in `newAgentsInstallCmd`'s `hosts := host.All(); ... hosts = []host.Host{h}`) → `agenthost.Host`

`codex.StatusRefs`, `codex.UpstreamMirrors`, `codex.FetchCurrentSHAs`, `codex.CheckDrift` stay as `codex.*` — only the import path changed (from `pixkb/pkg/agents/codex` to `github.com/inovacc/corral/codex`), the package-local qualifier is unaffected since both are named `codex`.

- [ ] **Step 2: Update cmd/pixkb/ask.go**

Find the import of `"pixkb/pkg/agents"` and any `_ "pixkb/pkg/agents/all"`; replace with `"github.com/inovacc/corral"` and `_ "github.com/inovacc/corral/all"`. Add `_ "pixkb/internal/roster"` (the `ask` command runs the `answerer` agent by name via `rag.Ask`→`AgentGenerator.Generate`→`Agency.Run(ctx, "answerer", prompt)`, so the roster must be populated). Replace `agents.NewAgency(...)` with `corral.NewAgency(...)`.

- [ ] **Step 3: Update cmd/pixkb/curate.go**

Same mechanical swap: `"pixkb/pkg/agents"` → `"github.com/inovacc/corral"`, any `_ "pixkb/pkg/agents/all"` → `_ "github.com/inovacc/corral/all"`, add `_ "pixkb/internal/roster"` (curate runs `hygiene`/`deviation`/`enrich`/`research` agents by name — the roster must be populated here too), `agents.NewAgency(...)` → `corral.NewAgency(...)`.

- [ ] **Step 4: Update cmd/pixkb/eval.go**

Same mechanical swap at line ~355's `agents.NewAgency(provider, dir)` (the `rag-diversity` eval command) → `corral.NewAgency(provider, dir)`. Add `_ "pixkb/internal/roster"` and `_ "github.com/inovacc/corral/all"` to this file's imports if `eval.go` doesn't already get them transitively from another file in the same `main` package being compiled together — Go compiles all files in a package together, so if `cmd/pixkb/agents.go` (in the same package `main`) already blank-imports `_ "github.com/inovacc/corral/all"` and `_ "pixkb/internal/roster"`, `eval.go` does NOT need to re-import them (blank imports are package-build-wide, not per-file) — confirm this and SKIP adding duplicate blank imports to every file; only `agents.go` needs them (already the pattern today: only `agents.go` currently carries `_ "pixkb/pkg/agents/all"`, and `eval.go`/`ask.go`/`curate.go` rely on that same binary having it loaded since they're all `package main` together).

Given that clarification, **revise Steps 2 and 3 above**: `cmd/pixkb/ask.go` and `cmd/pixkb/curate.go` do NOT need their own `_ "pixkb/internal/roster"` or `_ "github.com/inovacc/corral/all"` blank imports — only `cmd/pixkb/agents.go` needs them (as it does today for `_ "pixkb/pkg/agents/all"`), since all `cmd/pixkb/*.go` files link into the same `main` binary. Each file only needs the DIRECT `corral`/`corral/codex` imports for the types/functions it actually references by qualifier.

- [ ] **Step 5: Update cmd/pixkb/mcp.go**

Same mechanical swap for whatever `agents.` reference wires `kbmcp.Deps.Agency` (construct via `corral.NewAgency`). Replace `"pixkb/pkg/agents"` import with `"github.com/inovacc/corral"`.

- [ ] **Step 6: Verify the full repo builds**

Run: `go build ./...`
Expected: clean — this is the point where every consumer has moved off `pkg/agents`' core types EXCEPT `pkg/agents` itself (which still compiles standalone, unreferenced by anything else now). Confirm with:
```bash
grep -rl "pixkb/pkg/agents" --include=*.go . | grep -v '^\./pkg/agents/'
```
Expected: **no output** (zero external references remain).

Run: `go vet ./...`
Expected: clean.

- [ ] **Step 7: Run the full test suite**

Run: `go test ./... -short`
Expected: all packages pass (DSN-gated integration tests skip as usual without `PIXKB_TEST_DSN`).

- [ ] **Step 8: Commit**

```bash
git add cmd/pixkb/agents.go cmd/pixkb/ask.go cmd/pixkb/curate.go cmd/pixkb/eval.go cmd/pixkb/mcp.go
git commit -m "refactor: swap cmd/pixkb CLI layer onto corral, wire internal/roster + internal/agenthost"
```

---

### Task 8: Port the live e2e test

**Files:**
- Create: `internal/roster/roster_e2e_test.go`
- Delete: `pkg/agents/agency_e2e_test.go`

**Interfaces:**
- Consumes: `corral.NewAgency`, `corral.Agent` directly (this test builds an ad-hoc agent, NOT one from the roster — it does not actually need `internal/roster`'s registered agents, only the package's own name as a sensible home for "the pixkb fleet's e2e test").

- [ ] **Step 1: Create internal/roster/roster_e2e_test.go**

```go
package roster_test

import (
	"context"
	"encoding/json"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/inovacc/corral"
	_ "github.com/inovacc/corral/all" // registers codex/claude/agy providers
)

// e2eConceptSchema mirrors the roster conceptSchema (OpenAI-strict: every
// property in required) so the agent reply is structured.
const e2eConceptSchema = `{
  "type": "object",
  "additionalProperties": false,
  "required": ["concepts"],
  "properties": {
    "concepts": {
      "type": "array",
      "items": {
        "type": "object",
        "additionalProperties": false,
        "required": ["id","type","title","body","tags","language","source_uri"],
        "properties": {
          "id":         {"type": "string"},
          "type":       {"type": "string"},
          "title":      {"type": "string"},
          "body":       {"type": "string"},
          "tags":       {"type": "array", "items": {"type": "string"}},
          "language":   {"type": "string", "enum": ["pt","en"]},
          "source_uri": {"type": "string"}
        }
      }
    }
  }
}`

// firstAvailableProvider returns a coding-agent backend whose CLI is on PATH, or
// "" if none. Codex is preferred (cheaper, native --output-schema).
func firstAvailableProvider() string {
	for _, p := range []struct{ name, bin string }{{"codex", "codex"}, {"claude", "claude"}} {
		if _, err := exec.LookPath(p.bin); err == nil {
			return p.name
		}
	}
	return ""
}

// TestAgency_RealAgentEmitsStructuredConcept is the live half of the fleet
// round-trip: a real coding-agent CLI, driven through corral's Agency with a
// conceptSchema, must return a parseable OKF concept. Combined with the MCP
// concept_upsert->search round-trip (internal/kbmcp), this proves the
// agent -> structured output -> write-back -> retrieve loop the Curator runs.
//
// Skipped under -short (it spends a real subscription turn) and when no provider
// CLI is installed, so the default `-short` suite stays fast and offline. Uses
// an ad-hoc corral.Agent (not one from internal/roster's registered fleet) —
// this test proves the Agency/Provider contract works end to end, independent
// of any specific roster agent's content.
func TestAgency_RealAgentEmitsStructuredConcept(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping live agent e2e in -short mode (spends a real subscription turn)")
	}
	prov := firstAvailableProvider()
	if prov == "" {
		t.Skip("no codex/claude CLI on PATH")
	}

	ag, err := corral.NewAgency(prov, t.TempDir())
	if err != nil {
		t.Fatalf("NewAgency(%s): %v", prov, err)
	}
	defer func() { _ = ag.Close() }()

	agent := corral.Agent{
		Name:   "e2e-emitter",
		Schema: e2eConceptSchema,
		System: "You output ONLY the requested concept as JSON matching the schema. " +
			"Do not add prose. Preserve the exact id and source_uri given.",
	}
	const wantID = "reference/e2e/spi-marker.md"
	input := "Emit exactly one concept: id=" + wantID + ", type=Reference, " +
		"title='SPI — Sistema de Pagamentos Instantâneos', " +
		"body='O SPI é a infraestrutura de liquidação instantânea operada pelo BACEN.', " +
		"language=pt, source_uri='test:e2e', tags=['spi']."

	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()

	res, err := ag.RunAgent(ctx, agent, input)
	if err != nil {
		t.Fatalf("RunAgent: %v", err)
	}

	// Tolerant extraction: codex native schema returns clean JSON; claude may
	// fence or wrap it.
	raw := strings.TrimSpace(res.Text)
	if i, j := strings.IndexByte(raw, '{'), strings.LastIndexByte(raw, '}'); i >= 0 && j > i {
		raw = raw[i : j+1]
	}
	var doc struct {
		Concepts []struct {
			ID    string `json:"id"`
			Type  string `json:"type"`
			Title string `json:"title"`
			Body  string `json:"body"`
		} `json:"concepts"`
	}
	if err := json.Unmarshal([]byte(raw), &doc); err != nil {
		t.Fatalf("agent reply not parseable as conceptSchema: %v\nreply: %s", err, res.Text)
	}
	if len(doc.Concepts) == 0 {
		t.Fatalf("agent returned no concepts; reply: %s", res.Text)
	}
	c := doc.Concepts[0]
	if c.ID != wantID {
		t.Errorf("concept id = %q, want %q", c.ID, wantID)
	}
	if strings.TrimSpace(c.Title) == "" || strings.TrimSpace(c.Body) == "" {
		t.Errorf("concept missing title/body: %+v", c)
	}
}
```

- [ ] **Step 2: Run it (best-effort — requires a live provider CLI)**

Run: `go test ./internal/roster/... -run TestAgency_RealAgentEmitsStructuredConcept -v`
Expected: PASS if `codex` or `claude` is on PATH and authenticated; SKIP otherwise (both are acceptable — this is the same live-or-skip contract the original test had).

Run: `go test ./internal/roster/... -short -v`
Expected: `TestAgency_RealAgentEmitsStructuredConcept` SKIPs cleanly (does not spend a turn) under `-short`; `TestRosterRegistered` (Task 3) still PASSes.

- [ ] **Step 3: Delete the old e2e test**

```bash
rm pkg/agents/agency_e2e_test.go
```

- [ ] **Step 4: Commit**

```bash
git add internal/roster/roster_e2e_test.go pkg/agents/agency_e2e_test.go
git commit -m "test: port the live fleet e2e test to internal/roster onto corral"
```

---

### Task 9: Delete pkg/agents entirely, final verification

**Files:**
- Delete: everything remaining under `pkg/agents/` (`agency.go`, `agent.go`, `provider.go`, `session.go`, `monitor.go`, `providers.go`, `doctor.go`, `cli.go`, `doc.go`, `roster.go`, `core_test.go`, `cli_model_test.go`, `agy/`, `codex/`, `claude/`, `all/`)

**Interfaces:** none — this is pure deletion + verification, no new code.

- [ ] **Step 1: Confirm zero remaining references**

Run:
```bash
grep -rl "pixkb/pkg/agents" --include=*.go .
```
Expected: only files INSIDE `pkg/agents/` itself (the package referencing its own subpackages, e.g. `all/all.go` importing `pkg/agents/agy`). If ANY file outside `pkg/agents/` still appears, STOP — a consumer was missed in Tasks 5–8; go back and fix it before deleting anything.

- [ ] **Step 2: Delete pkg/agents**

```bash
rm -r pkg/agents
```

- [ ] **Step 3: Full-repo build, vet, test, lint**

Run: `go build ./...`
Expected: clean.

Run: `go vet ./...`
Expected: clean.

Run: `go test ./... -short`
Expected: all packages pass. Note: this is the point to also run the DSN-gated integration suite for real, matching this project's established discipline of never trusting an offline-only pass for DB-touching code — spin up the throwaway test DB (`task testdb:up`), run `go test ./...` with `PIXKB_TEST_DSN` set (packages one at a time if you hit the known shared-DB cross-package race — see any existing project notes on this), then `task testdb:down`.

Run: `golangci-lint run ./...`
Expected: 0 issues.

- [ ] **Step 4: Live smoke test of the CLI surface**

Run these against a real (or throwaway) DSN to confirm the migrated CLI behaves identically to before:
```bash
go run ./cmd/pixkb agents list
go run ./cmd/pixkb agents doctor
go run ./cmd/pixkb agents hosts
go run ./cmd/pixkb agents install --dry-run --host claude
```
Expected: `agents list` prints all 13 roster agents; `agents doctor` reports a roster count of 13 and CLI-on-PATH checks; `agents install --dry-run` plans files under a path still named `.../claude/pixkb/...` (NOT `.../claude/corral/...` — this is the specific regression this migration must not introduce).

If `codex` or `claude` is on PATH and authenticated, also run:
```bash
go run ./cmd/pixkb agents run judge "test input"
```
Expected: a real response (or a clear provider error, not a Go panic or "unknown agent" — the latter would mean the roster didn't register).

- [ ] **Step 5: Update docs**

In `docs/ROADMAP.md`, find Phase 8's agent-fleet section and add a line noting the migration (e.g. under a new bullet: "Migrated `pkg/agents` onto `github.com/inovacc/corral` — same runtime, upstream-maintained; pixkb keeps only the BACEN-charter roster (`internal/roster`) and the pixkb-branded host installer (`internal/agenthost`)."). Bump `docs/ROADMAP.md`'s `<!-- rev:NNN -->` tag by 1 (read the current value fresh first).

In `docs/BACKLOG.md`, add a line under an appropriate section (or a new "Shipped" entry) recording the migration, the go.mod version bump, and the 3-provider scope decision (grok/kimi deferred). Bump its `<!-- rev:NNN -->` tag by 1 (read fresh).

- [ ] **Step 6: Commit**

```bash
git add pkg/agents docs/ROADMAP.md docs/BACKLOG.md
git commit -m "refactor: delete pkg/agents — fully migrated to github.com/inovacc/corral"
```

---

## Self-Review Notes (already applied above)

- **Spec coverage:** every table row and consumer file in the spec has a corresponding task (Task 1 = go.mod bump; Task 2 = embed relocation; Task 3 = roster; Task 4 = agenthost; Tasks 5–7 = the 10 consumer files; Task 8 = e2e test; Task 9 = deletion + verification + docs).
- **Placeholder scan:** Task 7 originally under-specified whether `ask.go`/`curate.go`/`eval.go` each need their own blank imports of `corral/all`/`internal/roster` — resolved inline (Step 4) once the single-binary blank-import semantics were worked through; only `agents.go` needs them.
- **Type consistency:** `AgentGenerator.Agency`, `AgencyFixer.Agency`, `kbmcp.Deps.Agency` all consistently retyped to `*corral.Agency` across Tasks 5–6; `agenthost.Host`/`agenthost.AgentMarkdown(a corral.Agent)` consistent between Task 4's creation and Task 7's `cmd/pixkb/agents.go` usage.
