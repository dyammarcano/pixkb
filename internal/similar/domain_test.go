package similar

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"pixkb/internal/store/postgres"
)

func TestTagDomain_AppendsDomainSignalForAdjacentTypes(t *testing.T) {
	t.Parallel()
	hits := []Hit{
		{Hit: postgres.Hit{ID: "a", Type: "PacsMessage"}, Why: []string{SignalSemantic}},
		{Hit: postgres.Hit{ID: "b", Type: "ManualSection"}, Why: []string{SignalLexical}},
	}
	tagDomain(hits, "ApiEndpoint") // querying from an ApiEndpoint concept

	assert.Equal(t, []string{SignalSemantic, SignalDomain}, hits[0].Why, "PacsMessage is domain-adjacent to ApiEndpoint")
	assert.Equal(t, []string{SignalLexical}, hits[1].Why, "ManualSection is NOT domain-adjacent to ApiEndpoint")
}

func TestTagDomain_UnknownQueryTypeIsNoOp(t *testing.T) {
	t.Parallel()
	hits := []Hit{{Hit: postgres.Hit{ID: "a", Type: "PacsMessage"}, Why: []string{SignalSemantic}}}
	tagDomain(hits, "SomeUnmappedType")
	assert.Equal(t, []string{SignalSemantic}, hits[0].Why, "unmapped query type must not panic or mutate Why")
}
