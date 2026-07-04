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
