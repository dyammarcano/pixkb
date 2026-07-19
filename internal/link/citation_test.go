package link

import (
	"reflect"
	"testing"
)

func TestParseCitations(t *testing.T) {
	cases := []struct {
		name string
		body string
		want []string
	}{
		{
			name: "resolucao bcb with date yields year",
			body: "Nos termos da Resolução BCB nº 1, de 12 de agosto de 2020, fica instituído.",
			want: []string{"RES-BCB-1-2020"},
		},
		{
			name: "resolucao cmn with thousands separator",
			body: "conforme a Resolução CMN nº 4.893 do Conselho.",
			want: []string{"RES-CMN-4893"},
		},
		{
			name: "circular with thousands separator",
			body: "revoga a Circular nº 3.978 desta autarquia.",
			want: []string{"CIR-3978"},
		},
		{
			name: "instrucao normativa bcb",
			body: "de acordo com a Instrução Normativa BCB nº 300.",
			want: []string{"IN-BCB-300"},
		},
		{
			name: "multiple distinct citations in order",
			body: "A Resolução CMN nº 4.893 e a Circular nº 3.978 tratam do tema.",
			want: []string{"RES-CMN-4893", "CIR-3978"},
		},
		{
			name: "duplicate citation deduped",
			body: "Circular nº 3.978 ... e novamente a Circular nº 3.978.",
			want: []string{"CIR-3978"},
		},
		{
			name: "prose resolucao is not a citation",
			body: "Buscamos a resolução do problema de forma pacífica.",
			want: nil,
		},
		{
			name: "prose circular is not a citation",
			body: "Recebeu uma circular de papel na mesa.",
			want: nil,
		},
		{
			name: "empty body",
			body: "",
			want: nil,
		},
		{
			name: "keyword without number is not a citation",
			body: "A Resolução BCB estabelece diretrizes gerais.",
			want: nil,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ParseCitations(tc.body)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("ParseCitations(%q) = %#v, want %#v", tc.body, got, tc.want)
			}
		})
	}
}

func TestEdges(t *testing.T) {
	body := "Ver a Resolução CMN nº 4.893 e a Circular nº 3.978."
	got := Edges("concepts/pix/foo.md", body)
	want := []Edge{
		{Src: "concepts/pix/foo.md", Dst: "RES-CMN-4893", Kind: "cites"},
		{Src: "concepts/pix/foo.md", Dst: "CIR-3978", Kind: "cites"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Edges = %#v, want %#v", got, want)
	}
}

func TestEdgesNoCitations(t *testing.T) {
	if got := Edges("x", "circular de papel"); got != nil {
		t.Fatalf("Edges with no citations = %#v, want nil", got)
	}
}
