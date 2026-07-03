package agents

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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

func TestOpenAIEmbedder(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-key" {
			http.Error(w, "no auth", http.StatusUnauthorized)
			return
		}
		var body embedReq
		_ = json.NewDecoder(r.Body).Decode(&body)
		resp := embedResp{}
		for i := len(body.Input) - 1; i >= 0; i-- { // out of order to test sorting
			resp.Data = append(resp.Data, struct {
				Index     int       `json:"index"`
				Embedding []float32 `json:"embedding"`
			}{Index: i, Embedding: []float32{float32(i), 0.5}})
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	t.Setenv("OPENAI_API_KEY", "test-key")
	t.Setenv("OPENAI_BASE_URL", srv.URL)
	e, err := NewOpenAIEmbedder("", 2)
	if err != nil {
		t.Fatalf("new embedder: %v", err)
	}
	vecs, err := e.Embed(context.Background(), []string{"a", "b", "c"})
	if err != nil {
		t.Fatalf("embed: %v", err)
	}
	if len(vecs) != 3 {
		t.Fatalf("want 3 vecs, got %d", len(vecs))
	}
	for i := range vecs {
		if vecs[i][0] != float32(i) {
			t.Errorf("vec %d out of input order: %v", i, vecs[i])
		}
	}
}

func TestOpenAIEmbedderNoKey(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "")
	if _, err := NewOpenAIEmbedder("", 0); err == nil {
		t.Error("want error when OPENAI_API_KEY unset")
	}
}
