package ingest

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"pixkb/internal/okf"
)

func TestTagOfficial(t *testing.T) {
	t.Parallel()
	hosts := []string{"github.com/bacen", "bcb.gov.br", "bacen.github.io"}

	in := []okf.Concept{
		{ID: "a.md", SourceURI: "github.com/bacen/pix-api/README.md"},
		{ID: "b.md", SourceURI: "https://www.bcb.gov.br/estabilidadefinanceira/pix"},
		{ID: "c.md", Resource: "mirrors/bacen.github.io/pix-api/index.html"},
		{ID: "d.md", SourceURI: "https://example.com/random"},
		{ID: "e.md", SourceURI: "github.com/BACEN/pix-dict-api", Tags: []string{"zeta"}},
	}
	out := TagOfficial(in, hosts)

	assert.Contains(t, out[0].Tags, OfficialTag, "bacen repo tagged")
	assert.Contains(t, out[1].Tags, OfficialTag, "bcb page tagged")
	assert.Contains(t, out[2].Tags, OfficialTag, "matched via Resource")
	assert.NotContains(t, out[3].Tags, OfficialTag, "non-official untouched")
	assert.Contains(t, out[4].Tags, OfficialTag, "case-insensitive host match")
	assert.Contains(t, out[4].Tags, "zeta", "existing tags preserved")
}

func TestTagOfficial_Idempotent(t *testing.T) {
	t.Parallel()
	c := []okf.Concept{{ID: "a.md", SourceURI: "github.com/bacen/x"}}
	c = TagOfficial(c, []string{"github.com/bacen"})
	c = TagOfficial(c, []string{"github.com/bacen"})
	count := 0
	for _, tg := range c[0].Tags {
		if tg == OfficialTag {
			count++
		}
	}
	assert.Equal(t, 1, count, "tag added at most once")
}

func TestTagOfficial_NoHostsIsNoop(t *testing.T) {
	t.Parallel()
	c := []okf.Concept{{ID: "a.md", SourceURI: "github.com/bacen/x"}}
	c = TagOfficial(c, nil)
	assert.Empty(t, c[0].Tags)
}
