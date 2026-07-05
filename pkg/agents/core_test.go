package agents

import (
	"strings"
	"testing"
)

func TestRosterRegistered(t *testing.T) {
	all := All()
	if len(all) < 8 {
		t.Fatalf("want >=8 agents, got %d", len(all))
	}
	for _, name := range []string{"control", "gather", "scraper", "normalization", "quality", "governance", "research", "judge"} {
		a, ok := ByName(name)
		if !ok {
			t.Errorf("agent %q not registered", name)
			continue
		}
		if !strings.Contains(a.System, "pixkb operating contract") {
			t.Errorf("agent %q missing pixkb contract", name)
		}
	}
	j, _ := ByName("judge")
	if !strings.Contains(j.Schema, "relevance") {
		t.Error("judge agent missing structured schema")
	}
}

func TestComposePromptEmbedsSchema(t *testing.T) {
	req := RunRequest{Agent: Agent{System: "INSTRUCTION", Schema: `{"type":"object"}`}, Input: "the task"}
	p := req.ComposePrompt(true)
	if !strings.HasPrefix(p, "INSTRUCTION") || !strings.Contains(p, "the task") || !strings.Contains(p, `"type":"object"`) {
		t.Fatalf("compose prompt wrong:\n%s", p)
	}
	if got := req.EffectiveSchema(); got != `{"type":"object"}` {
		t.Errorf("effective schema = %q", got)
	}
}

func TestDoctor(t *testing.T) {
	r := Doctor()
	if len(r.Checks) == 0 || r.Verdict == "" {
		t.Fatal("doctor returned empty report")
	}
	var roster *Check
	for i := range r.Checks {
		if r.Checks[i].Name == "roster" {
			roster = &r.Checks[i]
		}
	}
	if roster == nil || roster.Verdict != "PASS" {
		t.Errorf("roster check should PASS, got %+v", roster)
	}
}

