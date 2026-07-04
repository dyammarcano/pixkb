package similar

// domainAdjacency maps a concept type to the OTHER types considered
// domain-adjacent to it for Feature 2's "domain" signal — e.g. an API
// endpoint is domain-adjacent to the ISO messages and reference docs that
// describe the same Pix workflow. This is intentionally a SMALL, FIXED v1
// table at type-pair granularity only — it re-tags candidates already
// surfaced by another signal (semantic/lexical/graph), it does not run an
// independent corpus scan. The spec's prose examples are finer-grained than
// type pairs ("DICT endpoints near key concepts" specifically, not just
// "ApiEndpoint near Reference" generally) — that level of topic-specific
// domain rule is out of scope for this v1 and is backlogged (see
// docs/BACKLOG.md); adding entries here should be measured the same way the
// multi-query-retrieval plan's entityTriggers table was (a rule that looks
// right in a spot-check is not the same as one that holds up measured).
var domainAdjacency = map[string][]string{
	"ApiEndpoint":   {"PacsMessage", "CamtMessage", "Reference"},
	"PacsMessage":   {"ApiEndpoint", "CamtMessage"},
	"CamtMessage":   {"ApiEndpoint", "PacsMessage"},
	"Reference":     {"ApiEndpoint", "ManualSection"},
	"ManualSection": {"Reference"},
}

// tagDomain appends SignalDomain to the Why of every hit in hits whose Type
// is domain-adjacent to queryType, mutating hits in place. A queryType with
// no table entry is a no-op (not an error) — most concept types (e.g.
// "WebPage") simply have no domain-adjacency rule yet.
func tagDomain(hits []Hit, queryType string) {
	adj := domainAdjacency[queryType]
	if len(adj) == 0 {
		return
	}
	adjSet := make(map[string]bool, len(adj))
	for _, t := range adj {
		adjSet[t] = true
	}
	for i := range hits {
		if adjSet[hits[i].Type] {
			hits[i].Why = append(hits[i].Why, SignalDomain)
		}
	}
}
