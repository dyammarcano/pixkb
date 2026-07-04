package curate

import (
	"context"
	"strings"
	"testing"

	"pixkb/internal/hygiene"
	"pixkb/internal/okf"
)

// fakeFixer returns canned proposals keyed by concept id, so routing + the gate
// are exercised with no provider and no database.
type fakeFixer struct {
	out  map[string][]okf.Concept
	seen []string // agents invoked, in order
}

func (f *fakeFixer) Fix(_ context.Context, agent string, c okf.Concept, _ []hygiene.Finding) ([]okf.Concept, error) {
	f.seen = append(f.seen, agent+":"+c.ID)
	return f.out[c.ID], nil
}

func TestAgentForRouting(t *testing.T) {
	cases := map[hygiene.Check]string{
		hygiene.CheckDeviation:   "deviation",
		hygiene.CheckJunkTitle:   "hygiene",
		hygiene.CheckBrokenLink:  "hygiene",
		hygiene.CheckDuplicate:   "hygiene",
		hygiene.CheckStubBody:    "research",
		hygiene.CheckMissingProv: "",
		hygiene.CheckMissingType: "",
	}
	for ch, want := range cases {
		if got := agentFor(ch); got != want {
			t.Errorf("agentFor(%s) = %q, want %q", ch, got, want)
		}
	}
}

func TestPickAgentPriority(t *testing.T) {
	// A concept with both a deviation and a junk title must go to deviation.
	set := map[string]struct{}{"hygiene": {}, "deviation": {}}
	if got := pickAgent(set); got != "deviation" {
		t.Fatalf("pickAgent = %q, want deviation", got)
	}
	set = map[string]struct{}{"research": {}, "hygiene": {}}
	if got := pickAgent(set); got != "hygiene" {
		t.Fatalf("pickAgent = %q, want hygiene", got)
	}
}

// A concept carrying a Pulsar reference (deviation, error) routes to the
// deviation agent. A clean rewrite passes the gate; a still-deviating one is
// rejected.
func TestRunGateAcceptsCleanRejectsDeviating(t *testing.T) {
	dirty := okf.Concept{
		ID: "messages/pix-in.md", Type: "Reference", Title: "Pix-in flow",
		SourceURI: "https://bcb.gov.br/x",
		Body:      "# Pix-in flow\n\nThe Recebedor PSP publishes the credit to a Pulsar topic for settlement.",
		ContentSHA: "sha-dirty",
	}
	clean := dirty
	clean.Body = "# Pix-in flow\n\nThe Recebedor PSP confirms the credit to the payer via pacs.002 after SPI settlement, per BACEN rules."
	stillBad := dirty
	stillBad.Body = "# Pix-in flow\n\nThe Recebedor PSP routes the credit through a Kafka topic before SPI settlement."

	writeBundle(t, dirty)

	t.Run("clean fix proposed", func(t *testing.T) {
		c := curatorOver(t, dirty, &fakeFixer{out: map[string][]okf.Concept{dirty.ID: {clean}}})
		out, err := c.Run(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if out.Routed != 1 || out.Proposed != 1 || out.Rejected != 0 {
			t.Fatalf("routed=%d proposed=%d rejected=%d, want 1/1/0", out.Routed, out.Proposed, out.Rejected)
		}
		if it := out.Items[0]; it.Agent != "deviation" || it.Status != StatusProposed {
			t.Fatalf("item = %+v, want deviation/proposed", it)
		}
	})

	t.Run("still-deviating fix rejected by gate", func(t *testing.T) {
		c := curatorOver(t, dirty, &fakeFixer{out: map[string][]okf.Concept{dirty.ID: {stillBad}}})
		out, err := c.Run(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if out.Rejected != 1 || out.Proposed != 0 {
			t.Fatalf("rejected=%d proposed=%d, want 1/0", out.Rejected, out.Proposed)
		}
		if d := out.Items[0].Detail; !strings.Contains(d, "deviation") {
			t.Fatalf("reject detail = %q, want mention of deviation", d)
		}
	})
}

func TestPlanOffline(t *testing.T) {
	dirty := okf.Concept{
		ID: "messages/pix-in.md", Type: "Reference", Title: "Pix-in flow",
		SourceURI: "https://bcb.gov.br/x",
		Body:      "# Pix-in flow\n\nThe Recebedor PSP publishes to a Pulsar topic.",
		ContentSHA: "sha1",
	}
	bundle := writeBundle(t, dirty)
	out, err := Plan(bundle, nil)
	if err != nil {
		t.Fatal(err)
	}
	if out.Routed != 1 || out.Items[0].Status != StatusPlanned || out.Items[0].Agent != "deviation" {
		t.Fatalf("plan out = %+v", out)
	}
}

// TestPlanOffline_IDsRestrictsRouting verifies the --ids targeting added for
// /steps:next item 4: two concepts have fixable findings, but restricting to
// one id must route only that one, leaving the other unrouted.
func TestPlanOffline_IDsRestrictsRouting(t *testing.T) {
	a := okf.Concept{
		ID: "messages/pix-in.md", Type: "Reference", Title: "Pix-in flow",
		SourceURI: "https://bcb.gov.br/x",
		Body:      "# Pix-in flow\n\nThe Recebedor PSP publishes to a Pulsar topic.",
		ContentSHA: "sha1",
	}
	b := okf.Concept{
		ID: "messages/pix-out.md", Type: "Reference", Title: "Pix-out flow",
		SourceURI: "https://bcb.gov.br/y",
		Body:      "# Pix-out flow\n\nThe Pagador PSP publishes to a Kafka topic.",
		ContentSHA: "sha2",
	}
	bundle := writeBundle(t, a, b)

	all, err := Plan(bundle, nil)
	if err != nil {
		t.Fatal(err)
	}
	if all.Routed != 2 {
		t.Fatalf("unrestricted plan should route both concepts, got %+v", all)
	}

	restricted, err := Plan(bundle, []string{a.ID})
	if err != nil {
		t.Fatal(err)
	}
	if restricted.Routed != 1 || restricted.Items[0].ConceptID != a.ID {
		t.Fatalf("--ids restricted plan should route only %s, got %+v", a.ID, restricted)
	}
}

func TestParseConcepts(t *testing.T) {
	raw := `{"concepts":[{"id":"a.md","type":"Reference","title":"T","body":"hello body","source_uri":"u"}]}`
	cs, err := ParseConcepts(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(cs) != 1 || cs[0].ID != "a.md" || !strings.HasPrefix(cs[0].Body, "# T") {
		t.Fatalf("parsed = %+v", cs)
	}
}

// curatorOver builds a dry-run Curator over a one-concept bundle written to a
// temp dir.
func curatorOver(t *testing.T, c okf.Concept, f Fixer) *Curator {
	t.Helper()
	return &Curator{Bundle: writeBundle(t, c), Fixer: f}
}

// writeBundle writes the concepts as an OKF bundle in a temp dir and returns the
// dir. Uses okf's own writer so ReadBundle round-trips.
func writeBundle(t *testing.T, concepts ...okf.Concept) string {
	t.Helper()
	dir := t.TempDir()
	for _, c := range concepts {
		if err := okf.WriteConcept(dir, c); err != nil {
			t.Fatalf("write concept %s: %v", c.ID, err)
		}
	}
	return dir
}
