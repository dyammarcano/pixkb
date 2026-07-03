// Package epoch snapshots and diffs the bundle, writing log.md and git commits
// while updating the Postgres index in one logical pass.
package epoch

import "sort"

// DiffResult is the concept-level delta between two epoch states.
type DiffResult struct {
	Added   []string
	Changed []string
	Removed []string
}

// diffSets compares two id->contentSHA maps (old vs new) and returns the
// sorted added / changed / removed concept-ID lists. Pure set logic, no I/O.
func diffSets(oldSHA, newSHA map[string]string) DiffResult {
	var d DiffResult
	for id, ns := range newSHA {
		prev, ok := oldSHA[id]
		switch {
		case !ok:
			d.Added = append(d.Added, id)
		case prev != ns:
			d.Changed = append(d.Changed, id)
		}
	}
	for id := range oldSHA {
		if _, ok := newSHA[id]; !ok {
			d.Removed = append(d.Removed, id)
		}
	}
	sort.Strings(d.Added)
	sort.Strings(d.Changed)
	sort.Strings(d.Removed)
	return d
}
