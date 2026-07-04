# Search Evaluation Expansion Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development.

**Goal:** Feature 6 of `docs/SEARCH-CAPABILITY-SPEC.md`: add deterministic evaluation gates for the retrieval features shipped in Features 1-5 (multi-query coverage, concept-similarity expected-neighbors, out-of-domain rejection, search-explanation consistency, as-of filter correctness, RAG grounding type diversity) via a new `pixkb eval` command surface — satisfying the spec's own API Surface Requirement ("`search eval` or equivalent command surface for deterministic retrieval gates").

**Architecture:** New package `internal/evalkit`: TSV case loaders (reusing `eval/cases-precise-ids.tsv`'s existing `query<TAB>id1,id2,...` format where possible) and pure, DB-free metric functions (`Coverage` — how many of a required-id set appear anywhere in a result list; `BestRank` — lowest rank among acceptable ids, mirroring `eval/tophit.sh`'s own math). Six runner functions call the existing retrieval entry points directly and in-process — `query.MultiHybrid`, `similar.Similar`, `query.HybridExplain`, `query.Hybrid`, `rag.Ask` — no new ranking math, no second implementation of anything Features 1-5 already built. `cmd/pixkb/eval.go` adds `pixkb eval {multi,similar,ood,explain,asof,rag-diversity}` subcommands that load a case file, call the matching runner, and print a plain-text report (same style as `eval/tophit.sh`'s report — numbers, not a hard pass/fail exit code, since these are measurement tools like `tophit.sh`, not CI gates). New case files are curated by actually running the current index and recording what it does today (matching `eval/cases-precise-ids.tsv`'s own documented provenance) — every id and expected outcome in this plan was verified against the live KB before being written down.

**Tech Stack:** Go 1.25, `internal/evalkit` (new), `cmd/pixkb`, reuses `internal/query`, `internal/similar`, `internal/rag`, `internal/store/postgres`.

## Global Constraints

- Go 1.25.0, `CGO_ENABLED=0`, pure Go.
- No new ranking math. Every runner calls an existing Features 1-5 entry point (`query.Hybrid`, `query.MultiHybrid`, `query.HybridExplain`, `similar.Similar`, `rag.Ask`) verbatim — `internal/evalkit` only measures and reports.
- These are measurement tools, not CI gates: runners return structured results and the CLI prints a report; none of them `os.Exit(1)` on a "bad" number. This matches `eval/tophit.sh`'s existing convention (it reports `top@1`/`top@5`/`MRR`, it does not fail the shell). A human or a later plan decides what number is acceptable.
- Case files use the simplest format that reuses an existing convention where one already fits (`query<TAB>id1,id2,...` from `eval/tophit.sh`) and introduces a new one only where the shape genuinely differs (similarity needs a mode column; OOD needs no id column at all).
- Every id and query used in a case file in this plan has been verified against the live KB (not invented) — see the inline provenance comment on each new case file.

---

### Task 1: `internal/evalkit` — case loaders, pure metrics, and the `rag.Chunk.Type` prerequisite

**Files:**
- Create: `internal/evalkit/cases.go`, `internal/evalkit/metrics.go`, `internal/evalkit/cases_test.go`, `internal/evalkit/metrics_test.go`
- Modify: `internal/rag/rag.go`

**Interfaces:**
- Produces: `type PairCase struct { Query string; WantIDs []string }`; `func LoadPairCases(path string) ([]PairCase, error)`; `type SimilarCase struct { ConceptID, Mode string; WantIDs []string }`; `func LoadSimilarCases(path string) ([]SimilarCase, error)`; `func LoadQueries(path string) ([]string, error)`; `func Coverage(hits []postgres.Hit, wantIDs []string) (found, total int)`; `func BestRank(hits []postgres.Hit, wantIDs []string) int`; `func ForbiddenIDs(caseSets ...[]PairCase) map[string]bool`; `Chunk.Type string` (new field on `rag.Chunk`).
- Consumes: `postgres.Hit` (`internal/store/postgres/search.go:19`).

- [ ] **Step 1 (rag prerequisite):** In `internal/rag/rag.go`, add `Type` to `Chunk` and populate it in `BuildGrounding` — a small additive change (mirrors Feature 5's `Hit.Type`) needed so Task 6's RAG-diversity runner can measure the concept-type diversity of what got cited, without which "type diversity for RAG grounding" (the spec's own Feature 6 metric) is unmeasurable. Change:

```go
// Chunk is one grounding fragment, tagged for citation.
type Chunk struct {
	ID        string
	Title     string
	Type      string
	SourceURI string
	Body      string
}
```

and the one line that constructs a `Chunk` in `BuildGrounding`:

```go
		g.Chunks = append(g.Chunks, Chunk{ID: id, Title: c.Title, Type: c.Type, SourceURI: c.SourceURI, Body: body})
```

(`c` is the `okf.Concept` already read two lines above via `cs.Concept(ctx, id)` — `okf.Concept.Type` already exists, `internal/okf/concept.go:11` — this is purely additive, zero behavior change to `Render()` or anything else.) Run `go test ./internal/rag/... -v` to confirm all pre-existing tests still pass unmodified (Chunk gained a field nothing reads yet outside this change).

- [ ] **Step 2:** Create `internal/evalkit/cases.go`:

```go
// Package evalkit provides deterministic, in-process evaluation runners for
// pixkb's retrieval surfaces (multi-query, similarity, RAG, as-of filtering,
// search explanation, out-of-domain rejection) — Feature 6 of
// docs/SEARCH-CAPABILITY-SPEC.md. Every runner calls an existing retrieval
// entry point (query.Hybrid, query.MultiHybrid, query.HybridExplain,
// similar.Similar, rag.Ask) directly; this package only loads case files,
// measures results, and reports numbers. It never re-implements ranking.
package evalkit

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

// PairCase is one (query, acceptable-ids) case — the same shape
// eval/tophit.sh's case files already use, reused here so
// eval/cases-precise-ids.tsv and eval/cases-fuzzy-ids.tsv can be read by both
// the bash harness and this package without a format change.
type PairCase struct {
	Query   string
	WantIDs []string
}

// LoadPairCases parses the "query<TAB>id1[,id2,...]" TSV format: comments
// (lines starting with '#') and blank lines are skipped, matching
// eval/tophit.sh's existing convention exactly.
func LoadPairCases(path string) ([]PairCase, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()

	var out []PairCase
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimRight(sc.Text(), "\r\n")
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "\t", 2)
		if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(parts[1]) == "" {
			continue
		}
		ids := strings.Split(parts[1], ",")
		for i := range ids {
			ids[i] = strings.TrimSpace(ids[i])
		}
		out = append(out, PairCase{Query: parts[0], WantIDs: ids})
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("scan %s: %w", path, err)
	}
	return out, nil
}

// SimilarCase is one (seed concept id, similarity mode, acceptable neighbour
// ids) case for eval/cases-similar-ids.tsv.
type SimilarCase struct {
	ConceptID string
	Mode      string
	WantIDs   []string
}

// LoadSimilarCases parses the "concept-id<TAB>mode<TAB>id1[,id2,...]" TSV
// format. Comments and blank lines are skipped, same convention as
// LoadPairCases.
func LoadSimilarCases(path string) ([]SimilarCase, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()

	var out []SimilarCase
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimRight(sc.Text(), "\r\n")
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "\t", 3)
		if len(parts) != 3 {
			continue
		}
		id, mode, idsField := strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1]), strings.TrimSpace(parts[2])
		if id == "" || mode == "" || idsField == "" {
			continue
		}
		ids := strings.Split(idsField, ",")
		for i := range ids {
			ids[i] = strings.TrimSpace(ids[i])
		}
		out = append(out, SimilarCase{ConceptID: id, Mode: mode, WantIDs: ids})
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("scan %s: %w", path, err)
	}
	return out, nil
}

// LoadQueries parses a bare one-query-per-line file (comments and blanks
// skipped) — used for eval/cases-ood.tsv, which has no expected-id column at
// all (the whole point of an out-of-domain case is that nothing should
// match).
func LoadQueries(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()

	var out []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		out = append(out, line)
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("scan %s: %w", path, err)
	}
	return out, nil
}

// ForbiddenIDs flattens one or more PairCase sets into a single id set — used
// by the OOD runner as its "these ids must NOT appear" list: the union of
// every normative concept id the precise/fuzzy suites already trust, so an
// out-of-domain query that returns institutional filler still passes (that's
// tolerable noise) while one that confidently returns a specific Pix
// procedure does not (that's the "confident noise" the spec's Ranking
// Principles call worse than silence).
func ForbiddenIDs(caseSets ...[]PairCase) map[string]bool {
	out := map[string]bool{}
	for _, cases := range caseSets {
		for _, c := range cases {
			for _, id := range c.WantIDs {
				out[id] = true
			}
		}
	}
	return out
}
```

- [ ] **Step 3:** Create `internal/evalkit/metrics.go`:

```go
package evalkit

import "pixkb/internal/store/postgres"

// Coverage counts how many of wantIDs appear anywhere in hits — the
// "required-id coverage for multi-query cases" metric
// docs/SEARCH-CAPABILITY-SPEC.md Feature 6 asks for: a multi-intent query
// should surface evidence for EACH intent somewhere in the fused result set,
// not just the single best-ranked one (that's BestRank's job).
func Coverage(hits []postgres.Hit, wantIDs []string) (found, total int) {
	present := make(map[string]bool, len(hits))
	for _, h := range hits {
		present[h.ID] = true
	}
	for _, id := range wantIDs {
		if present[id] {
			found++
		}
	}
	return found, len(wantIDs)
}

// BestRank returns the lowest Rank among hits whose ID is in wantIDs, or 0 if
// none match — the same "best rank across acceptable ids" semantics
// eval/tophit.sh already implements in awk, reimplemented natively here for
// evalkit's in-process runners (similarity, explain-consistency) that have
// direct access to Hit.Rank instead of parsing CLI text output.
func BestRank(hits []postgres.Hit, wantIDs []string) int {
	want := make(map[string]bool, len(wantIDs))
	for _, id := range wantIDs {
		want[id] = true
	}
	best := 0
	for _, h := range hits {
		if want[h.ID] && (best == 0 || h.Rank < best) {
			best = h.Rank
		}
	}
	return best
}

// ForbiddenPresent returns the subset of forbidden that actually appears in
// hits — the "forbidden-id absence" check for the OOD runner. An empty
// result means the gate passed (nothing forbidden leaked through).
func ForbiddenPresent(hits []postgres.Hit, forbidden map[string]bool) []string {
	var out []string
	for _, h := range hits {
		if forbidden[h.ID] {
			out = append(out, h.ID)
		}
	}
	return out
}
```

- [ ] **Step 4:** Create `internal/evalkit/cases_test.go` (unit tests, no DB — uses `t.TempDir()` + hand-written fixture files):

```go
package evalkit

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeFixture(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "cases.tsv")
	require.NoError(t, os.WriteFile(p, []byte(content), 0o644))
	return p
}

func TestLoadPairCases_SkipsCommentsAndBlanks(t *testing.T) {
	p := writeFixture(t, "# comment\n\ncriar cobrança\tapi/openapi/post-cob.md\nconsultar\ta.md,b.md\n")
	cases, err := LoadPairCases(p)
	require.NoError(t, err)
	require.Len(t, cases, 2)
	assert.Equal(t, "criar cobrança", cases[0].Query)
	assert.Equal(t, []string{"api/openapi/post-cob.md"}, cases[0].WantIDs)
	assert.Equal(t, []string{"a.md", "b.md"}, cases[1].WantIDs)
}

func TestLoadSimilarCases_ParsesThreeColumns(t *testing.T) {
	p := writeFixture(t, "# comment\napi/openapi/post-cob.md\thybrid\tapi/openapi/get-cob-txid.md\n")
	cases, err := LoadSimilarCases(p)
	require.NoError(t, err)
	require.Len(t, cases, 1)
	assert.Equal(t, "api/openapi/post-cob.md", cases[0].ConceptID)
	assert.Equal(t, "hybrid", cases[0].Mode)
	assert.Equal(t, []string{"api/openapi/get-cob-txid.md"}, cases[0].WantIDs)
}

func TestLoadQueries_SkipsCommentsAndBlanks(t *testing.T) {
	p := writeFixture(t, "# ood\nqual a previsão do tempo?\n\nreceita de bolo\n")
	qs, err := LoadQueries(p)
	require.NoError(t, err)
	assert.Equal(t, []string{"qual a previsão do tempo?", "receita de bolo"}, qs)
}

func TestForbiddenIDs_UnionsMultipleSets(t *testing.T) {
	setA := []PairCase{{Query: "q1", WantIDs: []string{"a.md", "b.md"}}}
	setB := []PairCase{{Query: "q2", WantIDs: []string{"b.md", "c.md"}}}
	forbidden := ForbiddenIDs(setA, setB)
	assert.True(t, forbidden["a.md"])
	assert.True(t, forbidden["b.md"])
	assert.True(t, forbidden["c.md"])
	assert.Len(t, forbidden, 3)
}
```

- [ ] **Step 5:** Create `internal/evalkit/metrics_test.go`:

```go
package evalkit

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"pixkb/internal/store/postgres"
)

func TestCoverage_CountsPresentIDs(t *testing.T) {
	hits := []postgres.Hit{{ID: "a.md"}, {ID: "b.md"}}
	found, total := Coverage(hits, []string{"a.md", "z.md"})
	assert.Equal(t, 1, found)
	assert.Equal(t, 2, total)
}

func TestBestRank_ReturnsLowestMatchingRank(t *testing.T) {
	hits := []postgres.Hit{{ID: "a.md", Rank: 3}, {ID: "b.md", Rank: 1}}
	assert.Equal(t, 1, BestRank(hits, []string{"a.md", "b.md"}))
	assert.Equal(t, 0, BestRank(hits, []string{"z.md"}))
}

func TestForbiddenPresent_ReturnsOnlyForbiddenHits(t *testing.T) {
	hits := []postgres.Hit{{ID: "a.md"}, {ID: "b.md"}}
	forbidden := map[string]bool{"a.md": true}
	assert.Equal(t, []string{"a.md"}, ForbiddenPresent(hits, forbidden))
}
```

- [ ] **Step 6:** `go test ./internal/evalkit/... -v`, `go test ./internal/rag/... -v` (confirm Step 1's `Chunk.Type` addition didn't break anything), `go build ./...`, `go vet ./...`.
- [ ] **Step 7:** Commit: `git add internal/rag/rag.go internal/evalkit/cases.go internal/evalkit/metrics.go internal/evalkit/cases_test.go internal/evalkit/metrics_test.go && git commit -m "feat: add internal/evalkit case loaders + metrics, rag.Chunk.Type"`.

---

### Task 2: Multi-query coverage runner + `pixkb eval multi` + `eval/cases-multi-ids.tsv`

**Files:**
- Create: `internal/evalkit/runners.go`, `eval/cases-multi-ids.tsv`
- Modify: `cmd/pixkb/eval.go` (new file — created here, extended by later tasks)

**Interfaces:**
- Produces: `type CoverageResult struct { Case PairCase; Found, Total int }`; `func RunMultiCoverage(ctx context.Context, s query.Searcher, emb embed.Embedder, cases []PairCase, limit int) ([]CoverageResult, error)`; `func newEvalCmd() *cobra.Command`.
- Consumes: `query.MultiHybrid` (`internal/query/multi.go:51`), `query.Hits` (`internal/query/multi.go:29`), `query.Searcher` (`internal/query/hybrid.go:109`), `embed.Embedder` (`internal/embed/embedder.go:6`), `Coverage` from Task 1, `openStore`/`newEmbedder`/`loadConfig` (existing `cmd/pixkb` helpers used by every other command in this package).

- [ ] **Step 1:** Create `internal/evalkit/runners.go` with the first runner:

```go
package evalkit

import (
	"context"
	"fmt"

	"pixkb/internal/embed"
	"pixkb/internal/query"
	"pixkb/internal/store/postgres"
)

// CoverageResult is one case's outcome from RunMultiCoverage: how many of the
// case's required ids the fused multi-query result set actually covered.
type CoverageResult struct {
	Case  PairCase
	Found int
	Total int
}

// RunMultiCoverage runs query.MultiHybrid (unmodified — Feature 1's fusion,
// not a reimplementation) for each case and measures required-id coverage:
// did the fused result set surface evidence for EVERY intent in the
// combined query, not just the best-ranked one. Reports a number per case;
// does not fail on a low score (see plan's Global Constraints — this is a
// measurement tool, like eval/tophit.sh, not a CI gate).
func RunMultiCoverage(ctx context.Context, s query.Searcher, emb embed.Embedder, cases []PairCase, limit int) ([]CoverageResult, error) {
	out := make([]CoverageResult, 0, len(cases))
	for _, c := range cases {
		f := postgres.Filter{Limit: limit}
		mh, err := query.MultiHybrid(ctx, s, emb, c.Query, f)
		if err != nil {
			return nil, fmt.Errorf("multi-hybrid %q: %w", c.Query, err)
		}
		found, total := Coverage(query.Hits(mh), c.WantIDs)
		out = append(out, CoverageResult{Case: c, Found: found, Total: total})
	}
	return out, nil
}
```

- [ ] **Step 2:** Create `eval/cases-multi-ids.tsv` — every query/id pair below was run live against the current index (`pixkb search "<query>" --mode multi --limit 20`) before being written down:

```
# Multi-intent coverage cases (eval/evalkit RunMultiCoverage, `pixkb eval multi`).
# Format: query<TAB>id1,id2,...   — ALL listed ids are required (coverage, not
# best-rank — see eval/cases-precise-ids.tsv / eval/tophit.sh for that).
# Each case combines two of Feature 1's named entity triggers (see
# internal/query/expand.go's entityTriggers) in one query, so MultiHybrid's
# per-entity subqueries both fire. Verified live at limit=20 on 2026-07-04:
# full coverage (2/2) for the three cases below.
consultar chave dict e verificar requisitos de certificado mtls para conexão	reference/bacen-flows/03-dict-key-resolution.md,reference/bacen-pix-concepts/03-requisitos-de-seguran-a-pix-mtls-e-certificados.md
gerar qr code dinâmico para cobrança com vencimento	reference/bacen-pix-concepts/02-qr-code-est-tico-pix-br-code-emv-mpm.md,api/openapi/get-cobv-txid.md
liquidação no spi e requisitos de certificado mtls	reference/bacen-pix-concepts/03-requisitos-de-seguran-a-pix-mtls-e-certificados.md,reference/spi/liquidacao-spi.md

# Known partial-coverage case (documented baseline, not a target to force to
# 2/2 by re-tuning ranking — ADR 0002 forbids that). At limit=20 only the
# webhook intent is covered; the refund intent's own subquery ranks its
# concepts well in isolation (verified: `pixkb search "devolução pix refund"`
# puts api/openapi/put-pix-e2eid-devolucao-id.md at rank 4) but the RRF fusion
# across all 4 subqueries pushes it to rank 21 (verified at limit=50). A
# coverage regression BELOW today's 1/2 on this case is real signal; getting
# it to 2/2 needs fusion-weighting work, backlogged (see Task 6's backlog).
como estornar um pix recebido por engano e configurar webhook para notificação	api/openapi/put-webhook-chave.md,api/openapi/put-pix-e2eid-devolucao-id.md
```

- [ ] **Step 3:** Create `cmd/pixkb/eval.go`:

```go
package main

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"pixkb/internal/evalkit"
)

// newEvalCmd is the deterministic-retrieval-gate surface Feature 6 of
// docs/SEARCH-CAPABILITY-SPEC.md asks for ("search eval or equivalent
// command surface"). Each subcommand loads a case file, runs the matching
// evalkit runner against the live KB, and prints a plain-text report — same
// spirit as eval/tophit.sh: numbers for a human to compare before/after a
// ranking change, not a pass/fail exit code.
func newEvalCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "eval",
		Short: "Deterministic retrieval evaluation gates (Feature 6 of docs/SEARCH-CAPABILITY-SPEC.md)",
	}
	root.AddCommand(newEvalMultiCmd())
	return root
}

func newEvalMultiCmd() *cobra.Command {
	var dsn, file string
	var limit int
	cmd := &cobra.Command{
		Use:   "multi",
		Short: "Required-id coverage for multi-intent queries (query.MultiHybrid)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg := loadConfig()
			if dsn != "" {
				cfg.DSN = dsn
			}
			ctx := cmd.Context()
			st, err := openStore(ctx, cfg)
			if err != nil {
				return err
			}
			defer st.Close()
			emb, err := newEmbedder(cfg)
			if err != nil {
				return err
			}
			cases, err := evalkit.LoadPairCases(file)
			if err != nil {
				return err
			}
			results, err := evalkit.RunMultiCoverage(ctx, st, emb, cases, limit)
			if err != nil {
				return err
			}
			return printCoverageReport(cmd.OutOrStdout(), results)
		},
	}
	cmd.Flags().StringVar(&dsn, "dsn", "", "Postgres DSN")
	cmd.Flags().StringVar(&file, "file", "eval/cases-multi-ids.tsv", "coverage case file (query<TAB>id1,id2,...)")
	cmd.Flags().IntVar(&limit, "limit", 20, "max results per fused multi-query search")
	return cmd
}

// printCoverageReport writes one line per case plus an aggregate
// required-id-coverage percentage — the metric docs/SEARCH-CAPABILITY-SPEC.md
// Feature 6 names explicitly.
func printCoverageReport(w io.Writer, results []evalkit.CoverageResult) error {
	var foundSum, totalSum int
	for _, r := range results {
		fmt.Fprintf(w, "%d/%d  %.60s\n", r.Found, r.Total, r.Case.Query)
		foundSum += r.Found
		totalSum += r.Total
	}
	fmt.Fprintln(w, "----")
	if totalSum == 0 {
		fmt.Fprintln(w, "no cases")
		return nil
	}
	fmt.Fprintf(w, "cases=%d  required-id coverage=%d/%d (%.0f%%)\n",
		len(results), foundSum, totalSum, 100*float64(foundSum)/float64(totalSum))
	return nil
}
```

- [ ] **Step 4:** In `cmd/pixkb/commands.go`, add `newEvalCmd()` to the `attachCommands` `root.AddCommand(...)` call (append it to the existing list, do not reorder the others):

```go
	root.AddCommand(newIngestCmd(), newSearchCmd(), newReindexCmd(), newDiffCmd(), newStatsCmd(), newRelatedCmd(), newSimilarCmd(), newAgentsCmd(), newConceptCmd(), newMCPCmd(), newHygieneCmd(), newCurateCmd(), newQRCmd(), newAskCmd(), newISPBCmd(), newEvalCmd())
```

- [ ] **Step 5:** `go build ./...`, `go vet ./...`, `go test ./internal/evalkit/... -v`. If a DSN is reachable (`PIXKB_DSN` / `.env`), manually run `go run ./cmd/pixkb eval multi` and confirm the three curated cases report `2/2` and the fourth reports `1/2`, matching this task's documented live verification — note in the report if the live numbers differ (that would mean the index changed since this plan was written, not that the code is wrong).
- [ ] **Step 6:** Commit: `git add internal/evalkit/runners.go eval/cases-multi-ids.tsv cmd/pixkb/eval.go cmd/pixkb/commands.go && git commit -m "feat: add pixkb eval multi (required-id coverage for multi-query retrieval)"`.

---

### Task 3: Similarity per-family runner + `pixkb eval similar` + `eval/cases-similar-ids.tsv`

**Files:**
- Modify: `internal/evalkit/runners.go`, `cmd/pixkb/eval.go`
- Create: `eval/cases-similar-ids.tsv`

**Interfaces:**
- Produces: `type RankResult struct { Label string; Rank int }`; `func RunSimilarFamily(ctx context.Context, s similar.Store, emb embed.Embedder, bundleDir string, cases []SimilarCase, limit int) ([]RankResult, error)`; `func newEvalSimilarCmd() *cobra.Command`.
- Consumes: `similar.Similar` (`internal/similar/dispatch.go:32`), `similar.Store`/`similar.Options` (`internal/similar/similar.go:36`, `internal/similar/dispatch.go:23`), `BestRank` from Task 1.

- [ ] **Step 1:** Append to `internal/evalkit/runners.go`:

```go
// RankResult is one case's outcome from a rank-based runner (similarity,
// explain-consistency): the best rank among acceptable ids, or 0 if none
// appeared within the requested limit.
type RankResult struct {
	Label string
	Rank  int
}

// RunSimilarFamily runs similar.Similar (unmodified — Feature 2's dispatch,
// not a reimplementation) for each case and reports the best rank among the
// case's acceptable neighbour ids — the "expected-neighbor test per major
// concept family" docs/SEARCH-CAPABILITY-SPEC.md Feature 6 asks for
// ("API endpoint, ISO message, reference concept, manual section").
func RunSimilarFamily(ctx context.Context, s similar.Store, emb embed.Embedder, bundleDir string, cases []SimilarCase, limit int) ([]RankResult, error) {
	out := make([]RankResult, 0, len(cases))
	for _, c := range cases {
		opts := similar.Options{Mode: c.Mode, IncludeGraph: true, Filter: postgres.Filter{Limit: limit}}
		hits, err := similar.Similar(ctx, s, emb, bundleDir, c.ConceptID, opts)
		if err != nil {
			return nil, fmt.Errorf("similar %q (%s): %w", c.ConceptID, c.Mode, err)
		}
		plain := make([]postgres.Hit, len(hits))
		for i, h := range hits {
			plain[i] = h.Hit
		}
		out = append(out, RankResult{Label: c.ConceptID, Rank: BestRank(plain, c.WantIDs)})
	}
	return out, nil
}
```

Add `"pixkb/internal/similar"` to `internal/evalkit/runners.go`'s import block (alongside `embed`, `query`, `postgres` already there from Task 2).

- [ ] **Step 2:** Create `eval/cases-similar-ids.tsv` — every case below was run live (`pixkb similar <id> --mode <mode> --limit 20`) before being written down; one case per major concept family the spec names:

```
# Concept-similarity expected-neighbor cases (eval/evalkit RunSimilarFamily,
# `pixkb eval similar`). Format: concept-id<TAB>mode<TAB>id1[,id2,...].
# One case per major concept family (docs/SEARCH-CAPABILITY-SPEC.md Feature 6
# acceptance criterion: "at least one expected-neighbor test per major
# concept family: API endpoint, ISO message, reference concept, manual
# section"). Verified live at limit=20 on 2026-07-04; ranks noted in comments
# are what was observed then — a regression is a rank that gets worse or the
# expected id disappearing, not necessarily rank drifting by one or two.

# ApiEndpoint family — verified rank 1.
api/openapi/post-cob.md	hybrid	api/openapi/get-cob-txid.md

# PacsMessage (ISO 20022 message) family — verified rank 1.
messages/pacs.008.md	hybrid	messages/pacs.009.md

# Reference concept family — verified rank 1.
reference/bacen-pix-concepts/01-tipos-de-chave-pix-cpf-cnpj-e-mail-telefone-evp.md	hybrid	api/openapi/get-entries-key.md

# ManualSection family — graph mode, verified rank 16 of 20 (this
# ManualSection is graph-linked to its own sibling sections first, which is
# correct domain behavior, not noise; messages/pacs.008.md is a real direct
# edge, confirmed via `pixkb related`).
manuals/ii-manualdepadroesparainiciacaodopix/secao-51.md	graph	messages/pacs.008.md
```

- [ ] **Step 3:** In `cmd/pixkb/eval.go`, add `newEvalSimilarCmd()` and register it:

```go
func newEvalSimilarCmd() *cobra.Command {
	var dsn, file string
	var limit int
	cmd := &cobra.Command{
		Use:   "similar",
		Short: "Expected-neighbor rank checks per concept family (similar.Similar)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg := loadConfig()
			if dsn != "" {
				cfg.DSN = dsn
			}
			ctx := cmd.Context()
			st, err := openStore(ctx, cfg)
			if err != nil {
				return err
			}
			defer st.Close()
			emb, err := newEmbedder(cfg)
			if err != nil {
				return err
			}
			cases, err := evalkit.LoadSimilarCases(file)
			if err != nil {
				return err
			}
			results, err := evalkit.RunSimilarFamily(ctx, st, emb, cfg.BundleDir, cases, limit)
			if err != nil {
				return err
			}
			return printRankReport(cmd.OutOrStdout(), results)
		},
	}
	cmd.Flags().StringVar(&dsn, "dsn", "", "Postgres DSN")
	cmd.Flags().StringVar(&file, "file", "eval/cases-similar-ids.tsv", "similarity case file (concept-id<TAB>mode<TAB>id1,id2,...)")
	cmd.Flags().IntVar(&limit, "limit", 20, "max results per similarity query")
	return cmd
}

// printRankReport writes one line per case's best rank plus top@1/top@5/MRR
// aggregates — the same metrics eval/tophit.sh reports, for the rank-based
// runners (similarity, explain-consistency).
func printRankReport(w io.Writer, results []evalkit.RankResult) error {
	var t1, t5 int
	var mrr float64
	for _, r := range results {
		if r.Rank > 0 {
			if r.Rank <= 1 {
				t1++
			}
			if r.Rank <= 5 {
				t5++
			}
			mrr += 1.0 / float64(r.Rank)
			fmt.Fprintf(w, "%-70.70s  rank=%d\n", r.Label, r.Rank)
		} else {
			fmt.Fprintf(w, "%-70.70s  rank=—\n", r.Label)
		}
	}
	fmt.Fprintln(w, "----")
	n := len(results)
	if n == 0 {
		fmt.Fprintln(w, "no cases")
		return nil
	}
	fmt.Fprintf(w, "cases=%d  top@1=%d (%.0f%%)  top@5=%d (%.0f%%)  MRR=%.3f\n",
		n, t1, 100*float64(t1)/float64(n), t5, 100*float64(t5)/float64(n), mrr/float64(n))
	return nil
}
```

And in `newEvalCmd`, change `root.AddCommand(newEvalMultiCmd())` to `root.AddCommand(newEvalMultiCmd(), newEvalSimilarCmd())`.

- [ ] **Step 4:** `go build ./...`, `go vet ./...`. If a DSN is reachable, manually run `go run ./cmd/pixkb eval similar` and confirm the four cases report rank 1, 1, 1, 16 — matching this task's documented live verification.
- [ ] **Step 5:** Commit: `git add internal/evalkit/runners.go eval/cases-similar-ids.tsv cmd/pixkb/eval.go && git commit -m "feat: add pixkb eval similar (expected-neighbor checks per concept family)"`.

---

### Task 4: Out-of-domain rejection runner + `pixkb eval ood` + `eval/cases-ood.tsv`

**Files:**
- Modify: `internal/evalkit/runners.go`, `cmd/pixkb/eval.go`
- Create: `eval/cases-ood.tsv`

**Interfaces:**
- Produces: `type OODResult struct { Query string; Leaked []string }`; `func RunOOD(ctx context.Context, s query.Searcher, emb embed.Embedder, queries []string, forbidden map[string]bool, limit int) ([]OODResult, error)`; `func newEvalOODCmd() *cobra.Command`.
- Consumes: `query.Hybrid`, `ForbiddenPresent` from Task 1, `LoadPairCases`+`ForbiddenIDs` (to build the forbidden set from the existing precise/fuzzy suites).

- [ ] **Step 1:** Append to `internal/evalkit/runners.go`:

```go
// OODResult is one out-of-domain case's outcome: which (if any) forbidden
// normative ids leaked into the result set for a query that should not match
// any of them.
type OODResult struct {
	Query  string
	Leaked []string
}

// RunOOD runs query.Hybrid (unmodified) for each out-of-domain query and
// checks that none of the forbidden ids (normally the union of every
// precise/fuzzy suite's expected ids — see ForbiddenIDs) leaked into the
// result. This is the "forbidden-id absence for out-of-domain or noisy
// cases" metric docs/SEARCH-CAPABILITY-SPEC.md Feature 6 names — it
// tolerates generic institutional filler in the results (verified live: OOD
// queries here return web/acessoinformacao-*.md noise, not empty results —
// the vector floor does not fully zero these out today) but treats a
// confidently-returned NORMATIVE Pix procedure as a real failure, per the
// Ranking Principles: "Treat out-of-domain silence as better than confident
// noise."
func RunOOD(ctx context.Context, s query.Searcher, emb embed.Embedder, queries []string, forbidden map[string]bool, limit int) ([]OODResult, error) {
	out := make([]OODResult, 0, len(queries))
	for _, q := range queries {
		hits, err := query.Hybrid(ctx, s, emb, q, postgres.Filter{Limit: limit})
		if err != nil {
			return nil, fmt.Errorf("hybrid %q: %w", q, err)
		}
		out = append(out, OODResult{Query: q, Leaked: ForbiddenPresent(hits, forbidden)})
	}
	return out, nil
}
```

- [ ] **Step 2:** Create `eval/cases-ood.tsv` — every query below was run live (`pixkb search "<query>" --limit 5`) before being written down; none returned a normative Pix procedure id (all returned `web/acessoinformacao-*.md` institutional filler, which this gate tolerates):

```
# Out-of-domain rejection cases (eval/evalkit RunOOD, `pixkb eval ood`).
# One query per line — no expected-id column (nothing should confidently
# match). Gate: none of eval/cases-precise-ids.tsv's or
# eval/cases-fuzzy-ids.tsv's expected ids may appear in the result (see
# ForbiddenIDs / ForbiddenPresent). Verified live on 2026-07-04: all four
# return only web/acessoinformacao-*.md institutional filler, zero leaked
# normative ids.
qual a previsão do tempo para amanhã em São Paulo?
receita de bolo de cenoura
melhor time de futebol do brasil
qual o número de telefone do presidente do Banco Central?
```

- [ ] **Step 3:** In `cmd/pixkb/eval.go`, add `newEvalOODCmd()` and register it:

```go
func newEvalOODCmd() *cobra.Command {
	var dsn, file, preciseFile, fuzzyFile string
	var limit int
	cmd := &cobra.Command{
		Use:   "ood",
		Short: "Forbidden-id absence for out-of-domain queries (query.Hybrid)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg := loadConfig()
			if dsn != "" {
				cfg.DSN = dsn
			}
			ctx := cmd.Context()
			st, err := openStore(ctx, cfg)
			if err != nil {
				return err
			}
			defer st.Close()
			emb, err := newEmbedder(cfg)
			if err != nil {
				return err
			}
			queries, err := evalkit.LoadQueries(file)
			if err != nil {
				return err
			}
			precise, err := evalkit.LoadPairCases(preciseFile)
			if err != nil {
				return err
			}
			fuzzy, err := evalkit.LoadPairCases(fuzzyFile)
			if err != nil {
				return err
			}
			forbidden := evalkit.ForbiddenIDs(precise, fuzzy)
			results, err := evalkit.RunOOD(ctx, st, emb, queries, forbidden, limit)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			leaks := 0
			for _, r := range results {
				if len(r.Leaked) > 0 {
					leaks++
					fmt.Fprintf(out, "LEAK  %-60.60s  %v\n", r.Query, r.Leaked)
				} else {
					fmt.Fprintf(out, "clean %-60.60s\n", r.Query)
				}
			}
			fmt.Fprintln(out, "----")
			fmt.Fprintf(out, "cases=%d  clean=%d  leaked=%d\n", len(results), len(results)-leaks, leaks)
			return nil
		},
	}
	cmd.Flags().StringVar(&dsn, "dsn", "", "Postgres DSN")
	cmd.Flags().StringVar(&file, "file", "eval/cases-ood.tsv", "OOD query file (one query per line)")
	cmd.Flags().StringVar(&preciseFile, "precise-file", "eval/cases-precise-ids.tsv", "forbidden-id source: precise cases")
	cmd.Flags().StringVar(&fuzzyFile, "fuzzy-file", "eval/cases-fuzzy-ids.tsv", "forbidden-id source: fuzzy cases")
	cmd.Flags().IntVar(&limit, "limit", 5, "max results per OOD query")
	return cmd
}
```

And in `newEvalCmd`, change the `root.AddCommand(...)` call to also include `newEvalOODCmd()`.

- [ ] **Step 4:** `go build ./...`, `go vet ./...`. If a DSN is reachable, manually run `go run ./cmd/pixkb eval ood` and confirm all four cases report `clean` — matching this task's documented live verification.
- [ ] **Step 5:** Commit: `git add internal/evalkit/runners.go eval/cases-ood.tsv cmd/pixkb/eval.go && git commit -m "feat: add pixkb eval ood (forbidden-id absence for out-of-domain queries)"`.

---

### Task 5: Explain-consistency runner + as-of invariant runner + `pixkb eval explain` / `pixkb eval asof`

**Files:**
- Modify: `internal/evalkit/runners.go`, `cmd/pixkb/eval.go`

**Interfaces:**
- Produces: `type ExplainIssue struct { Query string; Detail string }`; `func RunExplainConsistency(ctx context.Context, s query.Searcher, emb embed.Embedder, cases []PairCase) ([]ExplainIssue, error)`; `type AsOfIssue struct { Query string; Detail string }`; `func RunAsOfInvariant(ctx context.Context, s query.Searcher, emb embed.Embedder, cases []PairCase, latestEpoch int) ([]AsOfIssue, error)`; `func newEvalExplainCmd() *cobra.Command`; `func newEvalAsOfCmd() *cobra.Command`.
- Consumes: `query.HybridExplain` (`internal/query/hybrid.go:169`), `query.Hybrid` with `postgres.Filter.AsOfEpoch` (`internal/store/postgres/search.go:13`), `Store.Stats().LatestEpoch` (existing, used by `newStatsCmd`).

Both runners reuse `eval/cases-precise-ids.tsv` (only the `Query` field; `WantIDs` is ignored) — no new case file needed, matching this task's narrower scope than Tasks 2-4.

- [ ] **Step 1:** Append to `internal/evalkit/runners.go`:

```go
// ExplainIssue is one structural-consistency problem found in a --explain
// response: the rank order (Hit.Rank) must agree with the score order
// (Explain.FinalScore descending) — if rank 2 has a higher FinalScore than
// rank 1, the explanation is lying about why something ranked where it did,
// which is exactly what Feature 3 exists to prevent silently breaking.
type ExplainIssue struct {
	Query  string
	Detail string
}

// RunExplainConsistency runs query.HybridExplain (unmodified) for each case
// and checks two invariants that must always hold if the explanation is
// telling the truth about the ranking it describes: (1) FinalScore is
// non-increasing across the hits in rank order; (2) explains[i].FinalScore
// equals hits[i].Score for every i (the same invariant
// TestHybridExplain_MatchesHits already unit-tests with a fake — this reruns
// it against the live index as a "search explanation consistency" gate, per
// docs/SEARCH-CAPABILITY-SPEC.md Feature 6).
func RunExplainConsistency(ctx context.Context, s query.Searcher, emb embed.Embedder, cases []PairCase) ([]ExplainIssue, error) {
	var issues []ExplainIssue
	for _, c := range cases {
		hits, explains, err := query.HybridExplain(ctx, s, emb, c.Query, postgres.Filter{})
		if err != nil {
			return nil, fmt.Errorf("hybrid-explain %q: %w", c.Query, err)
		}
		if len(hits) != len(explains) {
			issues = append(issues, ExplainIssue{Query: c.Query, Detail: fmt.Sprintf("len(hits)=%d != len(explains)=%d", len(hits), len(explains))})
			continue
		}
		for i := range hits {
			if hits[i].Score != explains[i].FinalScore {
				issues = append(issues, ExplainIssue{Query: c.Query, Detail: fmt.Sprintf("hit[%d].Score=%v != explain[%d].FinalScore=%v", i, hits[i].Score, i, explains[i].FinalScore)})
			}
			if i > 0 && explains[i].FinalScore > explains[i-1].FinalScore {
				issues = append(issues, ExplainIssue{Query: c.Query, Detail: fmt.Sprintf("explain[%d].FinalScore=%v > explain[%d].FinalScore=%v (rank order violated)", i, explains[i].FinalScore, i-1, explains[i-1].FinalScore)})
			}
		}
	}
	return issues, nil
}

// AsOfIssue is one as-of-filtering invariant violation: querying at the
// current latest epoch must return exactly the same result as an unfiltered
// query, since "as of the latest state" and "no time-travel filter" describe
// the same state by construction. This is the deterministic gate
// docs/SEARCH-CAPABILITY-SPEC.md Feature 4's own acceptance criterion asks
// for ("As-of filtering is test-covered at the public surface") — reusing
// the live index instead of authoring historical fixtures (a concept's
// epoch history is environment-specific and would make a hardcoded
// before/after fixture fragile across KB instances).
type AsOfIssue struct {
	Query  string
	Detail string
}

// RunAsOfInvariant runs query.Hybrid (unmodified) twice per case — once
// unfiltered, once with Filter.AsOfEpoch pinned to the current latest
// epoch — and checks the two id sequences are identical.
func RunAsOfInvariant(ctx context.Context, s query.Searcher, emb embed.Embedder, cases []PairCase, latestEpoch int) ([]AsOfIssue, error) {
	var issues []AsOfIssue
	for _, c := range cases {
		unfiltered, err := query.Hybrid(ctx, s, emb, c.Query, postgres.Filter{})
		if err != nil {
			return nil, fmt.Errorf("hybrid %q: %w", c.Query, err)
		}
		epoch := latestEpoch
		asOf, err := query.Hybrid(ctx, s, emb, c.Query, postgres.Filter{AsOfEpoch: &epoch})
		if err != nil {
			return nil, fmt.Errorf("hybrid --as-of-epoch %d %q: %w", epoch, c.Query, err)
		}
		if len(unfiltered) != len(asOf) {
			issues = append(issues, AsOfIssue{Query: c.Query, Detail: fmt.Sprintf("len(unfiltered)=%d != len(as-of)=%d", len(unfiltered), len(asOf))})
			continue
		}
		for i := range unfiltered {
			if unfiltered[i].ID != asOf[i].ID {
				issues = append(issues, AsOfIssue{Query: c.Query, Detail: fmt.Sprintf("position %d: unfiltered=%s as-of=%s", i, unfiltered[i].ID, asOf[i].ID)})
				break
			}
		}
	}
	return issues, nil
}
```

- [ ] **Step 2:** In `cmd/pixkb/eval.go`, add both commands and register them:

```go
func newEvalExplainCmd() *cobra.Command {
	var dsn, file string
	cmd := &cobra.Command{
		Use:   "explain",
		Short: "Structural consistency of --explain output (query.HybridExplain)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg := loadConfig()
			if dsn != "" {
				cfg.DSN = dsn
			}
			ctx := cmd.Context()
			st, err := openStore(ctx, cfg)
			if err != nil {
				return err
			}
			defer st.Close()
			emb, err := newEmbedder(cfg)
			if err != nil {
				return err
			}
			cases, err := evalkit.LoadPairCases(file)
			if err != nil {
				return err
			}
			issues, err := evalkit.RunExplainConsistency(ctx, st, emb, cases)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			for _, iss := range issues {
				fmt.Fprintf(out, "ISSUE  %-50.50s  %s\n", iss.Query, iss.Detail)
			}
			fmt.Fprintln(out, "----")
			fmt.Fprintf(out, "cases=%d  issues=%d\n", len(cases), len(issues))
			return nil
		},
	}
	cmd.Flags().StringVar(&dsn, "dsn", "", "Postgres DSN")
	cmd.Flags().StringVar(&file, "file", "eval/cases-precise-ids.tsv", "queries to check (only the query column is used)")
	return cmd
}

func newEvalAsOfCmd() *cobra.Command {
	var dsn, file string
	cmd := &cobra.Command{
		Use:   "asof",
		Short: "As-of-at-latest-epoch equals unfiltered (query.Hybrid + Filter.AsOfEpoch)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg := loadConfig()
			if dsn != "" {
				cfg.DSN = dsn
			}
			ctx := cmd.Context()
			st, err := openStore(ctx, cfg)
			if err != nil {
				return err
			}
			defer st.Close()
			emb, err := newEmbedder(cfg)
			if err != nil {
				return err
			}
			s, err := st.Stats(ctx)
			if err != nil {
				return err
			}
			cases, err := evalkit.LoadPairCases(file)
			if err != nil {
				return err
			}
			issues, err := evalkit.RunAsOfInvariant(ctx, st, emb, cases, s.LatestEpoch)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			for _, iss := range issues {
				fmt.Fprintf(out, "ISSUE  %-50.50s  %s\n", iss.Query, iss.Detail)
			}
			fmt.Fprintln(out, "----")
			fmt.Fprintf(out, "cases=%d  latest-epoch=%d  issues=%d\n", len(cases), s.LatestEpoch, len(issues))
			return nil
		},
	}
	cmd.Flags().StringVar(&dsn, "dsn", "", "Postgres DSN")
	cmd.Flags().StringVar(&file, "file", "eval/cases-precise-ids.tsv", "queries to check (only the query column is used)")
	return cmd
}
```

And in `newEvalCmd`, add both to the `root.AddCommand(...)` call.

- [ ] **Step 3:** `go build ./...`, `go vet ./...`. If a DSN is reachable, manually run `go run ./cmd/pixkb eval explain` and `go run ./cmd/pixkb eval asof` and confirm both report `issues=0` — matching this task's documented live verification (`pixkb search "criar cobrança imediata" --as-of-epoch 1` was confirmed identical to unfiltered output during this plan's authoring, at the KB's current latest epoch of 1).
- [ ] **Step 4:** Commit: `git add internal/evalkit/runners.go cmd/pixkb/eval.go && git commit -m "feat: add pixkb eval explain and pixkb eval asof (structural consistency gates)"`.

---

### Task 6: RAG grounding type-diversity runner + `pixkb eval rag-diversity` + full verification + backlog

**Files:**
- Modify: `internal/evalkit/runners.go`, `cmd/pixkb/eval.go`, `docs/BACKLOG.md`
- Create: `eval/cases-rag-diversity.tsv`

**Interfaces:**
- Produces: `type DiversityResult struct { ID, Question string; Types []string; MinTypes int }`; `func RunRAGDiversity(ctx context.Context, r rag.Retriever, cs rag.ConceptSource, gen rag.Generator, cases []RAGDiversityCase) ([]DiversityResult, error)`; `type RAGDiversityCase struct { ID, Question string; MinTypes int }`; `func LoadRAGDiversityCases(path string) ([]RAGDiversityCase, error)`; `func newEvalRAGDiversityCmd() *cobra.Command`.
- Consumes: `rag.Ask` (`internal/rag/adapters.go:76`), `rag.Chunk.Type` from Task 1, `agents.NewAgency` (existing, used by `cmd/pixkb/ask.go`).

- [ ] **Step 1:** In `internal/evalkit/cases.go`, add the loader for this task's case format (4 columns: id, question, min-types, and — unlike the other loaders — no ids column, since the whole point is measuring how many DISTINCT `Chunk.Type`s the cited concepts span, not which specific ids get cited):

```go
// RAGDiversityCase is one RAG grounding-diversity case: a question and the
// minimum number of DISTINCT concept types its citations should span.
type RAGDiversityCase struct {
	ID        string
	Question  string
	MinTypes  int
}

// LoadRAGDiversityCases parses "id<TAB>question<TAB>min-types" TSV, mirroring
// eval/cases-rag.tsv's id-prefixed convention (comments/blanks skipped).
func LoadRAGDiversityCases(path string) ([]RAGDiversityCase, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()

	var out []RAGDiversityCase
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimRight(sc.Text(), "\r\n")
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "\t", 3)
		if len(parts) != 3 {
			continue
		}
		id, question, minField := strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1]), strings.TrimSpace(parts[2])
		if id == "" || question == "" || minField == "" {
			continue
		}
		var minTypes int
		if _, err := fmt.Sscanf(minField, "%d", &minTypes); err != nil {
			return nil, fmt.Errorf("%s: bad min-types %q on case %q: %w", path, minField, id, err)
		}
		out = append(out, RAGDiversityCase{ID: id, Question: question, MinTypes: minTypes})
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("scan %s: %w", path, err)
	}
	return out, nil
}
```

No import changes needed for this addition — `cases.go`'s existing import block (`bufio`, `fmt`, `os`, `strings`, from Task 1) already covers everything `LoadRAGDiversityCases` uses.

- [ ] **Step 2:** Append to `internal/evalkit/runners.go`:

```go
// DiversityResult is one RAG-diversity case's outcome: the distinct concept
// Types among the answer's actual citations (not all grounding chunks —
// what got CITED is the real diversity signal), against the case's minimum.
type DiversityResult struct {
	ID       string
	Question string
	Types    []string
	MinTypes int
}

// RunRAGDiversity runs rag.Ask (unmodified — Features 5's retrieval +
// answer.Synthesize, not a reimplementation) for each case and counts the
// distinct concept Types among the cited ids — the "type diversity for RAG
// grounding" metric docs/SEARCH-CAPABILITY-SPEC.md Feature 6 names
// explicitly. Needs a live agent provider (like eval/run-rag-judge.sh); the
// CLI wraps this with the same --provider flag pixkb ask already exposes.
func RunRAGDiversity(ctx context.Context, r rag.Retriever, cs rag.ConceptSource, gen rag.Generator, cases []RAGDiversityCase) ([]DiversityResult, error) {
	out := make([]DiversityResult, 0, len(cases))
	for _, c := range cases {
		ans, g, err := rag.Ask(ctx, r, cs, gen, c.Question, rag.Options{})
		if err != nil {
			return nil, fmt.Errorf("ask %q: %w", c.ID, err)
		}
		typeByID := make(map[string]string, len(g.Chunks))
		for _, ch := range g.Chunks {
			typeByID[ch.ID] = ch.Type
		}
		seen := map[string]bool{}
		var types []string
		for _, cid := range ans.Citations {
			t := typeByID[cid]
			if t != "" && !seen[t] {
				seen[t] = true
				types = append(types, t)
			}
		}
		out = append(out, DiversityResult{ID: c.ID, Question: c.Question, Types: types, MinTypes: c.MinTypes})
	}
	return out, nil
}
```

Add `"pixkb/internal/rag"` to `internal/evalkit/runners.go`'s import block.

- [ ] **Step 3:** Create `eval/cases-rag-diversity.tsv`. These questions are deliberately broader than `eval/cases-rag.tsv`'s single-concept cases, designed to need evidence from more than one concept type — but (unlike every other case file in this plan) they were NOT live-verified during authoring, because doing so would spend a real subscription-agent turn per case (`rag.Ask` calls the answerer fleet, same cost as `eval/run-rag-judge.sh`). Say so plainly in the file header; the implementer runs this task's Step 5 to get the first real numbers:

```
# RAG grounding type-diversity cases (eval/evalkit RunRAGDiversity,
# `pixkb eval rag-diversity`). Format: id<TAB>question<TAB>min-distinct-types.
# UNLIKE every other case file in this plan, these questions were NOT
# live-verified during authoring — rag.Ask spends a real subscription-agent
# turn per case (same cost as eval/run-rag-judge.sh), so verification is
# deferred to this task's own Step 5 (run once, on purpose, and record the
# actual numbers here as a comment or in the task report). min-types is a
# starting expectation, not a proven baseline — adjust after the first run
# if the actual diversity differs, and say so in the task report rather than
# silently editing the number to make it pass.
diversity-devolucao-fluxo	quais os passos e regras para tratar a devolução de um Pix, desde a chamada de API até a mensagem interbancária envolvida?	2
diversity-cobranca-fluxo	como funciona o ciclo de uma cobrança com vencimento, da criação via API até a liquidação no SPI?	2
```

- [ ] **Step 4:** In `cmd/pixkb/eval.go`, add `newEvalRAGDiversityCmd()` (mirrors `cmd/pixkb/ask.go`'s existing agent-wiring pattern — `agents.NewAgency`, `rag.HybridRetriever`, `rag.BundleSource`, `rag.AgentGenerator`) and register it:

```go
func newEvalRAGDiversityCmd() *cobra.Command {
	var dsn, provider, file string
	cmd := &cobra.Command{
		Use:   "rag-diversity",
		Short: "Distinct concept-type coverage of RAG citations (rag.Ask)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg := loadConfig()
			if dsn != "" {
				cfg.DSN = dsn
			}
			ctx := cmd.Context()
			st, err := openStore(ctx, cfg)
			if err != nil {
				return err
			}
			defer st.Close()
			emb, err := newEmbedder(cfg)
			if err != nil {
				return err
			}
			dir, err := os.Getwd()
			if err != nil {
				return err
			}
			ag, err := agents.NewAgency(provider, dir)
			if err != nil {
				return err
			}
			defer func() { _ = ag.Close() }()

			cases, err := evalkit.LoadRAGDiversityCases(file)
			if err != nil {
				return err
			}
			results, err := evalkit.RunRAGDiversity(ctx,
				rag.HybridRetriever{Store: st, Emb: emb},
				rag.BundleSource{Dir: cfg.BundleDir},
				rag.AgentGenerator{Agency: ag},
				cases,
			)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			below := 0
			for _, r := range results {
				status := "ok"
				if len(r.Types) < r.MinTypes {
					status = "BELOW MIN"
					below++
				}
				fmt.Fprintf(out, "%-9s  %-40.40s  types=%v (want>=%d)\n", status, r.ID, r.Types, r.MinTypes)
			}
			fmt.Fprintln(out, "----")
			fmt.Fprintf(out, "cases=%d  below-min=%d\n", len(results), below)
			return nil
		},
	}
	cmd.Flags().StringVar(&dsn, "dsn", "", "Postgres DSN")
	cmd.Flags().StringVar(&provider, "provider", "claude", "answerer backend: claude|codex|agy")
	cmd.Flags().StringVar(&file, "file", "eval/cases-rag-diversity.tsv", "RAG diversity case file (id<TAB>question<TAB>min-types)")
	return cmd
}
```

Add `"os"`, `"pixkb/internal/rag"`, `"pixkb/pkg/agents"`, and `_ "pixkb/pkg/agents/all"` to `cmd/pixkb/eval.go`'s import block (matching `cmd/pixkb/ask.go`'s own imports for the same providers).

In `newEvalCmd`, add `newEvalRAGDiversityCmd()` to the final `root.AddCommand(...)` call so all six subcommands are registered.

- [ ] **Step 5:** `go build ./...`, `go vet ./...`, `go test ./... -short` (full suite — every package, confirming zero regressions from this 6-task plan). If both a DSN and an agent provider are reachable, run `go run ./cmd/pixkb eval rag-diversity` ONCE and record the actual `types=[...]` output for both cases in the task report (this is the live verification Step 3 deferred) — if either case comes in below its `min-types`, that is a real, reportable finding (not a bug to silently fix by lowering the number without saying so).
- [ ] **Step 6:** Backlog (P2) in `docs/BACKLOG.md`:
  - `eval multi`'s known partial-coverage case (refund+webhook, 1/2 today) — fusion-weighting work to improve cross-subquery coverage without regressing precise top@5, deferred per ADR 0002's re-tuning caution.
  - `eval rag-diversity`'s two cases need their first live run's actual numbers recorded (Step 5) — if a case never reaches its min-types across a few runs, revisit the question wording or lower the bar, but do either explicitly, not silently.
  - Feature 7 (Domain-Aware Query Understanding) and Feature 8 (Search Quality Operations) remain unimplemented from `docs/SEARCH-CAPABILITY-SPEC.md` — each needs its own scoped plan.
  - `pixkb eval`'s six subcommands report numbers but never fail the process (`os.Exit(1)` on a bad number) — intentional per this plan's Global Constraints, but a future CI-gating use case would need a `--fail-under` style flag; not built here.
- [ ] **Step 7:** Commit: `git add internal/evalkit/cases.go internal/evalkit/runners.go eval/cases-rag-diversity.tsv cmd/pixkb/eval.go docs/BACKLOG.md && git commit -m "feat: add pixkb eval rag-diversity; backlog fusion-coverage gap and Feature 7-8 scope"`.
