// Package link derives cross-domain citation edges from BACEN normative text.
//
// The parser is a PURE, deterministic core: it takes concept body text and
// returns the canonical normative-reference ids (norm_ref) that the text
// cites, using an allow-list of anchored regexes for BACEN's instrument
// formats. It performs no I/O and never guesses — prose that merely contains
// an instrument word (e.g. "resolução do problema") yields nothing because a
// match requires the instrument keyword immediately followed by "nº <number>".
//
// Matching between a citation and the concept that IS its normative source is
// DATE-INDEPENDENT: the canonical id keeps its optional year suffix for
// display, but resolution happens on a BASE key (type+number, e.g. RES-BCB-1)
// via BaseRef, so the same instrument cited with a date and without a date
// resolves to the same target.
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

// ResolvedEdge is a citation edge whose Dst has been resolved from a canonical
// norm_ref to the concept id that embodies that normative source. NormRef keeps
// the base normative key that matched (for display/provenance).
type ResolvedEdge struct {
	Src     string
	Dst     string
	NormRef string
	Kind    string
}

// instrument maps one anchored BACEN citation pattern to its canonical
// norm_ref prefix. Each pattern uses named capture groups: `num` (the possibly
// dot-separated instrument number, required) and, optionally, `year` (the
// 4-digit year from a trailing "de <dia> de <mês> de <ano>" date clause). The
// circular arm additionally carries a `carta` group so "Carta Circular" is
// disambiguated from a plain "Circular" (see cartaAware).
type instrument struct {
	prefix string
	// cartaAware, when true, promotes prefix to "CC" whenever the `carta`
	// capture group matched, so "Carta Circular nº X" -> CC-X while a plain
	// "Circular nº X" stays CIR-X. This is handled with a single optional
	// leading group rather than a lookbehind (RE2 has none): the leftmost match
	// greedily consumes the "Carta " prefix when present, so the inner
	// "Circular" is never independently re-matched as a plain circular.
	cartaAware bool
	re         *regexp.Regexp
}

// dateSuffix is the optional trailing date clause that, when present, supplies
// the year for the canonical id (e.g. ", de 12 de agosto de 2020" -> 2020).
const dateSuffix = `(?:,?\s+de\s+\d{1,2}\s+de\s+\p{L}+\s+de\s+(?P<year>\d{4}))?`

// numGroup matches an instrument number with optional thousands separators.
const numGroup = `(?P<num>\d[\d.]*)`

// nMark matches the "nº" / "n°" / "no." abbreviation in its common variants.
const nMark = `n[ºo°]\.?\s*`

// instruments is the allow-list, most-specific keywords first. Every pattern
// is anchored on the instrument keyword(s) directly followed by the number
// mark, so prose without "nº <number>" can never match.
var instruments = []instrument{
	{prefix: "RES-BCB", re: regexp.MustCompile(`(?i)Resolução\s+BCB\s+` + nMark + numGroup + dateSuffix)},
	{prefix: "RES-CMN", re: regexp.MustCompile(`(?i)Resolução\s+CMN\s+` + nMark + numGroup + dateSuffix)},
	{prefix: "IN-BCB", re: regexp.MustCompile(`(?i)Instrução\s+Normativa\s+BCB\s+` + nMark + numGroup + dateSuffix)},
	// Single arm handles both "Carta Circular" (-> CC) and plain "Circular"
	// (-> CIR); the optional leading `carta` group selects the prefix.
	{prefix: "CIR", cartaAware: true, re: regexp.MustCompile(`(?i)(?P<carta>Carta\s+)?Circular\s+` + nMark + numGroup + dateSuffix)},
}

// canon builds the canonical id from a submatch of inst.re, or "" to skip.
func (inst instrument) canon(m []string) string {
	numIdx := inst.re.SubexpIndex("num")
	if numIdx < 0 || numIdx >= len(m) {
		return ""
	}
	number := strings.ReplaceAll(m[numIdx], ".", "")
	if number == "" {
		return ""
	}
	prefix := inst.prefix
	if inst.cartaAware {
		if ci := inst.re.SubexpIndex("carta"); ci >= 0 && ci < len(m) && m[ci] != "" {
			prefix = "CC"
		}
	}
	id := prefix + "-" + number
	if yi := inst.re.SubexpIndex("year"); yi >= 0 && yi < len(m) && m[yi] != "" {
		id += "-" + m[yi]
	}
	return id
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
			id := inst.canon(m)
			if id == "" {
				continue
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
			if inst.canon(m) == id && loc[0] < best {
				best = loc[0]
			}
		}
	}
	return best
}

// baseRefRe strips the optional trailing "-YYYY" year segment from a canonical
// normative id to yield its date-independent BASE key (type+number). The prefix
// allow-list keeps a 4-digit instrument number (e.g. CIR-3978) from being
// mistaken for a year: the year is only ever a SEPARATE trailing segment after
// the number.
var baseRefRe = regexp.MustCompile(`^(RES-BCB|RES-CMN|IN-BCB|CC|CIR)-(\d+)(?:-\d{4})?$`)

// BaseRef reduces a canonical normative id (a citation id OR a concept's
// norm_ref) to its date-independent base key: RES-BCB-1-2020 -> RES-BCB-1,
// RES-BCB-1 -> RES-BCB-1, CIR-3978 -> CIR-3978. Ids that do not match a known
// instrument shape are returned unchanged, so an unexpected norm_ref never
// silently collapses.
func BaseRef(id string) string {
	m := baseRefRe.FindStringSubmatch(id)
	if m == nil {
		return id
	}
	return m[1] + "-" + m[2]
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

// ResolveEdges returns the resolved citation edges for a single citing concept.
// targets maps BaseRef(norm_ref) -> concept id for every concept that embodies a
// normative source (build it with BaseRef so matching is date-independent).
// A citation is emitted only when its base key hits a target; self-loops (the
// resolved target equals the citing concept) are skipped rather than written.
func ResolveEdges(conceptID, body string, targets map[string]string) []ResolvedEdge {
	edges := Edges(conceptID, body)
	if len(edges) == 0 {
		return nil
	}
	var out []ResolvedEdge
	for _, e := range edges {
		base := BaseRef(e.Dst)
		dst, ok := targets[base]
		if !ok {
			continue // citation to a norm_ref not present in the KB
		}
		if dst == conceptID {
			continue // self-loop: a concept quoting its own instrument text
		}
		out = append(out, ResolvedEdge{Src: conceptID, Dst: dst, NormRef: base, Kind: "cites"})
	}
	return out
}
