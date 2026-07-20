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

	t.Run("empty domains reproduces v0.1 WHERE byte-for-byte", func(t *testing.T) {
		// Regression guard: an all-domain search (no --domain) must emit the
		// exact WHERE + args a v0.1 filter produced — no domain predicate.
		where, args, err := buildFTSWhere("pix", Filter{Domains: nil})
		require.NoError(t, err)
		require.Equal(t, "WHERE fts @@ websearch_to_tsquery('pixpt', $1)", where)
		require.Equal(t, []any{"pix"}, args)
		require.NotContains(t, where, "domain")
	})

	t.Run("domains append domain = ANY at the next free placeholder", func(t *testing.T) {
		doms := []string{"pix", "bacen-normative"}
		where, args, err := buildFTSWhere("pix", Filter{Domains: doms})
		require.NoError(t, err)
		require.Contains(t, where, "AND domain = ANY($2)")
		require.Len(t, args, 2)          // q($1) + domains($2)
		require.Equal(t, doms, args[1])  // the slice itself, bound as one arg
		require.Equal(t, 1, strings.Count(where, "$2"))
	})

	t.Run("domains number after type/tag/exclude", func(t *testing.T) {
		doms := []string{"tax"}
		where, args, err := buildFTSWhere("pix", Filter{
			Type:       "ApiEndpoint",
			Tag:        "domain:tax",
			ExcludeIDs: []string{"x.md"},
			Domains:    doms,
		})
		require.NoError(t, err)
		require.Contains(t, where, "type = ANY($2)")
		require.Contains(t, where, "tags @> ARRAY[$3]::text[]")
		require.Contains(t, where, "id != ALL($4)")
		require.Contains(t, where, "AND domain = ANY($5)")
		require.Len(t, args, 5)
		require.Equal(t, doms, args[4])
	})
}
