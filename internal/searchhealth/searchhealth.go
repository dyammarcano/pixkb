// Package searchhealth assembles Feature 8 of docs/SEARCH-CAPABILITY-SPEC.md
// ("Search Quality Operations") into one report: which concepts are missing
// intent_terms, which titles are too noisy for title boosting, which
// concepts have sparse graph links, whether embedding coverage/consistency
// look healthy, and which deterministic eval cases are currently failing.
// It reuses existing signals rather than re-detecting them — hygiene.Scan
// and hygiene.MissingIntentTerms already do the content-quality checks;
// postgres.GraphSparsity/EmbeddingCoverage do the index-health checks;
// query.Hybrid + evalkit's own rank math do the eval-regression check. This
// package's only new logic is synthesis: turning four kinds of signal into
// one prioritized re-enrichment recommendation list — the spec's acceptance
// criterion "A maintainer can run one command to see search-readiness
// health."
package searchhealth

import (
	"context"
	"fmt"
	"sort"

	"pixkb/internal/embed"
	"pixkb/internal/evalkit"
	"pixkb/internal/hygiene"
	"pixkb/internal/okf"
	"pixkb/internal/query"
	"pixkb/internal/store/postgres"
)

// Signal kinds — see Signal.Kind.
const (
	KindSparseTerms    = "sparse-terms"
	KindNoisyTitle     = "noisy-title"
	KindSparseGraph    = "sparse-graph"
	KindEvalRegression = "eval-regression"
)

// signalWeight scores each signal kind for Recommend's prioritization.
// eval-regression is weighted heaviest — a concept failing a real, curated
// eval case is the highest-confidence sign something is actually broken,
// versus the other three signals, which are enrichment OPPORTUNITIES, not
// proven problems (the spec's own acceptance criterion: "avoid treating all
// missing enrichment as errors").
var signalWeight = map[string]int{
	KindEvalRegression: 3,
	KindSparseTerms:    1,
	KindNoisyTitle:     1,
	KindSparseGraph:    1,
}

// Signal is one search-readiness finding, unified across the different
// underlying sources so `pixkb search-health` can report and prioritize
// them in one list.
type Signal struct {
	ConceptID string
	Kind      string
	Detail    string
}

// Recommendation is one concept prioritized for re-enrichment, with the
// signals that put it there.
type Recommendation struct {
	ConceptID string
	Score     int
	Signals   []Signal
}

// Recommend groups signals by concept id and ranks them by total weight
// (see signalWeight), breaking ties by concept id for determinism. This is
// triage guidance, not an error list.
func Recommend(signals []Signal) []Recommendation {
	byID := map[string][]Signal{}
	var order []string
	for _, s := range signals {
		if _, ok := byID[s.ConceptID]; !ok {
			order = append(order, s.ConceptID)
		}
		byID[s.ConceptID] = append(byID[s.ConceptID], s)
	}
	out := make([]Recommendation, 0, len(byID))
	for _, id := range order {
		sigs := byID[id]
		score := 0
		for _, s := range sigs {
			score += signalWeight[s.Kind]
		}
		out = append(out, Recommendation{ConceptID: id, Score: score, Signals: sigs})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Score != out[j].Score {
			return out[i].Score > out[j].Score
		}
		return out[i].ConceptID < out[j].ConceptID
	})
	return out
}

// EvalCaseResult is one deterministic eval case's outcome — the "listing
// search eval regressions by case" signal.
type EvalCaseResult struct {
	Query   string
	WantIDs []string
	Rank    int // 0 = no acceptable id found within the search — a regression
}

// EvalRegressions runs query.Hybrid (unmodified) for each case in the given
// files and reports the rank at which an acceptable id was found (0 = not
// found — a regression). Reuses eval/tophit.sh's case-file format via
// evalkit.LoadPairCases and its own rank math via evalkit.BestRank; no new
// ranking implementation.
func EvalRegressions(ctx context.Context, s query.Searcher, emb embed.Embedder, casePaths ...string) ([]EvalCaseResult, error) {
	var out []EvalCaseResult
	for _, path := range casePaths {
		cases, err := evalkit.LoadPairCases(path)
		if err != nil {
			return nil, fmt.Errorf("load cases %s: %w", path, err)
		}
		for _, c := range cases {
			hits, err := query.Hybrid(ctx, s, emb, c.Query, postgres.Filter{})
			if err != nil {
				return nil, fmt.Errorf("hybrid %q: %w", c.Query, err)
			}
			out = append(out, EvalCaseResult{Query: c.Query, WantIDs: c.WantIDs, Rank: evalkit.BestRank(hits, c.WantIDs)})
		}
	}
	return out, nil
}

// Report is the assembled search-readiness health report.
type Report struct {
	TotalConcepts      int
	MissingIntentTerms []hygiene.Finding
	NoisyTitles        []hygiene.Finding
	SparseGraph        []postgres.SparseConcept
	Embedding          postgres.EmbeddingCoverage
	EvalRegressions    []EvalCaseResult
	Recommendations    []Recommendation
}

// BuildReport assembles a Report from the bundle concepts (for the
// hygiene-based signals), the live store (for graph/embedding signals), and
// the given deterministic eval case files (for the eval-regression signal —
// pass none to skip that signal).
func BuildReport(ctx context.Context, concepts []okf.Concept, st *postgres.Store, emb embed.Embedder, casePaths ...string) (Report, error) {
	rep := Report{TotalConcepts: len(concepts)}
	rep.MissingIntentTerms = hygiene.MissingIntentTerms(concepts)

	scan := hygiene.Scan(concepts)
	for _, f := range scan.Findings {
		if f.Check == hygiene.CheckJunkTitle {
			rep.NoisyTitles = append(rep.NoisyTitles, f)
		}
	}

	sparse, err := st.GraphSparsity(ctx)
	if err != nil {
		return rep, fmt.Errorf("graph sparsity: %w", err)
	}
	rep.SparseGraph = sparse

	cov, err := st.EmbeddingCoverage(ctx)
	if err != nil {
		return rep, fmt.Errorf("embedding coverage: %w", err)
	}
	rep.Embedding = cov

	if len(casePaths) > 0 {
		regressions, err := EvalRegressions(ctx, st, emb, casePaths...)
		if err != nil {
			return rep, fmt.Errorf("eval regressions: %w", err)
		}
		rep.EvalRegressions = regressions
	}

	var signals []Signal
	for _, f := range rep.MissingIntentTerms {
		signals = append(signals, Signal{ConceptID: f.ConceptID, Kind: KindSparseTerms, Detail: f.Detail})
	}
	for _, f := range rep.NoisyTitles {
		signals = append(signals, Signal{ConceptID: f.ConceptID, Kind: KindNoisyTitle, Detail: f.Detail})
	}
	for _, sc := range rep.SparseGraph {
		signals = append(signals, Signal{ConceptID: sc.ID, Kind: KindSparseGraph, Detail: "no graph edges"})
	}
	for _, r := range rep.EvalRegressions {
		if r.Rank == 0 {
			for _, id := range r.WantIDs {
				signals = append(signals, Signal{ConceptID: id, Kind: KindEvalRegression, Detail: fmt.Sprintf("query %q found no acceptable hit", r.Query)})
			}
		}
	}
	rep.Recommendations = Recommend(signals)

	return rep, nil
}
