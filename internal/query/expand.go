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
