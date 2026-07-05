package roster_test

import (
	"context"
	"encoding/json"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/inovacc/corral"
	_ "github.com/inovacc/corral/all" // registers codex/claude/agy providers
)

// e2eConceptSchema mirrors the roster conceptSchema (OpenAI-strict: every
// property in required) so the agent reply is structured.
const e2eConceptSchema = `{
  "type": "object",
  "additionalProperties": false,
  "required": ["concepts"],
  "properties": {
    "concepts": {
      "type": "array",
      "items": {
        "type": "object",
        "additionalProperties": false,
        "required": ["id","type","title","body","tags","language","source_uri"],
        "properties": {
          "id":         {"type": "string"},
          "type":       {"type": "string"},
          "title":      {"type": "string"},
          "body":       {"type": "string"},
          "tags":       {"type": "array", "items": {"type": "string"}},
          "language":   {"type": "string", "enum": ["pt","en"]},
          "source_uri": {"type": "string"}
        }
      }
    }
  }
}`

// firstAvailableProvider returns a coding-agent backend whose CLI is on PATH, or
// "" if none. Codex is preferred (cheaper, native --output-schema).
func firstAvailableProvider() string {
	for _, p := range []struct{ name, bin string }{{"codex", "codex"}, {"claude", "claude"}} {
		if _, err := exec.LookPath(p.bin); err == nil {
			return p.name
		}
	}
	return ""
}

// TestAgency_RealAgentEmitsStructuredConcept is the live half of the fleet
// round-trip: a real coding-agent CLI, driven through corral's Agency with a
// conceptSchema, must return a parseable OKF concept. Combined with the MCP
// concept_upsert->search round-trip (internal/kbmcp), this proves the
// agent -> structured output -> write-back -> retrieve loop the Curator runs.
//
// Skipped under -short (it spends a real subscription turn) and when no provider
// CLI is installed, so the default `-short` suite stays fast and offline. Uses
// an ad-hoc corral.Agent (not one from internal/roster's registered fleet) —
// this test proves the Agency/Provider contract works end to end, independent
// of any specific roster agent's content.
func TestAgency_RealAgentEmitsStructuredConcept(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping live agent e2e in -short mode (spends a real subscription turn)")
	}
	prov := firstAvailableProvider()
	if prov == "" {
		t.Skip("no codex/claude CLI on PATH")
	}

	ag, err := corral.NewAgency(prov, t.TempDir())
	if err != nil {
		t.Fatalf("NewAgency(%s): %v", prov, err)
	}
	defer func() { _ = ag.Close() }()

	agent := corral.Agent{
		Name:   "e2e-emitter",
		Schema: e2eConceptSchema,
		System: "You output ONLY the requested concept as JSON matching the schema. " +
			"Do not add prose. Preserve the exact id and source_uri given.",
	}
	const wantID = "reference/e2e/spi-marker.md"
	input := "Emit exactly one concept: id=" + wantID + ", type=Reference, " +
		"title='SPI — Sistema de Pagamentos Instantâneos', " +
		"body='O SPI é a infraestrutura de liquidação instantânea operada pelo BACEN.', " +
		"language=pt, source_uri='test:e2e', tags=['spi']."

	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()

	res, err := ag.RunAgent(ctx, agent, input)
	if err != nil {
		t.Fatalf("RunAgent: %v", err)
	}

	// Tolerant extraction: codex native schema returns clean JSON; claude may
	// fence or wrap it.
	raw := strings.TrimSpace(res.Text)
	if i, j := strings.IndexByte(raw, '{'), strings.LastIndexByte(raw, '}'); i >= 0 && j > i {
		raw = raw[i : j+1]
	}
	var doc struct {
		Concepts []struct {
			ID    string `json:"id"`
			Type  string `json:"type"`
			Title string `json:"title"`
			Body  string `json:"body"`
		} `json:"concepts"`
	}
	if err := json.Unmarshal([]byte(raw), &doc); err != nil {
		t.Fatalf("agent reply not parseable as conceptSchema: %v\nreply: %s", err, res.Text)
	}
	if len(doc.Concepts) == 0 {
		t.Fatalf("agent returned no concepts; reply: %s", res.Text)
	}
	c := doc.Concepts[0]
	if c.ID != wantID {
		t.Errorf("concept id = %q, want %q", c.ID, wantID)
	}
	if strings.TrimSpace(c.Title) == "" || strings.TrimSpace(c.Body) == "" {
		t.Errorf("concept missing title/body: %+v", c)
	}
}
