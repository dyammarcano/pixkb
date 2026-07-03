package rag

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// Generator runs the answerer agent over a prompt and returns its raw reply.
// Production wires this to agents.Agency.Run("answerer", prompt); tests inject a
// fake. The rag core never imports the agent fleet directly, so Answer is unit-
// testable with no provider.
type Generator interface {
	Generate(ctx context.Context, prompt string) (string, error)
}

// Answer is the synthesized, grounded reply. Citations are concept ids that were
// validated against the grounding (hallucinated ids are dropped). Refused is true
// when the context did not support an answer — either the agent refused or the
// guardrails forced it.
type Answer struct {
	Text      string   `json:"answer"`
	Citations []string `json:"citations"`
	Refused   bool     `json:"refused"`
}

// refusalNote is the canonical "not in the KB" message (Portuguese — the KB's
// primary language) used when the layer refuses before/after the agent.
const refusalNote = "não consta na base de conhecimento"

// Answer synthesizes a grounded reply for the grounding's query. It REFUSES
// without spending an agent turn when there is no context (OOD / empty
// retrieval). Otherwise it prompts the answerer, parses the structured reply, and
// VALIDATES citations against the grounding's concept ids — any id the agent
// invented is dropped, and a non-refused answer left with zero valid citations is
// downgraded to a refusal (faithfulness: an uncited claim is not trustworthy in a
// normative KB).
func Synthesize(ctx context.Context, gen Generator, g Grounding) (Answer, error) {
	if len(g.Chunks) == 0 {
		return Answer{Text: refusalNote + " (nenhum contexto recuperado)", Refused: true}, nil
	}

	raw, err := gen.Generate(ctx, buildAnswerPrompt(g))
	if err != nil {
		return Answer{}, fmt.Errorf("answerer: %w", err)
	}
	var a Answer
	if err := json.Unmarshal([]byte(extractJSON(raw)), &a); err != nil {
		return Answer{}, fmt.Errorf("parse answerer reply: %w", err)
	}

	a.Citations = validCitations(a.Citations, g)
	switch {
	case !a.Refused && len(a.Citations) == 0:
		// Agent answered but cited nothing in the context — not trustworthy.
		a.Refused = true
		if strings.TrimSpace(a.Text) == "" {
			a.Text = refusalNote + " (resposta sem citação válida)"
		}
	case !a.Refused && strings.TrimSpace(a.Text) == "":
		// Cited but produced no prose — treat as a refusal, not a blank answer.
		a.Refused = true
		a.Citations = nil
		a.Text = refusalNote + " (resposta vazia)"
	}
	return a, nil
}

// buildAnswerPrompt frames the question + the citation-tagged context block.
func buildAnswerPrompt(g Grounding) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Pergunta: %s\n\n", g.Query)
	b.WriteString("Contexto (responda SOMENTE com base nestes conceitos; cite o id de cada um que usar):\n\n")
	b.WriteString(g.Render())
	b.WriteString("\n\nResponda em JSON: answer, citations (ids dos conceitos citados), refused. " +
		"Se o contexto não responder à pergunta, refused=true e citations vazio.")
	return b.String()
}

// validCitations keeps only the cited ids that actually appear in the grounding,
// deduped and order-preserved — the guard against fabricated provenance.
func validCitations(cited []string, g Grounding) []string {
	inCtx := make(map[string]bool, len(g.Chunks))
	for _, c := range g.Chunks {
		inCtx[c.ID] = true
	}
	var out []string
	seen := map[string]bool{}
	for _, id := range cited {
		id = strings.TrimSpace(id)
		if inCtx[id] && !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	return out
}

// extractJSON pulls the JSON object out of an agent reply that may wrap it in
// markdown fences or prose (mirrors curate.extractJSON — kept local so rag has no
// cross-package coupling). Returns the span from the first '{' to the last '}'.
func extractJSON(raw string) string {
	s := strings.TrimSpace(raw)
	if f := strings.Index(s, "```"); f >= 0 {
		rest := s[f+3:]
		if nl := strings.IndexByte(rest, '\n'); nl >= 0 {
			rest = rest[nl+1:]
		}
		if c := strings.Index(rest, "```"); c >= 0 {
			rest = rest[:c]
		}
		s = strings.TrimSpace(rest)
	}
	i, j := strings.IndexByte(s, '{'), strings.LastIndexByte(s, '}')
	if i >= 0 && j > i {
		return s[i : j+1]
	}
	return s
}
