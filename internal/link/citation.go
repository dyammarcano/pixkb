// Package link derives cross-domain citation edges from BACEN normative text.
//
// The parser is a PURE, deterministic core: it takes concept body text and
// returns the canonical normative-reference ids (norm_ref) that the text
// cites, using an allow-list of anchored regexes for BACEN's instrument
// formats. It performs no I/O and never guesses — prose that merely contains
// an instrument word (e.g. "resolução do problema") yields nothing because a
// match requires the instrument keyword immediately followed by "nº <number>".
package link

import (
	"regexp"
	"strings"
)

// Edge is a directed citation relation: the citing concept (Src) references a
// canonical normative id (Dst). Kind is always "cites".
type Edge struct {
	Src  string
	Dst  string
	Kind string
}

// instrument maps one anchored BACEN citation pattern to its canonical
// norm_ref prefix. Each pattern captures group 1 = the (possibly
// dot-separated) instrument number and, optionally, group 2 = the 4-digit
// year from a trailing "de <dia> de <mês> de <ano>" date clause.
type instrument struct {
	prefix string
	re     *regexp.Regexp
}

// dateSuffix is the optional trailing date clause that, when present, supplies
// the year for the canonical id (e.g. ", de 12 de agosto de 2020" -> 2020).
const dateSuffix = `(?:,?\s+de\s+\d{1,2}\s+de\s+\p{L}+\s+de\s+(\d{4}))?`

// numGroup matches an instrument number with optional thousands separators.
const numGroup = `(\d[\d.]*)`

// nMark matches the "nº" / "n°" / "no." abbreviation in its common variants.
const nMark = `n[ºo°]\.?\s*`

// instruments is the allow-list, most-specific keywords first. Every pattern
// is anchored on the instrument keyword(s) directly followed by the number
// mark, so prose without "nº <number>" can never match.
var instruments = []instrument{
	{prefix: "RES-BCB", re: regexp.MustCompile(`(?i)Resolução\s+BCB\s+` + nMark + numGroup + dateSuffix)},
	{prefix: "RES-CMN", re: regexp.MustCompile(`(?i)Resolução\s+CMN\s+` + nMark + numGroup + dateSuffix)},
	{prefix: "IN-BCB", re: regexp.MustCompile(`(?i)Instrução\s+Normativa\s+BCB\s+` + nMark + numGroup + dateSuffix)},
	{prefix: "CIR", re: regexp.MustCompile(`(?i)Circular\s+` + nMark + numGroup + dateSuffix)},
}

// ParseCitations returns the canonical norm_ref ids cited in body, in order of
// first appearance and de-duplicated. It is pure and deterministic.
func ParseCitations(body string) []string {
	if body == "" {
		return nil
	}
	var out []string
	seen := make(map[string]struct{})
	for _, inst := range instruments {
		for _, m := range inst.re.FindAllStringSubmatch(body, -1) {
			number := strings.ReplaceAll(m[1], ".", "")
			if number == "" {
				continue
			}
			id := inst.prefix + "-" + number
			if len(m) > 2 && m[2] != "" {
				id += "-" + m[2]
			}
			if _, dup := seen[id]; dup {
				continue
			}
			seen[id] = struct{}{}
			out = append(out, id)
		}
	}
	return orderByPosition(body, out)
}

// orderByPosition sorts the (already de-duplicated) ids by the position of
// their first citing match in body, so multi-instrument bodies read in
// document order rather than instrument-list order.
func orderByPosition(body string, ids []string) []string {
	if len(ids) < 2 {
		return ids
	}
	pos := make(map[string]int, len(ids))
	for _, id := range ids {
		pos[id] = firstIndex(body, id)
	}
	// stable insertion sort by position (small n; keeps it dependency-free)
	sorted := make([]string, len(ids))
	copy(sorted, ids)
	for i := 1; i < len(sorted); i++ {
		for j := i; j > 0 && pos[sorted[j]] < pos[sorted[j-1]]; j-- {
			sorted[j], sorted[j-1] = sorted[j-1], sorted[j]
		}
	}
	return sorted
}

// firstIndex returns the byte offset of the earliest instrument match that
// canonicalizes to id, or a large sentinel if none is found.
func firstIndex(body, id string) int {
	best := len(body) + 1
	for _, inst := range instruments {
		for _, loc := range inst.re.FindAllStringSubmatchIndex(body, -1) {
			m := inst.re.FindStringSubmatch(body[loc[0]:loc[1]])
			number := strings.ReplaceAll(m[1], ".", "")
			cand := inst.prefix + "-" + number
			if len(m) > 2 && m[2] != "" {
				cand += "-" + m[2]
			}
			if cand == id && loc[0] < best {
				best = loc[0]
			}
		}
	}
	return best
}

// Edges builds the citation edges from a concept body: one "cites" edge per
// distinct norm_ref found, all pointing from conceptID to the canonical id.
func Edges(conceptID, body string) []Edge {
	refs := ParseCitations(body)
	if len(refs) == 0 {
		return nil
	}
	edges := make([]Edge, 0, len(refs))
	for _, ref := range refs {
		edges = append(edges, Edge{Src: conceptID, Dst: ref, Kind: "cites"})
	}
	return edges
}
