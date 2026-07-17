package main

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestUnknownConfiguredDomains(t *testing.T) {
	cfg := Config{
		OpenAPISpecs: []OpenAPISpecConf{
			{File: "a.json", Domain: "tax"},  // known
			{File: "b.json", Domain: "taxx"}, // typo -> reported
			{File: "c.json", Domain: ""},     // empty -> allowed (backfilled to pix)
		},
		Legislation: []LegislationConf{
			{File: "lc214.pdf", Lei: "lc-214-2025", Domain: "tax"}, // known
			{File: "junk.pdf", Lei: "x", Domain: "pixx"},           // typo -> reported
		},
	}

	bad := unknownConfiguredDomains(cfg)
	require.Len(t, bad, 2)
	require.Contains(t, bad, "openapi_specs:b.json (domain: taxx)")
	require.Contains(t, bad, "legislation:junk.pdf (domain: pixx)")
	// The valid and empty domains must NOT be reported.
	for _, b := range bad {
		require.NotContains(t, b, "a.json")
		require.NotContains(t, b, "c.json")
		require.NotContains(t, b, "lc214.pdf")
	}
}

func TestUnknownConfiguredDomains_AllValid(t *testing.T) {
	cfg := Config{
		OpenAPISpecs: []OpenAPISpecConf{{File: "a.json", Domain: "tax"}},
		Legislation:  []LegislationConf{{File: "b.pdf", Lei: "l", Domain: "pix"}},
	}
	require.Empty(t, unknownConfiguredDomains(cfg))
}
