# Domain-Aware Query Understanding Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development.

**Goal:** Feature 7 of `docs/SEARCH-CAPABILITY-SPEC.md`: replace `internal/query/expand.go`'s small, hardcoded `entityTriggers` table with a versioned, auditable domain-vocabulary file (as that table's own doc comment already anticipated: "separate from the larger, versioned vocabulary Feature 7... will add later; that feature should supersede/extend this table, not duplicate it"), add the spec's remaining named alias mappings that measure well, document (not silently drop) the ones that don't, and expose an inspect/disable surface for debugging.

**Architecture:** New embedded YAML `internal/query/domain_vocabulary.yaml` — one entry per domain-entity mapping: `stems` (word-stems, same folded-prefix matching `ExpandQuery` already does), `subquery` (the canonical rewrite), `enabled` (bool), `reason` (human-readable — either the eval evidence that justifies it being live, or the measured regression that justifies it being disabled). `internal/query/vocab.go` loads it once via `go:embed` and exposes `Vocabulary()` (full table, for inspection) and an internal `activeVocabulary()` (enabled-only, for matching). `ExpandQuery` is modified to iterate `activeVocabulary()` instead of `entityTriggers` — same stem-matching logic, same order, so the 7 existing mappings produce byte-identical subqueries and every existing `expand_test.go` test passes unmodified. Two new mappings are added and enabled (`txid`, `e2eid` — verified live against the current index); three of the spec's ten candidate mappings (`pacs.008`, `pacs.004`, `camt.054`) are added but `enabled: false`, with their `reason` field citing the exact prior measurement (commit `2e3b722`) that already found this class of trigger regresses precise top@5. A `PIXKB_DISABLE_DOMAIN_VOCAB` env var (checked once, no signature change) lets a caller skip vocabulary expansion entirely for debugging. `pixkb vocab list [--reasons]` is the inspection surface.

**Tech Stack:** Go 1.25, `internal/query`, `cmd/pixkb`, `gopkg.in/yaml.v3` (already a dependency).

## Global Constraints

- Go 1.25.0, `CGO_ENABLED=0`, pure Go.
- **Zero behavior change for the 7 migrated mappings.** `expand_test.go`'s existing tests (`TestExpandQuery_MatchesRefundEntityOnRealEvalCase`, `TestExpandQuery_MatchesWebhookEntity`, `TestExpandQuery_CapsAtMaxSubqueries`, etc.) must pass UNMODIFIED — same subquery text, same order, same cap behavior.
- The vocabulary must be versioned with the repo (`internal/query/domain_vocabulary.yaml`, committed, loaded via `go:embed` — no runtime file path, no external config to go stale).
- The vocabulary must not override user filters — `ExpandQuery` never touches `postgres.Filter`; it only produces subquery strings that flow into `MultiHybrid`'s existing per-subquery `Hybrid` calls, same as today.
- Changes must be measured against precise and fuzzy evals (`eval/tophit.sh eval/cases-precise-ids.tsv` / `eval/cases-fuzzy-ids.tsv`) before any new mapping is enabled — this plan's two new enabled mappings (`txid`, `e2eid`) were verified live against the current index during authoring (see each entry's `reason` field); re-verify at implementation time since the index may have changed.
- A mapping the spec names but that measures worse than doing nothing is not silently dropped — it is added `enabled: false` with a `reason` citing the measurement, satisfying the spec's own "documented reason to exist" escape hatch.

---

### Task 1: `internal/query/domain_vocabulary.yaml` + `vocab.go` loader

**Files:**
- Create: `internal/query/domain_vocabulary.yaml`, `internal/query/vocab.go`, `internal/query/vocab_test.go`

**Interfaces:**
- Produces: `type VocabEntry struct { Stems []string; Subquery string; Enabled bool; Reason string }`; `func Vocabulary() []VocabEntry` (full table, exported for `pixkb vocab list`); unexported `func parseVocabulary(data []byte) ([]VocabEntry, error)`, `func activeVocabulary(entries []VocabEntry) []VocabEntry`.
- Consumes: nothing new (pure YAML parsing).

- [ ] **Step 1:** Create `internal/query/domain_vocabulary.yaml`:

```yaml
# Domain vocabulary — Feature 7 of docs/SEARCH-CAPABILITY-SPEC.md ("Domain-
# Aware Query Understanding"). Curated, deterministic, auditable: each entry
# maps a set of word-stems (folded via internal/query's foldTokens —
# lowercase, diacritics stripped, tokenized on non-alnum) to a canonical
# subquery internal/query.ExpandQuery adds when a query mentions that domain
# entity. `enabled: false` entries are kept, not deleted, so this file stays
# a complete audit trail of what was tried and why it isn't live — satisfying
# the spec's "Each mapping has at least one eval case or documented reason to
# exist" acceptance criterion even for entries that didn't pass their own
# bar.
#
# Versioned with the repo (this file); loaded once via go:embed in vocab.go.
# Order is fixed (top to bottom) — ExpandQuery iterates entries in this order
# and stops once maxSubqueries is reached, same as the entityTriggers table
# this file replaces (see git history: internal/query/expand.go before this
# plan).

entries:
  - stems: ["estorn", "devolu", "refund"]
    subquery: "devolução pix refund"
    enabled: true
    reason: >
      Feature 1's original entity trigger, covers the spec's "estorno,
      devolução, refund -> Pix refund concepts and refund endpoints"
      mapping. Eval case: eval/cases-fuzzy-ids.tsv "como estornar um pix que
      recebi por engano" -> api/openapi/put-pix-e2eid-devolucao-id.md.

  - stems: ["webhook", "notific", "avis"]
    subquery: "webhook notificação pix"
    enabled: true
    reason: >
      Feature 1's original entity trigger, covers the spec's "webhook,
      notificação, aviso automático -> Pix webhook concepts" mapping. Eval
      case: internal/query/expand_test.go's
      TestExpandQuery_MatchesWebhookEntity; live query "notificar via
      webhook pix" surfaces api/openapi/put-webhook-chave.md.

  - stems: ["chave", "dict", "evp"]
    subquery: "chave DICT pix"
    enabled: true
    reason: >
      Feature 1's original entity trigger, covers the spec's "chave
      aleatória, EVP -> DICT key type concepts" mapping. Eval case:
      eval/cases-precise-ids.tsv "consultar chave dict" ->
      api/openapi/get-entries-key.md.

  - stems: ["endpoint", "api"]
    subquery: "endpoint API"
    enabled: true
    reason: >
      Feature 1's original entity trigger — a generic API-surface steer, not
      one of Feature 7's ten named spec mappings. Kept as-is; re-evaluating
      or removing it is out of scope for this plan.

  - stems: ["certific", "mtls", "icp"]
    subquery: "certificado mTLS ICP-Brasil"
    enabled: true
    reason: >
      Feature 1's original entity trigger, covers the spec's "certificado,
      mTLS, ICP-Brasil -> security/connectivity concepts" mapping. Eval
      case: eval/cases-precise-ids.tsv "requisitos de segurança mtls
      certificado" ->
      reference/bacen-pix-concepts/03-requisitos-de-seguran-a-pix-mtls-e-certificados.md.

  - stems: ["qr"]
    subquery: "QR Code Pix BR Code"
    enabled: true
    reason: >
      Feature 1's original entity trigger. Eval case:
      eval/cases-precise-ids.tsv "qr code dinâmico location" ->
      api/openapi/post-loc.md.

  - stems: ["liquida", "spi"]
    subquery: "liquidação SPI settlement"
    enabled: true
    reason: >
      Feature 1's original entity trigger, covers the spec's "liquidação,
      reservas, SPI -> settlement concepts" mapping. Eval case:
      eval/cases-precise-ids.tsv "liquidação no spi reservas" ->
      reference/spi/liquidacao-spi.md.

  - stems: ["txid"]
    subquery: "consultar cobrança por txid"
    enabled: true
    reason: >
      Feature 7 new mapping: spec's "txid, identificador da transação ->
      cobrança lookup endpoints". Verified live 2026-07-04: the subquery
      alone ranks api/openapi/get-cobr-txid.md, api/openapi/get-cob-txid.md,
      and api/openapi/get-cobv-txid.md at ranks 1-3 (a natural-language
      baseline like "buscar pagamento pelo identificador da transação" does
      NOT surface these unaided — this mapping is what fixes that via
      MultiHybrid's fusion). Eval case: eval/cases-vocab-ids.tsv. Stem is
      deliberately just "txid" (not a looser stem like "transac") to
      minimize false-positive risk, matching this table's conservative-stem
      convention.

  - stems: ["e2eid", "endtoend"]
    subquery: "pix e2eid endToEndId identificador fim a fim"
    enabled: true
    reason: >
      Feature 7 new mapping: spec's "e2eid, endToEndId, identificador fim a
      fim -> payment/refund lifecycle". Verified live 2026-07-04: the
      subquery alone ranks api/openapi/get-pix-e2eid.md and
      api/openapi/put-pix-e2eid-devolucao-id.md at ranks 1-2 (same
      unaided-baseline gap as the txid entry above). Eval case:
      eval/cases-vocab-ids.tsv.

  - stems: ["pacs"]
    subquery: "pacs.008 customer credit transfer ordem de crédito"
    enabled: false
    reason: >
      Spec candidate mapping ("pacs.008, ordem de crédito -> customer
      credit transfer message") — ALREADY TRIED as part of Feature 1's
      original pacs/camt entity triggers and REVERTED in commit 2e3b722
      (see docs/superpowers/plans/2026-07-04-multi-query-retrieval.md
      Task 6): it regressed precise top@5 96%->88% by diluting
      already-precise queries with zero fuzzy-recall benefit — these
      ISO-message queries are already precise, and the base Hybrid search
      handles them via direct lexical/semantic match without help. Kept
      here, disabled, as the documented reason this mapping is not live.
      Note: the stem "pacs" alone cannot distinguish pacs.008 from any other
      pacs.NNN message — this ambiguity is itself part of why the original
      trigger was too broad; a future re-attempt would need per-message-code
      disambiguation, not a bare "pacs" stem.

  - stems: ["pacs"]
    subquery: "pacs.004 payment return devolução entre instituições"
    enabled: false
    reason: >
      Same regression history as the pacs.008 entry above — spec's
      "pacs.004, devolução entre instituições -> payment return message"
      mapping.

  - stems: ["camt"]
    subquery: "camt.054 account notification extrato lançamento"
    enabled: false
    reason: >
      Same regression history as the pacs.008 entry above — spec's
      "camt.054, extrato, lançamento -> account notification message"
      mapping.
```

- [ ] **Step 2:** Create `internal/query/vocab.go`:

```go
package query

import (
	_ "embed"
	"fmt"

	"gopkg.in/yaml.v3"
)

//go:embed domain_vocabulary.yaml
var vocabularyYAML []byte

// VocabEntry is one domain-vocabulary mapping: a set of word-stems (folded,
// the same prefix-matching convention ExpandQuery already used for
// entityTriggers) to a canonical subquery, plus an enabled flag and a
// human-readable reason — the audit trail Feature 7 of
// docs/SEARCH-CAPABILITY-SPEC.md requires ("curated, deterministic,
// auditable"). Disabled entries are kept, not deleted, so a maintainer can
// see what was tried and why it isn't live (e.g. a measured eval
// regression) without digging through git history.
type VocabEntry struct {
	Stems    []string `yaml:"stems"`
	Subquery string   `yaml:"subquery"`
	Enabled  bool     `yaml:"enabled"`
	Reason   string   `yaml:"reason"`
}

// vocabFile is domain_vocabulary.yaml's top-level shape.
type vocabFile struct {
	Entries []VocabEntry `yaml:"entries"`
}

// parseVocabulary parses the domain-vocabulary YAML format.
func parseVocabulary(data []byte) ([]VocabEntry, error) {
	var f vocabFile
	if err := yaml.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("parse domain vocabulary: %w", err)
	}
	return f.Entries, nil
}

// vocabulary is the full table (enabled and disabled entries) loaded once
// from the embedded domain_vocabulary.yaml at package init.
var vocabulary = mustParseVocabulary(vocabularyYAML)

// mustParseVocabulary panics on a parse failure — the embedded file is
// committed source, so a parse failure here is a build-time bug, not a
// runtime condition to recover from.
func mustParseVocabulary(data []byte) []VocabEntry {
	entries, err := parseVocabulary(data)
	if err != nil {
		panic(err)
	}
	return entries
}

// Vocabulary returns the full domain-vocabulary table (enabled AND disabled
// entries), in file order — exported for `pixkb vocab list`'s inspection
// surface (spec acceptance criterion: "Users can inspect... domain
// expansion when debugging").
func Vocabulary() []VocabEntry {
	return vocabulary
}

// activeVocabulary returns only the enabled entries, in file order — what
// ExpandQuery actually matches against.
func activeVocabulary(entries []VocabEntry) []VocabEntry {
	out := make([]VocabEntry, 0, len(entries))
	for _, e := range entries {
		if e.Enabled {
			out = append(out, e)
		}
	}
	return out
}
```

- [ ] **Step 3:** Create `internal/query/vocab_test.go`:

```go
package query

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseVocabulary_ParsesEmbeddedFile(t *testing.T) {
	t.Parallel()
	entries, err := parseVocabulary(vocabularyYAML)
	require.NoError(t, err)
	require.NotEmpty(t, entries)
	for _, e := range entries {
		assert.NotEmpty(t, e.Stems, "every entry must have at least one stem")
		assert.NotEmpty(t, e.Subquery, "every entry must have a subquery")
		assert.NotEmpty(t, e.Reason, "every entry must document a reason")
	}
}

func TestActiveVocabulary_FiltersDisabled(t *testing.T) {
	t.Parallel()
	entries := []VocabEntry{
		{Stems: []string{"a"}, Subquery: "A", Enabled: true, Reason: "r"},
		{Stems: []string{"b"}, Subquery: "B", Enabled: false, Reason: "r"},
	}
	active := activeVocabulary(entries)
	require.Len(t, active, 1)
	assert.Equal(t, "A", active[0].Subquery)
}

func TestVocabulary_PacsCamtEntriesAreDisabledWithReason(t *testing.T) {
	t.Parallel()
	// Guards against silently flipping these back on without addressing the
	// documented regression history (commit 2e3b722).
	for _, want := range []string{"pacs.008", "pacs.004", "camt.054"} {
		found := false
		for _, e := range Vocabulary() {
			if !strings.Contains(e.Subquery, want) {
				continue
			}
			found = true
			assert.False(t, e.Enabled, "%s entry must stay disabled", want)
			assert.Contains(t, e.Reason, "2e3b722", "%s entry must cite the regression commit", want)
		}
		assert.True(t, found, "expected a vocabulary entry mentioning %s", want)
	}
}
```

- [ ] **Step 4:** `go test ./internal/query/... -v`, `go build ./...`, `go vet ./...`.
- [ ] **Step 5:** Commit: `git add internal/query/domain_vocabulary.yaml internal/query/vocab.go internal/query/vocab_test.go && git commit -m "feat: add versioned domain-vocabulary file and loader (Feature 7 foundation)"`.

---

### Task 2: `ExpandQuery` — replace `entityTriggers` with the vocabulary + disable env var

**Files:**
- Modify: `internal/query/expand.go`
- Test: `internal/query/expand_test.go`

**Interfaces:**
- Consumes: `activeVocabulary(Vocabulary())` from Task 1.
- Produces: no signature change to `ExpandQuery`; adds the `PIXKB_DISABLE_DOMAIN_VOCAB` env-var check.

- [ ] **Step 1:** In `internal/query/expand.go`, delete the entire `entityTriggers` var and its doc comment (lines 9-34 in the pre-plan file — the comment block starting "entityTriggers maps a fixed, ordered set..." through the closing `}` of the var), and replace `ExpandQuery`'s body to match against the vocabulary instead:

```go
package query

import (
	"os"
	"strings"
)

// maxSubqueries bounds ExpandQuery's output. Spec: "Default expansion count
// should be small, preferably 3 to 5 queries."
const maxSubqueries = 5

// ExpandQuery deterministically expands q into up to maxSubqueries retrieval
// queries: the original query verbatim, a concise domain-term rewrite
// (diacritics folded, stopwords stripped, via the same foldTokens used for
// title-boost matching), and one subquery per recognized domain entity the
// query mentions. Domain-entity subqueries come from the versioned,
// auditable table in domain_vocabulary.yaml (Feature 7 of
// docs/SEARCH-CAPABILITY-SPEC.md — see vocab.go); only `enabled: true`
// entries are matched. Duplicate subqueries (case-insensitive) are dropped.
// The original query is always present and always first, so MultiHybrid
// always has at least the equivalent of a plain single-query hybrid search
// to fall back on. Setting PIXKB_DISABLE_DOMAIN_VOCAB (any non-empty value)
// skips the vocabulary step entirely — the debugging disable switch the
// spec's Feature 7 acceptance criteria ask for ("Users can inspect or
// disable domain expansion when debugging"; see also `pixkb vocab list` for
// inspection). The vocabulary never touches postgres.Filter, so it cannot
// override a user's own filters.
func ExpandQuery(q string) []string {
	out := []string{q}
	seen := map[string]bool{strings.ToLower(strings.TrimSpace(q)): true}
	add := func(s string) bool {
		s = strings.TrimSpace(s)
		key := strings.ToLower(s)
		if s == "" || seen[key] {
			return false
		}
		seen[key] = true
		out = append(out, s)
		return len(out) >= maxSubqueries
	}

	tokens := foldTokens(q)
	tokenSet := make(map[string]bool, len(tokens))
	for _, t := range tokens {
		tokenSet[t] = true
	}

	if add(strings.Join(tokens, " ")) {
		return out
	}
	if os.Getenv("PIXKB_DISABLE_DOMAIN_VOCAB") != "" {
		return out
	}
	for _, entry := range activeVocabulary(Vocabulary()) {
		matched := false
		for token := range tokenSet {
			for _, stem := range entry.Stems {
				if strings.HasPrefix(token, stem) {
					matched = true
					break
				}
			}
			if matched {
				break
			}
		}
		if matched && add(entry.Subquery) {
			return out
		}
	}
	return out
}
```

- [ ] **Step 2:** Add to `internal/query/expand_test.go` (append; do not modify any existing test):

```go
func TestExpandQuery_MatchesTxidEntity(t *testing.T) {
	t.Parallel()
	out := ExpandQuery("buscar pagamento pelo identificador da transação txid")
	found := false
	for _, sq := range out {
		if sq == "consultar cobrança por txid" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected the txid entity subquery, got %v", out)
	}
}

func TestExpandQuery_MatchesE2eidEntity(t *testing.T) {
	t.Parallel()
	out := ExpandQuery("consultar pix pelo e2eid")
	found := false
	for _, sq := range out {
		if sq == "pix e2eid endToEndId identificador fim a fim" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected the e2eid entity subquery, got %v", out)
	}
}

func TestExpandQuery_DisabledEntriesNeverMatch(t *testing.T) {
	t.Parallel()
	// pacs.008's disabled entry's own stem ("pacs") must never fire, even
	// though the query obviously mentions it.
	out := ExpandQuery("mensagem pacs.008 ordem de crédito")
	for _, sq := range out {
		if sq == "pacs.008 customer credit transfer ordem de crédito" {
			t.Fatalf("disabled vocabulary entry must never be matched, got %v", out)
		}
	}
}

func TestExpandQuery_DisableEnvVarSkipsVocabulary(t *testing.T) {
	t.Setenv("PIXKB_DISABLE_DOMAIN_VOCAB", "1")
	out := ExpandQuery("como estornar um pix que recebi por engano")
	if len(out) > 2 {
		t.Fatalf("PIXKB_DISABLE_DOMAIN_VOCAB must suppress all vocabulary subqueries, got %v", out)
	}
	for _, sq := range out {
		if sq == "devolução pix refund" {
			t.Fatalf("PIXKB_DISABLE_DOMAIN_VOCAB must suppress the refund entity subquery, got %v", out)
		}
	}
}
```

(`TestExpandQuery_DisableEnvVarSkipsVocabulary` cannot run with `t.Parallel()` since it mutates a process-wide env var — leave it sequential, matching Go's standard `t.Setenv` restriction of not being combinable with `t.Parallel` in the same test.)

- [ ] **Step 3:** Run `go test ./internal/query/... -v` — **every pre-existing `TestExpandQuery_*` test must pass with ZERO changes to their assertions** (this is the proof the migration didn't alter behavior for the 7 existing mappings). All 4 new tests must also pass.
- [ ] **Step 4:** `go build ./...`, `go vet ./...`.
- [ ] **Step 5:** Commit: `git add internal/query/expand.go internal/query/expand_test.go && git commit -m "feat: source ExpandQuery's domain entities from the versioned vocabulary; add PIXKB_DISABLE_DOMAIN_VOCAB"`.

---

### Task 3: `pixkb vocab list` + eval case + live verification + backlog

**Files:**
- Create: `cmd/pixkb/vocab.go`, `eval/cases-vocab-ids.tsv`
- Modify: `cmd/pixkb/commands.go`, `docs/BACKLOG.md`

**Interfaces:**
- Consumes: `query.Vocabulary()` from Task 1.
- Produces: `func newVocabCmd() *cobra.Command`.

- [ ] **Step 1:** Create `cmd/pixkb/vocab.go`:

```go
package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"pixkb/internal/query"
)

// newVocabCmd is the domain-vocabulary inspection surface (Feature 7 of
// docs/SEARCH-CAPABILITY-SPEC.md): lists every entry (enabled and disabled),
// its stems and subquery, and — with --reasons — the documented reason it is
// (or isn't) live. Satisfies the spec's "Users can inspect or disable domain
// expansion when debugging" acceptance criterion (the disable half is the
// PIXKB_DISABLE_DOMAIN_VOCAB env var, surfaced here for visibility).
func newVocabCmd() *cobra.Command {
	var showReasons bool
	cmd := &cobra.Command{
		Use:   "vocab",
		Short: "Inspect the domain vocabulary (Feature 7 of docs/SEARCH-CAPABILITY-SPEC.md)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			out := cmd.OutOrStdout()
			if v := os.Getenv("PIXKB_DISABLE_DOMAIN_VOCAB"); v != "" {
				fmt.Fprintf(out, "PIXKB_DISABLE_DOMAIN_VOCAB is set (%q) — domain-vocabulary expansion is currently DISABLED for multi-query search.\n\n", v)
			}
			for _, e := range query.Vocabulary() {
				status := "enabled"
				if !e.Enabled {
					status = "disabled"
				}
				fmt.Fprintf(out, "[%-8s] stems=%v -> %q\n", status, e.Stems, e.Subquery)
				if showReasons {
					fmt.Fprintf(out, "           %s\n", strings.TrimSpace(e.Reason))
				}
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&showReasons, "reasons", false, "also print each entry's documented reason")
	return cmd
}
```

- [ ] **Step 2:** In `cmd/pixkb/commands.go`, add `newVocabCmd()` to the `attachCommands` `root.AddCommand(...)` call (append it to the existing list, do not reorder the others):

```go
	root.AddCommand(newIngestCmd(), newSearchCmd(), newReindexCmd(), newDiffCmd(), newStatsCmd(), newRelatedCmd(), newSimilarCmd(), newAgentsCmd(), newConceptCmd(), newMCPCmd(), newHygieneCmd(), newCurateCmd(), newQRCmd(), newAskCmd(), newISPBCmd(), newEvalCmd(), newVocabCmd())
```

- [ ] **Step 3:** Create `eval/cases-vocab-ids.tsv`:

```
# Domain-vocabulary new-entry eval cases (Feature 7 of
# docs/SEARCH-CAPABILITY-SPEC.md). Format matches eval/tophit.sh /
# eval/cases-precise-ids.tsv: query<TAB>id1[,id2,...]. These are the
# STANDALONE subquery texts from internal/query/domain_vocabulary.yaml's
# txid and e2eid entries (not the natural-language baseline queries, which
# the base Hybrid search alone does not rank well for — that gap is exactly
# what these vocabulary entries fix via MultiHybrid's fusion). Verified live
# 2026-07-04. Run: bash eval/tophit.sh eval/cases-vocab-ids.tsv
consultar cobrança por txid	api/openapi/get-cob-txid.md,api/openapi/get-cobr-txid.md,api/openapi/get-cobv-txid.md
pix e2eid endToEndId identificador fim a fim	api/openapi/get-pix-e2eid.md,api/openapi/put-pix-e2eid-devolucao-id.md
```

- [ ] **Step 4:** `go build ./...`, `go vet ./...`, `go test ./... -short`. If a DSN is reachable:
  - Run `go run ./cmd/pixkb vocab` and `go run ./cmd/pixkb vocab --reasons`; confirm all 12 entries print, 9 `enabled`/3 `disabled`.
  - Run `bash eval/tophit.sh eval/cases-precise-ids.tsv` and `bash eval/tophit.sh eval/cases-fuzzy-ids.tsv` — confirm no regression from before Task 1 (these should be unaffected; the 7 migrated mappings are byte-identical and the 2 new ones only fire on their own unambiguous stems).
  - Run `bash eval/tophit.sh eval/cases-vocab-ids.tsv` — confirm both new cases hit rank 1-3.
  - Run the FULL pipeline check: `go run ./cmd/pixkb search "buscar pagamento pelo identificador da transação" --mode multi --limit 20` and `go run ./cmd/pixkb search "consultar pix pelo identificador fim a fim" --mode multi --limit 20` — confirm the txid/e2eid endpoints now appear in the fused multi-query result (they did not appear in an unaided single-query search of the same text, verified during this plan's authoring). Report the actual ranks found.
  - If any of these regress or don't reproduce, do not silently adjust the vocabulary to force a pass — report the finding; a stem or subquery text may need revision, which is a legitimate mid-implementation discovery, not a plan defect to paper over.
- [ ] **Step 5:** Backlog (P2) in `docs/BACKLOG.md`:
  - The `pacs`/`camt` stem ambiguity noted in `domain_vocabulary.yaml`'s disabled entries (a bare "pacs" stem can't distinguish pacs.008 from any other pacs.NNN message) — a future re-attempt at this class of mapping needs per-message-code disambiguation (e.g. matching the literal "pacs.008" token, or a smarter multi-word stem), not a bare family-name stem, if it's ever revisited.
  - `endpoint`/`api` stem entry was migrated as-is without re-evaluating whether it still earns its place — not evaluated in this plan, out of scope.
  - Feature 8 (Search Quality Operations) remains unimplemented from `docs/SEARCH-CAPABILITY-SPEC.md` — needs its own scoped plan.
- [ ] **Step 6:** Commit: `git add cmd/pixkb/vocab.go cmd/pixkb/commands.go eval/cases-vocab-ids.tsv docs/BACKLOG.md && git commit -m "feat: add pixkb vocab inspection command and eval/cases-vocab-ids.tsv; backlog remaining Feature 7-8 scope"`.
