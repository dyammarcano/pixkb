package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestBuildSources_IncludesScoutCrawlWhenConfigured(t *testing.T) {
	t.Parallel()
	cfg := Config{ScoutCrawlDir: "mirrors/bcb/knowledge/pages"}
	srcs := buildSources(cfg)

	names := map[string]bool{}
	for _, s := range srcs {
		names[s.Name()] = true
	}
	assert.True(t, names["scout-crawl"], "expected scout-crawl source when ScoutCrawlDir is set")
}

func TestBuildSources_OmitsScoutCrawlWhenNotConfigured(t *testing.T) {
	t.Parallel()
	cfg := Config{}
	srcs := buildSources(cfg)

	for _, s := range srcs {
		assert.NotEqual(t, "scout-crawl", s.Name())
	}
}
