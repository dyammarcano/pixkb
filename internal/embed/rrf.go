package embed

import "sort"

// RRF fuses multiple ranked lists of concept IDs using Reciprocal Rank
// Fusion: score(id) = sum over lists of 1/(k + rank), with rank starting
// at 0. A non-positive k defaults to 60. Ties are broken deterministically
// by first-seen order across the input lists, then lexicographically.
func RRF(rankLists [][]string, k int) []string {
	if k <= 0 {
		k = 60
	}
	scores := make(map[string]float64)
	firstSeen := make(map[string]int)
	order := 0
	for _, list := range rankLists {
		for rank, id := range list {
			scores[id] += 1.0 / float64(k+rank)
			if _, ok := firstSeen[id]; !ok {
				firstSeen[id] = order
				order++
			}
		}
	}
	ids := make([]string, 0, len(scores))
	for id := range scores {
		ids = append(ids, id)
	}
	sort.SliceStable(ids, func(i, j int) bool {
		si, sj := scores[ids[i]], scores[ids[j]]
		if si != sj {
			return si > sj
		}
		if firstSeen[ids[i]] != firstSeen[ids[j]] {
			return firstSeen[ids[i]] < firstSeen[ids[j]]
		}
		return ids[i] < ids[j]
	})
	return ids
}
