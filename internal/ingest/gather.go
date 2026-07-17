package ingest

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"pixkb/internal/okf"
)

// domainTagPrefix marks a concept's KB domain (domain:pix, domain:tax).
const domainTagPrefix = "domain:"

// defaultDomain is applied to any concept no source tagged with a domain.
const defaultDomain = "pix"

// tagDomain ensures every concept carries exactly one domain:* tag. A concept a
// source already tagged (e.g. domain:tax) keeps it; all others get domain:pix.
// Idempotent — a concept that already has a domain tag is left untouched.
func tagDomain(concepts []okf.Concept) []okf.Concept {
	for i := range concepts {
		hasDomain := false
		for _, t := range concepts[i].Tags {
			if strings.HasPrefix(t, domainTagPrefix) {
				hasDomain = true
				break
			}
		}
		if !hasDomain {
			concepts[i].Tags = append(concepts[i].Tags, domainTagPrefix+defaultDomain)
		}
	}
	return concepts
}

// GatherAll fetches every source in order and returns the merged concept set,
// sorted by ID. It errors if two sources emit the same concept ID — the OKF
// bundle uses the path as identity, so duplicate IDs would silently overwrite.
func GatherAll(ctx context.Context, sources []Source) ([]okf.Concept, error) {
	seen := make(map[string]string) // concept id -> source name
	var out []okf.Concept
	for _, src := range sources {
		cs, err := src.Fetch(ctx)
		if err != nil {
			return nil, fmt.Errorf("gather %s: %w", src.Name(), err)
		}
		for _, c := range cs {
			if prev, dup := seen[c.ID]; dup {
				return nil, fmt.Errorf("duplicate concept id %q from sources %s and %s", c.ID, prev, src.Name())
			}
			seen[c.ID] = src.Name()
			out = append(out, c)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return tagDomain(out), nil
}
