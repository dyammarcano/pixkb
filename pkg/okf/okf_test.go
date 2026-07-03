package okf_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	internalokf "pixkb/internal/okf"
	pkgokf "pixkb/pkg/okf"
)

func TestConceptAliasIsIdentical(t *testing.T) {
	t.Parallel()
	// Because pkg/okf.Concept is a type ALIAS (not a distinct named type), a
	// value of internal/okf.Concept is assignable to pkg/okf.Concept with no
	// conversion. This compiles only if they are the same type.
	var internal = internalokf.Concept{ID: "messages/pacs.008.md", Type: "PacsMessage"}
	// Assigning internal/okf.Concept to a pkg/okf.Concept variable with no
	// conversion compiles only if pkg/okf.Concept is an alias of the same type.
	//nolint:staticcheck // ST1023: the explicit pkgokf.Concept type is the point —
	// it asserts the alias identity at compile time; do not let it be inferred.
	var external pkgokf.Concept = internal
	assert.Equal(t, "messages/pacs.008.md", external.ID)
	assert.Equal(t, "PacsMessage", external.Type)
}
