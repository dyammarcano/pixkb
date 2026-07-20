package ingest

import (
	"slices"
	"sort"
	"strings"

	"pixkb/internal/okf"
)

// OfficialTag marks a concept as coming from an authoritative, operator-declared
// source. Downstream surfaces use it for rank boosting and to relax curation
// gates; searches can filter on it.
const OfficialTag = "trusted:official"

// TagOfficial adds OfficialTag to every concept whose SourceURI or Resource
// contains one of the given host roots (e.g. "github.com/bacen", "bcb.gov.br").
// Matching is case-insensitive and substring-based so it survives scheme/path
// variation. It is idempotent (never adds the tag twice) and a no-op when hosts
// is empty. Returns the same slice for call-site chaining.
func TagOfficial(concepts []okf.Concept, hosts []string) []okf.Concept {
	if len(hosts) == 0 {
		return concepts
	}
	norm := make([]string, 0, len(hosts))
	for _, h := range hosts {
		if h = strings.ToLower(strings.TrimSpace(h)); h != "" {
			norm = append(norm, h)
		}
	}
	for i := range concepts {
		hay := strings.ToLower(concepts[i].SourceURI + " " + concepts[i].Resource)
		if matchesAny(hay, norm) && !hasTag(concepts[i].Tags, OfficialTag) {
			concepts[i].Tags = append(concepts[i].Tags, OfficialTag)
			sort.Strings(concepts[i].Tags)
		}
	}
	return concepts
}

func matchesAny(hay string, needles []string) bool {
	for _, n := range needles {
		if strings.Contains(hay, n) {
			return true
		}
	}
	return false
}

func hasTag(tags []string, want string) bool {
	return slices.Contains(tags, want)
}
