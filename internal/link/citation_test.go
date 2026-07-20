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
		{
			name: "carta circular maps to CC not CIR",
			body: "conforme a Carta Circular nº 4.001 do BCB.",
			want: []string{"CC-4001"},
		},
		{
			name: "carta circular with date yields year on CC",
			body: "a Carta Circular nº 12, de 3 de março de 2021, dispõe.",
			want: []string{"CC-12-2021"},
		},
		{
			name: "plain circular stays CIR even alongside carta circular",
			body: "a Circular nº 3.978 e a Carta Circular nº 4.001 tratam do tema.",
			want: []string{"CIR-3978", "CC-4001"},
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

func TestBaseRef(t *testing.T) {
	cases := []struct{ in, want string }{
		{"RES-BCB-1-2020", "RES-BCB-1"}, // dated -> base
		{"RES-BCB-1", "RES-BCB-1"},      // undated -> same base
		{"RES-CMN-4893-2020", "RES-CMN-4893"},
		{"RES-CMN-4893", "RES-CMN-4893"},
		{"CIR-3978", "CIR-3978"},   // 4-digit number is NOT a year
		{"CC-4001", "CC-4001"},     // carta circular base
		{"CC-12-2021", "CC-12"},    // dated carta circular
		{"IN-BCB-300", "IN-BCB-300"},
		{"UNKNOWN-XYZ", "UNKNOWN-XYZ"}, // non-instrument id returned unchanged
	}
	for _, tc := range cases {
		if got := BaseRef(tc.in); got != tc.want {
			t.Errorf("BaseRef(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// TestResolveEdges_DateIndependent is the I1 invariant: a dated citation and an
// undated citation of the SAME instrument both resolve to the same target
// concept whose norm_ref may carry a year or not — and different instruments
// never collide.
func TestResolveEdges_DateIndependent(t *testing.T) {
	// Target concept embodies RES-BCB-1; its norm_ref happens to carry the year.
	targets := map[string]string{
		BaseRef("RES-BCB-1-2020"): "concepts/bacen/res-bcb-1.md",
		BaseRef("CIR-3978"):       "concepts/bacen/cir-3978.md",
	}

	dated := ResolveEdges("concepts/pix/a.md",
		"Nos termos da Resolução BCB nº 1, de 12 de agosto de 2020, ...", targets)
	undated := ResolveEdges("concepts/pix/b.md",
		"conforme a Resolução BCB nº 1 desta autarquia.", targets)

	if len(dated) != 1 || dated[0].Dst != "concepts/bacen/res-bcb-1.md" {
		t.Fatalf("dated citation did not link: %#v", dated)
	}
	if len(undated) != 1 || undated[0].Dst != "concepts/bacen/res-bcb-1.md" {
		t.Fatalf("undated citation did not link: %#v", undated)
	}
	if dated[0].Dst != undated[0].Dst {
		t.Fatalf("dated and undated resolved to different targets: %q vs %q", dated[0].Dst, undated[0].Dst)
	}

	// Different instrument must NOT collide with RES-BCB-1.
	other := ResolveEdges("concepts/pix/c.md", "revoga a Circular nº 3.978.", targets)
	if len(other) != 1 || other[0].Dst != "concepts/bacen/cir-3978.md" {
		t.Fatalf("circular citation mis-resolved: %#v", other)
	}
}

// TestResolveEdges_SkipsSelfLoop is the M4 invariant: a concept quoting its own
// instrument text must not produce a dst==src edge.
func TestResolveEdges_SkipsSelfLoop(t *testing.T) {
	self := "concepts/bacen/res-bcb-1.md"
	targets := map[string]string{BaseRef("RES-BCB-1-2020"): self}
	got := ResolveEdges(self, "Esta Resolução BCB nº 1 estabelece ...", targets)
	if got != nil {
		t.Fatalf("self-loop edge was not skipped: %#v", got)
	}
}

// TestResolveEdges_NoMatchDropped confirms a citation to an absent norm_ref is
// dropped (not an error, no edge).
func TestResolveEdges_NoMatchDropped(t *testing.T) {
	got := ResolveEdges("x", "conforme a Resolução BCB nº 99.", map[string]string{})
	if got != nil {
		t.Fatalf("unmatched citation should yield no edges: %#v", got)
	}
}
