package ispb

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestAliasFragments(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		query string
		want  []string // fragments that MUST be present (order-independent)
		empty bool
	}{
		{name: "nubank brand resolves to legal name", query: "nubank", want: []string{"nu pagamentos"}},
		{name: "case-insensitive", query: "NuBank", want: []string{"nu pagamentos"}},
		{name: "pagbank resolves to pagseguro", query: "pagbank", want: []string{"pagseguro"}},
		{name: "modal rebrand to genial", query: "modal", want: []string{"genial"}},
		{name: "bancoob rebrand to sicoob", query: "bancoob", want: []string{"sicoob"}},
		{name: "banese state bank", query: "banese", want: []string{"estado de sergipe"}},
		{name: "banco prefix rewrites to bco", query: "banco do brasil", want: []string{"bco do brasil"}},
		{name: "banco prefix plus nothing else", query: "banco inter", want: []string{"bco inter"}},
		{name: "unknown brand yields nothing", query: "totally unknown bank", empty: true},
		{name: "empty query", query: "", empty: true},
		{name: "too short does not fan out", query: "n", empty: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := AliasFragments(tt.query)
			if tt.empty {
				assert.Empty(t, got)
				return
			}
			for _, w := range tt.want {
				assert.Contains(t, got, w, "expected fragment %q for query %q", w, tt.query)
			}
		})
	}
}

// TestAliasFragments_NeverEchoesQuery guards the merge contract: a fragment
// identical to the raw query would just re-run the same search.
func TestAliasFragments_NeverEchoesQuery(t *testing.T) {
	t.Parallel()
	for _, q := range []string{"nu pagamentos", "pagseguro", "bco do brasil"} {
		assert.NotContains(t, AliasFragments(q), q)
	}
}
