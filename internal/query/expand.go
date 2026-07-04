package query

import "strings"

// maxSubqueries bounds ExpandQuery's output. Spec: "Default expansion count
// should be small, preferably 3 to 5 queries."
const maxSubqueries = 5

// entityTriggers maps a fixed, ordered set of Portuguese word-stems to a
// canonical subquery that steers retrieval toward that domain entity's
// concepts. Stems (not whole words) so common inflections match ("estornar",
// "estornado", "estorno" all start with "estorn"). Order is fixed so
// expansion is reproducible and reviewable. This list covers a subset of
// Feature 1's named example entities (Pix refund, webhook, DICT key, API
// endpoint, certificate, QR code); pacs/camt-message and settlement entities
// are no longer separately triggered (both measured to only fire on already-
// precise queries containing that literal jargon, providing no fuzzy-recall
// benefit while diluting precise ranking via concept ambiguity). The base
// Hybrid search handles these correctly via direct lexical/semantic match.
// This list is intentionally small and separate from the larger, versioned
// vocabulary Feature 7 ("Domain-Aware Query Understanding") will add later;
// that feature should supersede/extend this table, not duplicate it.
var entityTriggers = []struct {
	stems    []string
	subquery string
}{
	{[]string{"estorn", "devolu", "refund"}, "devolução pix refund"},
	{[]string{"webhook", "notific", "avis"}, "webhook notificação pix"},
	{[]string{"chave", "dict", "evp"}, "chave DICT pix"},
	{[]string{"endpoint", "api"}, "endpoint API"},
	{[]string{"certific", "mtls", "icp"}, "certificado mTLS ICP-Brasil"},
	{[]string{"qr"}, "QR Code Pix BR Code"},
	{[]string{"liquida", "spi"}, "liquidação SPI settlement"},
}

// ExpandQuery deterministically expands q into up to maxSubqueries retrieval
// queries: the original query verbatim, a concise domain-term rewrite
// (diacritics folded, stopwords stripped, via the same foldTokens used for
// title-boost matching), and one subquery per recognized domain entity the
// query mentions. Duplicate subqueries (case-insensitive) are dropped. The
// original query is always present and always first, so MultiHybrid always
// has at least the equivalent of a plain single-query hybrid search to fall
// back on.
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
	for _, trig := range entityTriggers {
		matched := false
		for token := range tokenSet {
			for _, stem := range trig.stems {
				if strings.HasPrefix(token, stem) {
					matched = true
					break
				}
			}
			if matched {
				break
			}
		}
		if matched && add(trig.subquery) {
			return out
		}
	}
	return out
}
