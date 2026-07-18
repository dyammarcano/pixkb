package postgres

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestBuildFTSWhere_PlaceholderNumbering exercises the pure WHERE/arg assembly
// (no DB): predicate composition and $N numbering, including a spliced HQL
// fragment — the exact place a mis-numbered placeholder would break.
func TestBuildFTSWhere_PlaceholderNumbering(t *testing.T) {
	t.Run("bare query is $1 only", func(t *testing.T) {
		where, args, err := buildFTSWhere("pix", Filter{})
		require.NoError(t, err)
		require.Equal(t, "WHERE fts @@ websearch_to_tsquery('pixpt', $1)", where)
		require.Equal(t, []any{"pix"}, args)
	})

	t.Run("type/tag/exclude number sequentially from $2", func(t *testing.T) {
		where, args, err := buildFTSWhere("pix", Filter{
			Type:       "ApiEndpoint",
			Tag:        "domain:tax",
			ExcludeIDs: []string{"x.md"},
		})
		require.NoError(t, err)
		require.Contains(t, where, "type = ANY($2)")
		require.Contains(t, where, "tags @> ARRAY[$3]::text[]")
		require.Contains(t, where, "id != ALL($4)")
		require.Len(t, args, 4) // q + types + tag + excludeIDs
	})

	t.Run("HQL fragment splices at the next free placeholder", func(t *testing.T) {
		// Filter already has q($1) + tag($2); the HQL closure must number from $3.
		hql := func(start int) (string, []any, error) {
			return fmt.Sprintf("livro = $%d", start+1), []any{"i"}, nil
		}
		where, args, err := buildFTSWhere("pix", Filter{Tag: "domain:tax", HQLWhere: hql})
		require.NoError(t, err)
		require.Contains(t, where, "AND (livro = $3)")
		require.Len(t, args, 3)
		require.Equal(t, "i", args[2])
		// No placeholder collision: $3 appears exactly once.
		require.Equal(t, 1, strings.Count(where, "$3"))
	})

	t.Run("HQL error propagates", func(t *testing.T) {
		hql := func(int) (string, []any, error) { return "", nil, fmt.Errorf("bad hql") }
		_, _, err := buildFTSWhere("pix", Filter{HQLWhere: hql})
		require.Error(t, err)
	})
}
