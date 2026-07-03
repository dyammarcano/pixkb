package okf

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseLinks(t *testing.T) {
	tests := []struct {
		name string
		body string
		want []string
	}{
		{
			name: "single relative md link",
			body: "See [pacs.002](pacs.002.md) for the response.",
			want: []string{"pacs.002.md"},
		},
		{
			name: "nested relative path",
			body: "Related: [DICT](../apis/dict-api/lookup.md).",
			want: []string{"apis/dict-api/lookup.md"},
		},
		{
			name: "dot-slash prefix normalized",
			body: "[here](./messages/pacs.008.md)",
			want: []string{"messages/pacs.008.md"},
		},
		{
			name: "dedup repeated links",
			body: "[a](pacs.008.md) and again [a](pacs.008.md)",
			want: []string{"pacs.008.md"},
		},
		{
			name: "ignore non-md links",
			body: "[site](https://bcb.gov.br) [img](logo.png) [md](camt.056.md)",
			want: []string{"camt.056.md"},
		},
		{
			name: "ignore absolute http md",
			body: "[ext](https://example.com/doc.md) [local](manuals/x.md)",
			want: []string{"manuals/x.md"},
		},
		{
			name: "strip anchor and query",
			body: "[s](messages/pacs.004.md#fields) [q](apis/dict.md?v=2)",
			want: []string{"messages/pacs.004.md", "apis/dict.md"},
		},
		{
			name: "no links",
			body: "plain text without links",
			want: []string{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseLinks(tt.body)
			assert.Equal(t, tt.want, got)
		})
	}
}
