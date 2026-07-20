package roster_test

import (
	"strings"
	"testing"

	"github.com/inovacc/corral"

	"pixkb/internal/roster"
)

func TestRosterRegistered(t *testing.T) {
	all := corral.All()
	if len(all) < 13 {
		t.Fatalf("want >=13 agents, got %d", len(all))
	}
	names := []string{
		"control", "gather", "scraper", "normalization", "quality", "governance",
		"research", "diagram", "hygiene", "deviation", "enrich", "answerer", "judge",
	}
	for _, name := range names {
		a, ok := corral.ByName(name)
		if !ok {
			t.Errorf("agent %q not registered", name)
			continue
		}
		if !strings.Contains(a.System, "pixkb operating contract") {
			t.Errorf("agent %q missing pixkb contract", name)
		}
		if !strings.Contains(a.System, "BACEN domain charter") {
			t.Errorf("agent %q missing BACEN domain charter", name)
		}
	}

	j, _ := corral.ByName("judge")
	if !strings.Contains(j.Schema, "relevance") {
		t.Error("judge agent missing structured schema")
	}

	e, _ := corral.ByName("enrich")
	if !strings.Contains(e.Schema, "intent_terms") {
		t.Error("enrich agent missing intent_terms schema")
	}

	ans, _ := corral.ByName("answerer")
	if !strings.Contains(ans.Schema, "refused") {
		t.Error("answerer agent missing refused field in schema")
	}
}

func TestCharterFor(t *testing.T) {
	const marker = "BACEN domain charter"

	for _, domain := range []string{"pix", "bacen-normative"} {
		c := roster.CharterFor(domain)
		if c == "" {
			t.Errorf("CharterFor(%q) returned empty charter", domain)
		}
		if !strings.Contains(c, marker) {
			t.Errorf("CharterFor(%q) missing marker %q", domain, marker)
		}
	}

	fallback := roster.CharterFor("unknown-domain")
	if fallback == "" {
		t.Error("CharterFor(unknown) returned empty fallback")
	}
	if !strings.Contains(fallback, marker) {
		t.Errorf("CharterFor(unknown) fallback missing marker %q", marker)
	}
}
