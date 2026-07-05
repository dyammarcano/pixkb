package query

import (
	"strings"
	"unicode"
)

// MatchedFields is the per-hit "why did this match" breakdown Feature 3's
// spec asks for: which query tokens appeared in which of the hit's three
// indexed text fields. It is computed AFTER ranking, from the same
// canonical text Postgres already indexed (title/intent_terms/body) — a
// read-only annotation, not new ranking logic (ADR 0002 forbids re-tuning
// FTS/vector ranking; this never changes score or order).
type MatchedFields struct {
	Tokens []string `json:"matched_tokens"`           // distinct query tokens found in ANY of the three fields, in query order
	Fields []string `json:"matched_field_categories"` // subset of "title","intent_terms","body" — which fields had at least one matching token
}

// ComputeMatchedFields tokenizes q the same simple way as the rest of this
// package's non-SQL text handling (lowercase, split on non-letter/digit
// runes, drop empty tokens) and checks each token for case-insensitive
// substring presence in title, intentTerms, and body independently. This
// is intentionally simpler than Postgres's stemmed/stopword-aware
// websearch_to_tsquery matching — it is a best-effort explanation aid, not
// a re-implementation of the ranking query's matching semantics; a token
// Postgres matched via stemming (e.g. query "cobranças" matching indexed
// "cobrança") may not be found here, and that's an acceptable, documented
// limitation of a presentation-layer feature.
func ComputeMatchedFields(q, title, intentTerms, body string) MatchedFields {
	tokens := tokenizeMatched(q)
	lowerTitle := strings.ToLower(title)
	lowerIntent := strings.ToLower(intentTerms)
	lowerBody := strings.ToLower(body)

	var matchedTokens []string
	var titleMatched, intentMatched, bodyMatched bool
	for _, t := range tokens {
		inTitle := strings.Contains(lowerTitle, t)
		inIntent := strings.Contains(lowerIntent, t)
		inBody := strings.Contains(lowerBody, t)
		titleMatched = titleMatched || inTitle
		intentMatched = intentMatched || inIntent
		bodyMatched = bodyMatched || inBody
		if inTitle || inIntent || inBody {
			matchedTokens = append(matchedTokens, t)
		}
	}

	// Fixed order regardless of which token matched which field first, per
	// this type's own doc comment ("title","intent_terms","body").
	var fields []string
	if titleMatched {
		fields = append(fields, "title")
	}
	if intentMatched {
		fields = append(fields, "intent_terms")
	}
	if bodyMatched {
		fields = append(fields, "body")
	}

	return MatchedFields{Tokens: matchedTokens, Fields: fields}
}

// tokenizeMatched lowercases q and splits on any rune that is not a Unicode
// letter or digit, dropping empty tokens and deduping while preserving
// first-seen order.
func tokenizeMatched(q string) []string {
	raw := strings.FieldsFunc(strings.ToLower(q), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	seen := make(map[string]bool, len(raw))
	out := make([]string, 0, len(raw))
	for _, t := range raw {
		if t == "" || seen[t] {
			continue
		}
		seen[t] = true
		out = append(out, t)
	}
	return out
}
