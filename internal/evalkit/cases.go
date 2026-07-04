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

// RAGDiversityCase is one RAG grounding-diversity case: a question and the
// minimum number of DISTINCT concept types its citations should span.
type RAGDiversityCase struct {
	ID       string
	Question string
	MinTypes int
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
