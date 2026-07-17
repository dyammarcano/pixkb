# RAG Answer Cache + PII/LGPD Post-Filter Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Close the two remaining `docs/BACKLOG.md` P1 "RAG follow-ups" items: (a) a deterministic, code-enforced PII/LGPD redaction pass over `Answer.Text` (today the answerer is only prompt-instructed to avoid PII — nothing enforces it), and (b) an in-memory LRU answer cache keyed by `(question-hash, KB-epoch)` so a repeated question does not re-spend a real subscription-agent turn. Both ship ON by default (safety and cost respectively) with an explicit CLI/MCP escape hatch to disable each independently for debugging.

**Architecture:** Two small, additive files in `internal/rag`, both zero-DB, zero-agent, unit-testable in isolation:
- `internal/rag/pii.go` — package-level compiled regexes for CPF/CNPJ (formatted + bare-digit)/phone/email, and `RedactPII(s string) string`, a pure string transform.
- `internal/rag/cache.go` — `AnswerCache` interface (`Get`/`Put`), `CacheKey(question string, epoch int) string` (sha256 of the normalized question + epoch), and `LRUCache`, a `container/list`-backed, mutex-guarded, fixed-capacity implementation (no new external dependency).

Both wire into the *existing* `rag.Ask` (in `adapters.go`) via two new `Options` fields each (`NoPIIFilter bool`, `Cache AnswerCache`, `Epoch int`) — `Ask`'s signature does not change, so every existing caller keeps compiling. `Ask` calls `BuildGrounding` first (retrieval is local/cheap, never the agent), then on a cache hit returns the cached `Answer` without calling `Synthesize` (skipping the agent turn); on a miss it synthesizes, redacts, and populates the cache — so a cached entry is always the already-redacted text, and cache hit vs. miss is invisible to a caller. The KB-epoch comes from `postgres.Store.Stats(ctx).LatestEpoch` (already shipped, `internal/store/postgres/stats.go:13`), read once per `ask` invocation by the CLI/MCP wiring layer (not by `rag` itself, which stays DB-free) — a new epoch after `pixkb ingest`/agent write-back naturally invalidates every prior cache entry by construction (new key), with no explicit eviction pass needed.

Redaction only ever touches `Answer.Text` — `Answer.Citations` (concept ids) are never passed through `RedactPII`, so citations/provenance are structurally unaffected regardless of what the regexes match.

**Tech Stack:** Go 1.25, stdlib only (`regexp`, `container/list`, `crypto/sha256`, `sync`) — no new `go.mod` dependency. `internal/rag`, `cmd/pixkb`, `internal/kbmcp`.

## Global Constraints

- Go 1.25.0, `CGO_ENABLED=0`, pure Go, no new dependency.
- `rag.Ask`'s signature is unchanged (`Ask(ctx, r, cs, gen, q, opts) (Answer, Grounding, error)`) — every existing call site (`cmd/pixkb/ask.go`, `internal/kbmcp/ask.go`) keeps compiling untouched by the core-package change; only the `Options{}` literal each passes needs new fields.
- **PII filter defaults ON** (`Options{}`'s zero value for `NoPIIFilter` is `false` — filtering happens): a false positive (over-redaction) is acceptable, a false negative (a leaked CPF/phone/email) is not. `--no-pii-filter` / `no_pii_filter: true` is the explicit, opt-in escape hatch for debugging — never silently.
- **Cache defaults ON at the CLI/MCP wiring layer** (both `cmd/pixkb/ask.go` and `internal/kbmcp/ask.go` construct an `*rag.LRUCache` and pass it unless `--no-cache` / `no_cache: true`), but **defaults OFF inside the `rag` package itself** (`Options{}`'s zero value for `Cache` is `nil` — `rag.Ask` skips all cache logic entirely when `Cache == nil`, byte-identical to pre-change behavior). This keeps the core package's existing zero-value tests meaningful with no cache-related side effects, while the product-level default (what an end user actually experiences) is "on".
- A cache hit must never call `Generator.Generate` — that is the entire point (skip the agent turn). Tests assert this via the existing `fakeGen.called` flag pattern (`internal/rag/answer_test.go`).
- Citations are never redacted or cache-corrupted — only `Answer.Text` passes through `RedactPII`.
- Follow existing conventions: doc comments explain *why*; plain `testing` package (no `testify`) matching `internal/rag`'s current test files — `testify` is a `go.mod` dependency used elsewhere in the repo, but not in this package, so match the local convention, not the global one.

---

### Task 1: PII/LGPD post-filter — `internal/rag/pii.go`, wired into `rag.Ask`

**Files:**
- Create: `internal/rag/pii.go`
- Create: `internal/rag/pii_test.go`
- Create: `internal/rag/adapters_test.go`
- Modify: `internal/rag/rag.go` (add `Options.NoPIIFilter`)
- Modify: `internal/rag/adapters.go` (`Ask` redacts `a.Text` before returning)
- Modify: `cmd/pixkb/ask.go` (add `--no-pii-filter` flag)
- Modify: `internal/kbmcp/ask.go` (add `no_pii_filter` input field)

**Interfaces:** Produces `func RedactPII(s string) string` (pure, no deps) and `Options.NoPIIFilter bool` (new field, zero value `false` = filter ON).

- [ ] **Step 1 — `internal/rag/pii.go`:**

```go
// Package rag (pii.go): a deterministic, regex-based PII/LGPD redaction pass
// over synthesized answer text. This exists because the answerer agent is only
// PROMPT-instructed to avoid personal data (CPF, CNPJ, phone, email) — nothing
// in code enforced that today. A false positive (redacting a non-PII digit run
// that happens to be 11 or 14 digits long) is an acceptable cost in a compliance
// context; a false negative (a leaked CPF slipping through) is not. Only ever
// applied to Answer.Text — Citations (concept ids) are never passed through
// this and so are structurally unaffected by any regex here.
package rag

import "regexp"

var (
	// Formatted forms first: their punctuation makes them unambiguous and
	// distinguishes a CPF from a CNPJ before either bare-digit fallback runs.
	reCNPJFormatted = regexp.MustCompile(`\b\d{2}\.\d{3}\.\d{3}/\d{4}-\d{2}\b`)
	reCPFFormatted  = regexp.MustCompile(`\b\d{3}\.\d{3}\.\d{3}-\d{2}\b`)
	// Bare-digit fallbacks: \b...\b anchors mean these can only match a digit
	// run of EXACTLY that length (a longer or shorter run has no internal word
	// boundary to match against), so an unformatted 14-digit CNPJ can never be
	// partially caught by the 11-digit CPF pattern.
	reCNPJBare = regexp.MustCompile(`\b\d{14}\b`)
	reCPFBare  = regexp.MustCompile(`\b\d{11}\b`)
	// Brazilian phone: optional +55 country code, optional parenthesized DDD,
	// 8 or 9 digit local number with an optional separator.
	rePhone = regexp.MustCompile(`(?:\+55\s?)?\(?\d{2}\)?[\s.-]?\d{4,5}-?\d{4}\b`)
	reEmail = regexp.MustCompile(`\b[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}\b`)
)

// RedactPII replaces CPF, CNPJ, Brazilian phone numbers, and email addresses
// in s with a labeled placeholder. Order matters: formatted CPF/CNPJ and both
// bare-digit fallbacks run BEFORE the phone pattern, so an ambiguous unformatted
// 11-digit run (which also structurally matches as a phone number) is redacted
// once as CPF, not double-tagged — once a span is replaced with a
// "[REDACTED:...]" placeholder it no longer contains digits, so no later
// pattern in this function can re-match it.
func RedactPII(s string) string {
	s = reCNPJFormatted.ReplaceAllString(s, "[REDACTED:CNPJ]")
	s = reCPFFormatted.ReplaceAllString(s, "[REDACTED:CPF]")
	s = reCNPJBare.ReplaceAllString(s, "[REDACTED:CNPJ]")
	s = reCPFBare.ReplaceAllString(s, "[REDACTED:CPF]")
	s = rePhone.ReplaceAllString(s, "[REDACTED:PHONE]")
	s = reEmail.ReplaceAllString(s, "[REDACTED:EMAIL]")
	return s
}
```

- [ ] **Step 2 — `internal/rag/pii_test.go`:**

```go
package rag

import (
	"strings"
	"testing"
)

func TestRedactPII_CPFFormatted(t *testing.T) {
	got := RedactPII("Meu CPF é 123.456.789-01, obrigado.")
	if strings.Contains(got, "123.456.789-01") || !strings.Contains(got, "[REDACTED:CPF]") {
		t.Fatalf("formatted CPF not redacted: %q", got)
	}
}

func TestRedactPII_CPFBare(t *testing.T) {
	got := RedactPII("CPF: 11122233344 registrado.")
	if strings.Contains(got, "11122233344") || !strings.Contains(got, "[REDACTED:CPF]") {
		t.Fatalf("bare CPF not redacted: %q", got)
	}
}

func TestRedactPII_CNPJFormatted(t *testing.T) {
	got := RedactPII("CNPJ 12.345.678/0001-95 ativo.")
	if strings.Contains(got, "12.345.678/0001-95") || !strings.Contains(got, "[REDACTED:CNPJ]") {
		t.Fatalf("formatted CNPJ not redacted: %q", got)
	}
}

func TestRedactPII_CNPJBare(t *testing.T) {
	got := RedactPII("CNPJ 12345678000195 ativo.")
	if strings.Contains(got, "12345678000195") || !strings.Contains(got, "[REDACTED:CNPJ]") {
		t.Fatalf("bare CNPJ not redacted: %q", got)
	}
}

func TestRedactPII_PhoneParens(t *testing.T) {
	got := RedactPII("Ligue para (11) 98765-4321 em horário comercial.")
	if strings.Contains(got, "98765-4321") || !strings.Contains(got, "[REDACTED:PHONE]") {
		t.Fatalf("phone not redacted: %q", got)
	}
}

func TestRedactPII_PhoneCountryCode(t *testing.T) {
	got := RedactPII("WhatsApp: +55 11 98765-4321")
	if strings.Contains(got, "98765-4321") || !strings.Contains(got, "[REDACTED:PHONE]") {
		t.Fatalf("phone with +55 not redacted: %q", got)
	}
}

func TestRedactPII_Email(t *testing.T) {
	got := RedactPII("Envie para contato@exemplo.com.br para suporte.")
	if strings.Contains(got, "contato@exemplo.com.br") || !strings.Contains(got, "[REDACTED:EMAIL]") {
		t.Fatalf("email not redacted: %q", got)
	}
}

func TestRedactPII_LeavesConceptIDsAndProseAlone(t *testing.T) {
	in := "Veja pix-glossary.md e api-endpoint-1.md para detalhes sobre a chave Pix."
	got := RedactPII(in)
	if got != in {
		t.Fatalf("prose/concept-id-like text must be untouched, got %q", got)
	}
}

func TestRedactPII_MultiplePIITypesInOneString(t *testing.T) {
	got := RedactPII("CPF 123.456.789-01, email contato@exemplo.com, tel (11) 91234-5678.")
	for _, want := range []string{"[REDACTED:CPF]", "[REDACTED:EMAIL]", "[REDACTED:PHONE]"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %s in %q", want, got)
		}
	}
}
```

- [ ] **Step 3 — `internal/rag/rag.go`:** add one field to `Options` (near `MinScore`):

```go
	MinScore      float64 // refuse (empty Grounding, no agent turn spent) when the top hit's score is below this (0 = disabled)
	NoPIIFilter   bool    // skip the deterministic PII/LGPD redaction pass over Answer.Text (default false = filter ON; debugging escape hatch only)
```

- [ ] **Step 4 — `internal/rag/adapters.go`:** change `Ask` to redact before returning:

```go
// Ask is the end-to-end RAG entry point: retrieve + augment, then synthesize. It
// returns the Grounding alongside the Answer so a surface can resolve each cited
// concept id back to its source_uri for display. Answer.Text is redacted for
// PII/LGPD (CPF/CNPJ/phone/email) before it is returned, unless
// Options.NoPIIFilter is set — a code-enforced backstop behind the answerer's
// prompt-level instruction to avoid personal data, since a prompt instruction is
// not a guarantee.
func Ask(ctx context.Context, r Retriever, cs ConceptSource, gen Generator, q string, opts Options) (Answer, Grounding, error) {
	g, err := BuildGrounding(ctx, r, cs, q, opts)
	if err != nil {
		return Answer{}, Grounding{}, err
	}
	a, err := Synthesize(ctx, gen, g)
	if err != nil {
		return Answer{}, g, err
	}
	if !opts.NoPIIFilter {
		a.Text = RedactPII(a.Text)
	}
	return a, g, nil
}
```

- [ ] **Step 5 — `internal/rag/adapters_test.go`** (new file — `adapters.go` has no test file today):

```go
package rag

import (
	"context"
	"strings"
	"testing"
)

func TestAsk_RedactsPIIInAnswerTextByDefault(t *testing.T) {
	r := &fakeRetriever{hits: []Hit{{ID: "a.md", Score: 1}}}
	cs := fakeSource{"a.md": concept("a.md", "A", "body", "doc:a")}
	gen := &fakeGen{reply: `{"answer":"Ligue (11) 98765-4321 ou envie CPF 123.456.789-01","citations":["a.md"],"refused":false}`}

	ans, _, err := Ask(context.Background(), r, cs, gen, "q", Options{})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(ans.Text, "98765-4321") || strings.Contains(ans.Text, "123.456.789-01") {
		t.Fatalf("PII must be redacted by default, got %q", ans.Text)
	}
	if len(ans.Citations) != 1 || ans.Citations[0] != "a.md" {
		t.Fatalf("citations must be untouched by redaction, got %v", ans.Citations)
	}
}

func TestAsk_NoPIIFilterOptOutSkipsRedaction(t *testing.T) {
	r := &fakeRetriever{hits: []Hit{{ID: "a.md", Score: 1}}}
	cs := fakeSource{"a.md": concept("a.md", "A", "body", "doc:a")}
	gen := &fakeGen{reply: `{"answer":"CPF 123.456.789-01","citations":["a.md"],"refused":false}`}

	ans, _, err := Ask(context.Background(), r, cs, gen, "q", Options{NoPIIFilter: true})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(ans.Text, "123.456.789-01") {
		t.Fatalf("NoPIIFilter must skip redaction, got %q", ans.Text)
	}
}
```

- [ ] **Step 6 — `cmd/pixkb/ask.go`:** add the escape hatch flag. Add `noPIIFilter bool` next to the other `var` flags, add `NoPIIFilter: noPIIFilter` to the `rag.Options{...}` literal, and register:

```go
	cmd.Flags().BoolVar(&noPIIFilter, "no-pii-filter", false, "disable the deterministic PII/LGPD redaction post-filter (debugging only — do not use for output shown to end users)")
```

- [ ] **Step 7 — `internal/kbmcp/ask.go`:** add `NoPIIFilter bool `json:"no_pii_filter,omitempty" jsonschema:"disable the deterministic PII/LGPD redaction post-filter (debugging only)"`` to `askIn`, and `NoPIIFilter: in.NoPIIFilter` to the `rag.Options{...}` literal in `registerAsk`.
- [ ] `go build ./...`, `go vet ./...`, `go test ./internal/rag/... -v` (all `TestRedactPII_*`, `TestAsk_*`, plus the full pre-existing suite, must pass).
- [ ] Commit: `git add internal/rag/pii.go internal/rag/pii_test.go internal/rag/adapters_test.go internal/rag/rag.go internal/rag/adapters.go cmd/pixkb/ask.go internal/kbmcp/ask.go && git commit -m "feat: add deterministic PII/LGPD redaction post-filter to rag.Ask"`

---

### Task 2: Answer cache — `internal/rag/cache.go`, wired into `rag.Ask` + CLI/MCP

**Files:**
- Create: `internal/rag/cache.go`
- Create: `internal/rag/cache_test.go`
- Modify: `internal/rag/rag.go` (add `Options.Cache`, `Options.Epoch`)
- Modify: `internal/rag/adapters.go` (`Ask` checks/populates the cache around `Synthesize`)
- Modify: `cmd/pixkb/ask.go` (construct a per-run `*rag.LRUCache`, add `--no-cache`, read the epoch via `st.Stats(ctx)`)
- Modify: `internal/kbmcp/ask.go` (a package-level, server-lifetime `*rag.LRUCache`, add `no_cache` input field, read the epoch via `d.Store.Stats(ctx)`)
- Modify: `docs/BACKLOG.md` (close out the two shipped follow-ups)

**Interfaces:** Produces `type AnswerCache interface { Get(key string) (Answer, bool); Put(key string, a Answer) }`, `func CacheKey(question string, epoch int) string`, `func NewLRUCache(capacity int) *LRUCache`, and two new `Options` fields: `Cache AnswerCache` (zero value `nil` = caching off, matches pre-change `rag` package behavior exactly), `Epoch int` (the KB epoch the answer was grounded against).

- [ ] **Step 1 — `internal/rag/cache.go`:**

```go
// Package rag (cache.go): an in-memory answer cache keyed by (normalized
// question, KB epoch), so a repeated question against an unchanged KB does not
// re-spend a real subscription-agent turn. Get/Put is an interface specifically
// so rag.Ask's caching path is unit-testable with a fake, with no dependency on
// LRUCache's own eviction semantics.
package rag

import (
	"container/list"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"
)

// AnswerCache is a Get/Put cache for synthesized answers, keyed by CacheKey's
// output. Production wires an *LRUCache; tests inject a fake.
type AnswerCache interface {
	Get(key string) (Answer, bool)
	Put(key string, a Answer)
}

// CacheKey derives a deterministic key from a question and the KB epoch it was
// answered against. Normalizing the question (lowercased, whitespace-collapsed)
// means trivial variations ("What is Pix?" vs "  what   is pix?  ") share an
// entry. Folding the epoch into the key means a KB update (a new epoch from
// `pixkb ingest` or an agent write-back) invalidates every prior answer purely
// by producing a different key — no explicit eviction pass is needed, and a
// stale answer from an old epoch can never be returned under the new epoch's
// key.
func CacheKey(question string, epoch int) string {
	norm := strings.ToLower(strings.Join(strings.Fields(question), " "))
	sum := sha256.Sum256([]byte(fmt.Sprintf("%d|%s", epoch, norm)))
	return hex.EncodeToString(sum[:])
}

// LRUCache is a fixed-capacity, in-memory, thread-safe, least-recently-used
// AnswerCache. Built on container/list (stdlib) — no new go.mod dependency.
// It holds process-lifetime only: for the MCP server (a long-running process
// serving many kb_ask calls) this is where the benefit actually accrues; for
// the CLI (one process per invocation) it is wired for interface symmetry and
// the --no-cache debugging escape hatch, but a single `pixkb ask` run cannot
// itself produce a hit.
type LRUCache struct {
	mu       sync.Mutex
	capacity int
	ll       *list.List
	items    map[string]*list.Element
}

type lruEntry struct {
	key string
	ans Answer
}

// NewLRUCache builds an LRUCache holding up to capacity entries. capacity <= 0
// is normalized to 128 rather than producing a cache that evicts on every Put.
func NewLRUCache(capacity int) *LRUCache {
	if capacity <= 0 {
		capacity = 128
	}
	return &LRUCache{capacity: capacity, ll: list.New(), items: make(map[string]*list.Element)}
}

// Get returns the cached Answer for key, if present, and promotes it to
// most-recently-used.
func (c *LRUCache) Get(key string) (Answer, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	el, ok := c.items[key]
	if !ok {
		return Answer{}, false
	}
	c.ll.MoveToFront(el)
	return el.Value.(*lruEntry).ans, true
}

// Put inserts or updates key's Answer, evicting the least-recently-used entry
// once the cache is over capacity.
func (c *LRUCache) Put(key string, a Answer) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if el, ok := c.items[key]; ok {
		el.Value.(*lruEntry).ans = a
		c.ll.MoveToFront(el)
		return
	}
	el := c.ll.PushFront(&lruEntry{key: key, ans: a})
	c.items[key] = el
	if c.ll.Len() > c.capacity {
		if oldest := c.ll.Back(); oldest != nil {
			c.ll.Remove(oldest)
			delete(c.items, oldest.Value.(*lruEntry).key)
		}
	}
}
```

- [ ] **Step 2 — `internal/rag/cache_test.go`:**

```go
package rag

import "testing"

type fakeCache struct{ m map[string]Answer }

func newFakeCache() *fakeCache             { return &fakeCache{m: map[string]Answer{}} }
func (c *fakeCache) Get(k string) (Answer, bool) { a, ok := c.m[k]; return a, ok }
func (c *fakeCache) Put(k string, a Answer)      { c.m[k] = a }

func TestCacheKey_NormalizesWhitespaceAndCase(t *testing.T) {
	a := CacheKey("What is Pix?", 5)
	b := CacheKey("  what   is   pix?  ", 5)
	if a != b {
		t.Fatalf("normalized-equivalent questions must share a cache key: %q vs %q", a, b)
	}
}

func TestCacheKey_DiffersByEpoch(t *testing.T) {
	a := CacheKey("What is Pix?", 1)
	b := CacheKey("What is Pix?", 2)
	if a == b {
		t.Fatal("different epochs must produce different cache keys")
	}
}

func TestLRUCache_GetPutRoundTrip(t *testing.T) {
	c := NewLRUCache(2)
	if _, ok := c.Get("missing"); ok {
		t.Fatal("empty cache must miss")
	}
	c.Put("k1", Answer{Text: "v1"})
	got, ok := c.Get("k1")
	if !ok || got.Text != "v1" {
		t.Fatalf("expected v1, got %+v ok=%v", got, ok)
	}
}

func TestLRUCache_EvictsLeastRecentlyUsed(t *testing.T) {
	c := NewLRUCache(2)
	c.Put("k1", Answer{Text: "v1"})
	c.Put("k2", Answer{Text: "v2"})
	c.Put("k3", Answer{Text: "v3"}) // capacity 2: evicts k1
	if _, ok := c.Get("k1"); ok {
		t.Fatal("k1 should have been evicted")
	}
	if _, ok := c.Get("k2"); !ok {
		t.Fatal("k2 should still be present")
	}
	if _, ok := c.Get("k3"); !ok {
		t.Fatal("k3 should still be present")
	}
}

func TestLRUCache_GetPromotesToMostRecentlyUsed(t *testing.T) {
	c := NewLRUCache(2)
	c.Put("k1", Answer{Text: "v1"})
	c.Put("k2", Answer{Text: "v2"})
	c.Get("k1")                     // promote k1 ahead of k2
	c.Put("k3", Answer{Text: "v3"}) // should evict k2, not k1
	if _, ok := c.Get("k1"); !ok {
		t.Fatal("k1 was recently used, should not have been evicted")
	}
	if _, ok := c.Get("k2"); ok {
		t.Fatal("k2 should have been evicted")
	}
}

func TestAsk_CacheHitSkipsGenerator(t *testing.T) {
	r := &fakeRetriever{hits: []Hit{{ID: "a.md", Score: 1}}}
	cs := fakeSource{"a.md": concept("a.md", "A", "body", "doc:a")}
	gen := &fakeGen{reply: `{"answer":"fresh","citations":["a.md"],"refused":false}`}
	cache := newFakeCache()
	cache.Put(CacheKey("q", 7), Answer{Text: "cached", Citations: []string{"a.md"}})

	ans, _, err := Ask(context.Background(), r, cs, gen, "q", Options{Cache: cache, Epoch: 7})
	if err != nil {
		t.Fatal(err)
	}
	if gen.called {
		t.Fatal("a cache hit must not spend an agent turn")
	}
	if ans.Text != "cached" {
		t.Fatalf("expected the cached answer, got %q", ans.Text)
	}
}

func TestAsk_CacheMissPopulatesCache(t *testing.T) {
	r := &fakeRetriever{hits: []Hit{{ID: "a.md", Score: 1}}}
	cs := fakeSource{"a.md": concept("a.md", "A", "body", "doc:a")}
	gen := &fakeGen{reply: `{"answer":"fresh answer","citations":["a.md"],"refused":false}`}
	cache := newFakeCache()

	ans, _, err := Ask(context.Background(), r, cs, gen, "q", Options{Cache: cache, Epoch: 3})
	if err != nil {
		t.Fatal(err)
	}
	if !gen.called {
		t.Fatal("a cache miss must still spend an agent turn")
	}
	cached, ok := cache.Get(CacheKey("q", 3))
	if !ok || cached.Text != ans.Text {
		t.Fatalf("cache must be populated after a miss, got %+v ok=%v", cached, ok)
	}
}

func TestAsk_DifferentEpochBypassesStaleCache(t *testing.T) {
	r := &fakeRetriever{hits: []Hit{{ID: "a.md", Score: 1}}}
	cs := fakeSource{"a.md": concept("a.md", "A", "body", "doc:a")}
	gen := &fakeGen{reply: `{"answer":"fresh","citations":["a.md"],"refused":false}`}
	cache := newFakeCache()
	cache.Put(CacheKey("q", 1), Answer{Text: "stale from epoch 1"})

	ans, _, err := Ask(context.Background(), r, cs, gen, "q", Options{Cache: cache, Epoch: 2})
	if err != nil {
		t.Fatal(err)
	}
	if !gen.called || ans.Text != "fresh" {
		t.Fatalf("a new epoch must not reuse a stale epoch's cache entry, got %+v called=%v", ans, gen.called)
	}
}

func TestAsk_NilCacheIsPreChangeBehavior(t *testing.T) {
	r := &fakeRetriever{hits: []Hit{{ID: "a.md", Score: 1}}}
	cs := fakeSource{"a.md": concept("a.md", "A", "body", "doc:a")}
	gen := &fakeGen{reply: `{"answer":"fresh","citations":["a.md"],"refused":false}`}

	ans, _, err := Ask(context.Background(), r, cs, gen, "q", Options{}) // Cache left nil
	if err != nil {
		t.Fatal(err)
	}
	if !gen.called || ans.Text != "fresh" {
		t.Fatalf("Options{} (Cache==nil) must behave exactly like caching didn't exist, got %+v called=%v", ans, gen.called)
	}
}
```

Add `"context"` to this file's imports (needed by the `Ask`-calling tests above).

- [ ] **Step 3 — `internal/rag/rag.go`:** add two fields to `Options` (near `NoPIIFilter` from Task 1):

```go
	NoPIIFilter   bool        // skip the deterministic PII/LGPD redaction pass over Answer.Text (default false = filter ON; debugging escape hatch only)
	Cache         AnswerCache // when set, Ask checks/populates it keyed by CacheKey(q, Epoch) to skip re-spending an agent turn on a repeated question (nil = no caching, the pre-change default)
	Epoch         int         // the KB epoch this question is being answered against (see postgres.Store.Stats().LatestEpoch); only consulted when Cache is set
```

- [ ] **Step 4 — `internal/rag/adapters.go`:** layer caching around `Synthesize` in `Ask` (builds on Task 1's version):

```go
// Ask is the end-to-end RAG entry point: retrieve + augment, then synthesize. It
// returns the Grounding alongside the Answer so a surface can resolve each cited
// concept id back to its source_uri for display. When Options.Cache is set, a
// hit for CacheKey(q, Options.Epoch) short-circuits Synthesize entirely — the
// whole point being to skip a real subscription-agent turn on a repeated
// question. Answer.Text is redacted for PII/LGPD before being cached, so a
// cache hit and a cache miss return identically-redacted text. Grounding is
// always rebuilt (retrieval is local and cheap, never the agent), even on a
// cache hit, so citation source_uri resolution keeps working.
func Ask(ctx context.Context, r Retriever, cs ConceptSource, gen Generator, q string, opts Options) (Answer, Grounding, error) {
	g, err := BuildGrounding(ctx, r, cs, q, opts)
	if err != nil {
		return Answer{}, Grounding{}, err
	}

	var key string
	if opts.Cache != nil {
		key = CacheKey(q, opts.Epoch)
		if a, ok := opts.Cache.Get(key); ok {
			return a, g, nil
		}
	}

	a, err := Synthesize(ctx, gen, g)
	if err != nil {
		return Answer{}, g, err
	}
	if !opts.NoPIIFilter {
		a.Text = RedactPII(a.Text)
	}
	if opts.Cache != nil {
		opts.Cache.Put(key, a)
	}
	return a, g, nil
}
```

- [ ] **Step 5 — `cmd/pixkb/ask.go`:** read the epoch, construct a per-run cache, add `--no-cache`.

  Add `noCache bool` next to the other flag vars. After `st, err := openStore(ctx, cfg)` / `defer st.Close()`, add:

```go
			stats, err := st.Stats(ctx)
			if err != nil {
				return err
			}
			var cache rag.AnswerCache
			if !noCache {
				cache = rag.NewLRUCache(128)
			}
```

  Add `Cache: cache, Epoch: stats.LatestEpoch,` to the `rag.Options{...}` literal (alongside `NoPIIFilter` from Task 1). Register the flag:

```go
	cmd.Flags().BoolVar(&noCache, "no-cache", false, "disable the in-memory answer cache (debugging / force a fresh agent turn)")
```

- [ ] **Step 6 — `internal/kbmcp/ask.go`:** a server-lifetime cache shared across every `kb_ask` call (this is where the feature actually pays off — the MCP server is a long-running process, unlike the CLI). Add a package-level var and read the epoch inside the handler:

```go
// askCache is shared across every kb_ask call for this server's lifetime — the
// CLI gets a fresh cache per invocation (no reuse possible there), but the MCP
// server is long-running, so this is the path that actually skips repeated
// agent turns in practice.
var askCache = rag.NewLRUCache(256)
```

  Add `NoCache bool `json:"no_cache,omitempty" jsonschema:"bypass the answer cache and force a fresh agent turn"`` to `askIn`. In `registerAsk`'s handler, before calling `rag.Ask`:

```go
		stats, err := d.Store.Stats(ctx)
		if err != nil {
			return nil, askOut{}, err
		}
		cache := rag.AnswerCache(askCache)
		if in.NoCache {
			cache = nil
		}
```

  Add `Cache: cache, Epoch: stats.LatestEpoch,` to the `rag.Options{...}` literal (alongside `NoPIIFilter` from Task 1).

- [ ] **Step 7 — `docs/BACKLOG.md`:** both follow-ups below are now shipped; only (a) remains open. Replace the P1 bullet:

  Old:
  ```
  - **RAG follow-ups (optional polish).** The core RAG layer is shipped; these are
    nice-to-haves, not blockers: (a) wire the now-shipped `query.MultiHybrid`
    (below) into `rag.Retriever`/`HybridRetriever` for grounding diversity on
    broad questions — the primitive exists, RAG just doesn't call it yet; (b) an
    answer cache keyed by (question-hash, KB-epoch) to avoid re-spending a turn on a
    repeated question; (c) a deterministic PII/LGPD post-filter on the answer (today
    it is prompt-level only). Gate any change on `eval/run-rag-judge.sh`.
  ```
  New:
  ```
  - **RAG follow-up (optional polish).** The core RAG layer is shipped; this is a
    nice-to-have, not a blocker: wire the now-shipped `query.MultiHybrid` (below)
    into `rag.Retriever`/`HybridRetriever` for grounding diversity on broad
    questions — the primitive exists, RAG just doesn't call it yet. Gate any
    change on `eval/run-rag-judge.sh`.
  ```
  Add to Shipped (create/extend that section if one already lists RAG items): `answer cache keyed by (question-hash, KB-epoch)` and `deterministic PII/LGPD post-filter on Answer.Text` — both default-on with `--no-cache`/`--no-pii-filter` (CLI) and `no_cache`/`no_pii_filter` (MCP) escape hatches.
  Bump `<!-- rev:046 -->` to `<!-- rev:047 -->`.

- [ ] `go build ./...`, `go vet ./...`, `go test ./internal/rag/... -v` (every `TestCacheKey_*`, `TestLRUCache_*`, `TestAsk_*` from both tasks, plus the full pre-existing suite, must pass).
- [ ] Manual smoke test if a live DB/agent is available: `pixkb ask "o que é Pix?"` twice in a row within the same process is not observable from the CLI (fresh cache per run) — instead verify via the MCP server: start it, call `kb_ask` twice with the same question, confirm the second call's latency drops sharply (no agent turn) and its answer text is identical to the first. Then run `eval/run-rag-judge.sh` per the backlog item's existing gate to confirm the redaction pass doesn't regress answer quality.
- [ ] Commit: `git add internal/rag/cache.go internal/rag/cache_test.go internal/rag/rag.go internal/rag/adapters.go cmd/pixkb/ask.go internal/kbmcp/ask.go docs/BACKLOG.md && git commit -m "feat: add in-memory answer cache keyed by (question, KB-epoch) to rag.Ask"`
