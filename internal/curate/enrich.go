package curate

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"pixkb/internal/hygiene"
	"pixkb/internal/okf"
)

// IntentFixer runs the enrich agent over one concept and returns the generated
// intent_terms string (recall synonyms / alternate phrasings). It is separate
// from Fixer because enrichment MERGES terms onto the existing concept rather
// than proposing a replacement — the agent never returns (and so can never
// mangle) the canonical body. AgencyFixer implements both; tests inject a fake.
type IntentFixer interface {
	EnrichTerms(ctx context.Context, concept okf.Concept) (string, error)
}

// enrichCandidates returns the concepts the enrich agent should be asked to
// enrich. Default: only those with empty intent_terms (the rollout fill). When
// reenrich is true, ALL concepts are routed so their existing terms can be
// re-tuned (e.g. after the embed-text or prompt changes). Output is a findings
// slice in deterministic concept-id order, matching MissingIntentTerms' shape.
func enrichCandidates(concepts []okf.Concept, reenrich bool) []hygiene.Finding {
	if !reenrich {
		return hygiene.MissingIntentTerms(concepts)
	}
	out := make([]hygiene.Finding, 0, len(concepts))
	for _, c := range concepts {
		out = append(out, hygiene.Finding{
			Check: hygiene.CheckMissingIntentTerms, ConceptID: c.ID,
			Severity: hygiene.SeverityWarn, Fixable: true,
			Detail: "re-enrich: regenerate intent_terms",
		})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].ConceptID < out[j].ConceptID })
	return out
}

// EnrichPlan is the offline preview of the enrich pass: it lists the concepts the
// enrich agent would be asked to enrich (those with no intent_terms, or ALL when
// reenrich is set) with no agent run and no database. Use it to scope a batch
// before spending any provider quota.
func EnrichPlan(bundle string, reenrich bool) (Outcome, error) {
	concepts, err := okf.ReadBundle(bundle)
	if err != nil {
		return Outcome{}, fmt.Errorf("read bundle %q: %w", bundle, err)
	}
	missing := enrichCandidates(concepts, reenrich)
	out := Outcome{Concepts: len(concepts), Findings: len(missing), Routed: len(missing)}
	for _, f := range missing {
		out.Items = append(out.Items, Item{
			ConceptID: f.ConceptID, Agent: "enrich",
			Checks: []hygiene.Check{hygiene.CheckMissingIntentTerms}, Status: StatusPlanned,
		})
	}
	return out, nil
}

// Enrich runs one intent_terms enrichment pass: scan the bundle for concepts
// with no intent_terms, ask the enrich agent for recall terms, MERGE them onto
// the concept (body untouched), gate the merged concept (charter-clean body AND
// charter-clean intent_terms, terms non-empty), and — only with Apply — write
// the batch back via the epoch runner + reindex. Limit caps concepts per pass to
// spare quota on the ~195-concept rollout.
func (c *Curator) Enrich(ctx context.Context) (Outcome, error) {
	if c.Enricher == nil {
		return Outcome{}, fmt.Errorf("enrich requested but no IntentFixer configured")
	}
	concepts, err := okf.ReadBundle(c.Bundle)
	if err != nil {
		return Outcome{}, fmt.Errorf("read bundle %q: %w", c.Bundle, err)
	}
	byID := make(map[string]okf.Concept, len(concepts))
	for _, x := range concepts {
		byID[x.ID] = x
	}

	missing := enrichCandidates(concepts, c.Reenrich)
	if c.Limit > 0 && len(missing) > c.Limit {
		missing = missing[:c.Limit]
	}
	out := Outcome{Concepts: len(concepts), Findings: len(missing), Routed: len(missing)}

	var accepted []okf.Concept
	for _, f := range missing {
		item := Item{ConceptID: f.ConceptID, Agent: "enrich",
			Checks: []hygiene.Check{hygiene.CheckMissingIntentTerms}}
		orig := byID[f.ConceptID]

		terms, err := c.Enricher.EnrichTerms(ctx, orig)
		if err != nil {
			item.Status, item.Detail = StatusError, err.Error()
			out.Errors++
			out.Items = append(out.Items, item)
			continue
		}
		terms = strings.TrimSpace(terms)
		if terms == "" {
			item.Status, item.Detail = StatusNoChange, "agent returned empty intent_terms"
			out.Items = append(out.Items, item)
			continue
		}

		merged := orig
		merged.IntentTerms = terms
		if bad := enrichGate(concepts, merged); len(bad) > 0 {
			item.Status = StatusRejected
			item.Detail = "gate: " + strings.Join(checkLabels(bad), ", ")
			out.Rejected++
			out.Items = append(out.Items, item)
			continue
		}

		item.Detail = fmt.Sprintf("intent_terms: %d terms (%dB)", len(strings.Fields(terms)), len(terms))
		if c.Apply {
			item.Status = StatusApplied
			accepted = append(accepted, merged)
			out.Applied++
		} else {
			item.Status = StatusProposed
			out.Proposed++
		}
		out.Items = append(out.Items, item)
	}

	if c.Apply && len(accepted) > 0 {
		if c.Runner == nil {
			return out, fmt.Errorf("apply requested but no runner configured")
		}
		res, err := c.Runner.UpsertBatch(ctx, accepted, "enrich")
		if err != nil {
			return out, fmt.Errorf("upsert enriched concepts: %w", err)
		}
		out.Commit = res.Commit
		if err := c.Runner.Reindex(ctx); err != nil {
			return out, fmt.Errorf("reindex after enrich: %w", err)
		}
	}
	return out, nil
}

// enrichGate validates a merged concept before write-back: the body/title must
// still be charter-clean (the standard gate, a defense since the body is
// unchanged) AND the generated intent_terms must not smuggle implementation
// detail. Empty result => safe to write.
func enrichGate(set []okf.Concept, merged okf.Concept) []hygiene.Finding {
	bad := gate(set, merged)
	bad = append(bad, hygiene.IntentTermsDeviations(merged)...)
	return bad
}

// intentTermsJSON mirrors the enrich agent's enrichSchema reply.
type intentTermsJSON struct {
	Concepts []struct {
		ID          string `json:"id"`
		IntentTerms string `json:"intent_terms"`
	} `json:"concepts"`
}

// ParseIntentTerms turns an enrich agent's enrichSchema JSON reply into an
// id -> intent_terms map, tolerating markdown fences / surrounding prose via the
// shared extractJSON. Exported so AgencyFixer and tests share one parser.
func ParseIntentTerms(raw string) (map[string]string, error) {
	var doc intentTermsJSON
	if err := json.Unmarshal([]byte(extractJSON(raw)), &doc); err != nil {
		return nil, fmt.Errorf("parse enrich reply: %w", err)
	}
	out := make(map[string]string, len(doc.Concepts))
	for _, c := range doc.Concepts {
		id := strings.TrimSpace(c.ID)
		if id == "" {
			continue
		}
		out[id] = strings.TrimSpace(c.IntentTerms)
	}
	return out, nil
}
