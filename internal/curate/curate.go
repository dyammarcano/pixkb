// Package curate is the Curator: the orchestrator that closes the KB control
// loop. It scans the canonical bundle with the deterministic hygiene engine,
// routes each fixable problem to the agent that repairs it, re-scans every
// PROPOSED concept through the SAME detector as a governance gate (so a fix can
// never introduce a new BACEN-charter deviation), and — only with Apply — writes
// the gated concepts back via an epoch upsert + reindex.
//
//	scan -> route -> fix-agent -> gate -> upsert -> reindex
//
// The agent-run step is behind the Fixer interface, so routing and the gate are
// pure and testable with no provider and no database. Dry-run is the default.
package curate

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"strings"

	"pixkb/internal/epoch"
	"pixkb/internal/hygiene"
	"pixkb/internal/okf"
)

// Fixer runs one repair agent over a concept and its findings, returning the
// concept(s) the agent proposes. The real implementation drives the agent fleet
// (see AgencyFixer); tests inject a deterministic fake.
type Fixer interface {
	Fix(ctx context.Context, agent string, concept okf.Concept, findings []hygiene.Finding) ([]okf.Concept, error)
}

// Curator runs the scan -> route -> fix -> gate -> write loop over a bundle.
type Curator struct {
	Bundle   string        // canonical bundle dir (read for the scan + gate)
	Fixer    Fixer         // runs the repair agents (required for Run)
	Enricher IntentFixer   // runs the enrich agent (required for Enrich)
	Runner   *epoch.Runner // write path; nil disables Apply
	Apply    bool          // false (default) = dry-run: propose + gate, never write
	Limit    int           // cap routed concepts per pass (0 = no cap); spares quota on big bundles
	Reenrich bool          // Enrich: route ALL concepts, not just those with empty intent_terms (re-tune terms)
}

// Status is the per-concept verdict of one curate pass.
type Status string

const (
	StatusApplied  Status = "applied"        // gated clean and written (Apply only)
	StatusProposed Status = "proposed"       // gated clean, withheld (dry-run)
	StatusRejected Status = "rejected-gate"  // agent fix still trips an error finding
	StatusNoChange Status = "no-change"      // agent returned nothing for this concept
	StatusError    Status = "error"          // the agent run failed
	StatusSkipped  Status = "skipped-unfix"  // only unfixable findings (e.g. provenance)
	StatusPlanned  Status = "planned"        // offline routing preview (no agent run)
)

// Item is the outcome for one routed concept.
type Item struct {
	ConceptID string        `json:"concept_id"`
	Agent     string        `json:"agent"`
	Checks    []hygiene.Check `json:"checks"`
	Status    Status        `json:"status"`
	Detail    string        `json:"detail"`
}

// Outcome is the full pass summary.
type Outcome struct {
	Concepts int    `json:"concepts"`
	Findings int    `json:"findings"` // fixable findings the scan surfaced
	Routed   int    `json:"routed"`   // concepts handed to an agent
	Applied  int    `json:"applied"`
	Proposed int    `json:"proposed"`
	Rejected int    `json:"rejected"`
	Errors   int    `json:"errors"`
	Commit   string `json:"commit,omitempty"`
	Items    []Item `json:"items"`
}

// agentFor maps a hygiene check to the roster agent that repairs it. Deviation
// findings go to the charter enforcer; mechanical findings to hygiene; thin
// bodies to research (which can source real BACEN content). Unfixable-only
// checks return "".
func agentFor(c hygiene.Check) string {
	switch c {
	case hygiene.CheckDeviation:
		return "deviation"
	case hygiene.CheckJunkTitle, hygiene.CheckBrokenLink, hygiene.CheckDuplicate, hygiene.CheckSampleData:
		return "hygiene"
	case hygiene.CheckStubBody:
		return "research"
	default:
		return ""
	}
}

// agentPriority orders agents when a concept has findings for several: the
// charter enforcer wins (it must run first / it implies a rewrite), then the
// mechanical fixer, then research. One agent handles a concept per pass.
var agentPriority = map[string]int{"deviation": 0, "hygiene": 1, "research": 2}

// route groups the fixable findings of a report by concept and picks the single
// highest-priority agent for each. Returns routes in deterministic concept-id
// order. Concepts whose only findings are unfixable are reported as skipped.
func route(rep hygiene.Report) (routes []route_, skipped []Item) {
	type bucket struct {
		findings []hygiene.Finding
		agents   map[string]struct{}
		checks   map[hygiene.Check]struct{}
		anyFix   bool
	}
	by := map[string]*bucket{}
	order := []string{}
	for _, f := range rep.Findings {
		b := by[f.ConceptID]
		if b == nil {
			b = &bucket{agents: map[string]struct{}{}, checks: map[hygiene.Check]struct{}{}}
			by[f.ConceptID] = b
			order = append(order, f.ConceptID)
		}
		b.findings = append(b.findings, f)
		b.checks[f.Check] = struct{}{}
		if f.Fixable {
			b.anyFix = true
			if a := agentFor(f.Check); a != "" {
				b.agents[a] = struct{}{}
			}
		}
	}
	sort.Strings(order)
	for _, id := range order {
		b := by[id]
		checks := sortedChecks(b.checks)
		if !b.anyFix || len(b.agents) == 0 {
			skipped = append(skipped, Item{ConceptID: id, Checks: checks, Status: StatusSkipped,
				Detail: "only unfixable findings (needs human input)"})
			continue
		}
		agent := pickAgent(b.agents)
		routes = append(routes, route_{ConceptID: id, Agent: agent, Checks: checks, Findings: b.findings})
	}
	return routes, skipped
}

type route_ struct {
	ConceptID string
	Agent     string
	Checks    []hygiene.Check
	Findings  []hygiene.Finding
}

func pickAgent(set map[string]struct{}) string {
	best, bestP := "", 1<<30
	for a := range set {
		if p := agentPriority[a]; p < bestP {
			best, bestP = a, p
		}
	}
	return best
}

func sortedChecks(set map[hygiene.Check]struct{}) []hygiene.Check {
	out := make([]hygiene.Check, 0, len(set))
	for c := range set {
		out = append(out, c)
	}
	slices.Sort(out)
	return out
}

// Plan is the offline routing preview: scan + route only — no agents, no
// database. It shows which agent each flagged concept would be handed to, so a
// curate run can be reviewed before any provider turn is spent.
func Plan(bundle string) (Outcome, error) {
	concepts, err := okf.ReadBundle(bundle)
	if err != nil {
		return Outcome{}, fmt.Errorf("read bundle %q: %w", bundle, err)
	}
	rep := hygiene.Scan(concepts)
	routes, skipped := route(rep)
	out := Outcome{Concepts: len(concepts), Findings: fixableCount(rep), Routed: len(routes), Items: skipped}
	for _, r := range routes {
		out.Items = append(out.Items, Item{ConceptID: r.ConceptID, Agent: r.Agent, Checks: r.Checks, Status: StatusPlanned})
	}
	return out, nil
}

// Run executes one curate pass. It never panics on a single agent failure: that
// concept is marked StatusError and the loop continues.
func (c *Curator) Run(ctx context.Context) (Outcome, error) {
	concepts, err := okf.ReadBundle(c.Bundle)
	if err != nil {
		return Outcome{}, fmt.Errorf("read bundle %q: %w", c.Bundle, err)
	}
	rep := hygiene.Scan(concepts)
	byID := make(map[string]okf.Concept, len(concepts))
	for _, x := range concepts {
		byID[x.ID] = x
	}

	routes, skipped := route(rep)
	if c.Limit > 0 && len(routes) > c.Limit {
		routes = routes[:c.Limit]
	}
	out := Outcome{Concepts: len(concepts), Findings: fixableCount(rep), Routed: len(routes), Items: skipped}

	var accepted []okf.Concept
	for _, r := range routes {
		item := Item{ConceptID: r.ConceptID, Agent: r.Agent, Checks: r.Checks}
		orig := byID[r.ConceptID]

		proposed, err := c.Fixer.Fix(ctx, r.Agent, orig, r.Findings)
		if err != nil {
			item.Status, item.Detail = StatusError, err.Error()
			out.Errors++
			out.Items = append(out.Items, item)
			continue
		}
		mine := proposalFor(proposed, r.ConceptID)
		if mine == nil {
			item.Status, item.Detail = StatusNoChange, "agent proposed no replacement for this concept"
			out.Items = append(out.Items, item)
			continue
		}
		if bad := gate(concepts, *mine); len(bad) > 0 {
			item.Status = StatusRejected
			item.Detail = "gate: still " + strings.Join(checkLabels(bad), ", ")
			out.Rejected++
			out.Items = append(out.Items, item)
			continue
		}
		item.Detail = fixSummary(orig, *mine)
		if c.Apply {
			item.Status = StatusApplied
			accepted = append(accepted, *mine)
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
		res, err := c.Runner.UpsertBatch(ctx, accepted, "curate")
		if err != nil {
			return out, fmt.Errorf("upsert curated concepts: %w", err)
		}
		out.Commit = res.Commit
		if err := c.Runner.Reindex(ctx); err != nil {
			return out, fmt.Errorf("reindex after curate: %w", err)
		}
	}
	return out, nil
}

// gate re-scans the proposed concept in the context of the full set (its
// original swapped out by id) and returns the proposed concept's remaining
// ERROR findings. Empty => the fix is clean and may be written. This reuses the
// exact air-gap-pure detector that found the problem, so an agent can never
// write a concept that still deviates.
func gate(set []okf.Concept, proposed okf.Concept) []hygiene.Finding {
	swapped := make([]okf.Concept, 0, len(set)+1)
	replaced := false
	for _, x := range set {
		if x.ID == proposed.ID {
			swapped = append(swapped, proposed)
			replaced = true
		} else {
			swapped = append(swapped, x)
		}
	}
	if !replaced {
		swapped = append(swapped, proposed)
	}
	rep := hygiene.Scan(swapped)
	var bad []hygiene.Finding
	for _, f := range rep.Findings {
		if f.ConceptID == proposed.ID && f.Severity == hygiene.SeverityError {
			bad = append(bad, f)
		}
	}
	return bad
}

// fixSummary describes the agent's change for the dry-run report: a title
// rename when it changed, plus the new body size, so a reviewer sees WHAT a
// proposed fix does before any write.
func fixSummary(orig, fixed okf.Concept) string {
	if strings.TrimSpace(orig.Title) != strings.TrimSpace(fixed.Title) {
		return fmt.Sprintf("title %q -> %q (body %dB)", orig.Title, fixed.Title, len(fixed.Body))
	}
	return fmt.Sprintf("body %dB -> %dB (title kept)", len(orig.Body), len(fixed.Body))
}

func proposalFor(proposed []okf.Concept, id string) *okf.Concept {
	for i := range proposed {
		if proposed[i].ID == id {
			return &proposed[i]
		}
	}
	return nil
}

func fixableCount(rep hygiene.Report) int {
	n := 0
	for _, f := range rep.Findings {
		if f.Fixable {
			n++
		}
	}
	return n
}

func checkLabels(fs []hygiene.Finding) []string {
	out := make([]string, len(fs))
	for i, f := range fs {
		out[i] = string(f.Check)
	}
	return out
}

// extractJSON pulls the JSON object out of an agent reply that may wrap it in
// markdown code fences or surrounding prose (Claude Code, lacking a native
// output-schema flag, sometimes does this despite the no-fences instruction).
// It returns the substring from the first '{' to the last '}'.
func extractJSON(raw string) string {
	s := strings.TrimSpace(raw)
	if f := strings.Index(s, "```"); f >= 0 {
		// drop opening fence (```json or ```), keep content up to the closing fence
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

// conceptJSON mirrors the agents' conceptSchema output so a Fixer can parse an
// agent's structured reply into okf concepts.
type conceptJSON struct {
	Concepts []struct {
		ID        string   `json:"id"`
		Type      string   `json:"type"`
		Title     string   `json:"title"`
		Body      string   `json:"body"`
		Tags      []string `json:"tags"`
		Language  string   `json:"language"`
		SourceURI string   `json:"source_uri"`
	} `json:"concepts"`
}

// ParseConcepts turns an agent's conceptSchema JSON reply into okf concepts,
// applying the same defaults as the concept_upsert MCP verb (H1-prefix the body,
// default language pt). Exported so AgencyFixer and tests share one parser.
func ParseConcepts(raw string) ([]okf.Concept, error) {
	var doc conceptJSON
	if err := json.Unmarshal([]byte(extractJSON(raw)), &doc); err != nil {
		return nil, fmt.Errorf("parse agent reply: %w", err)
	}
	out := make([]okf.Concept, 0, len(doc.Concepts))
	for _, c := range doc.Concepts {
		if strings.TrimSpace(c.ID) == "" || strings.TrimSpace(c.Title) == "" {
			continue
		}
		body := c.Body
		if !strings.HasPrefix(strings.TrimSpace(body), "# ") {
			body = "# " + c.Title + "\n\n" + body
		}
		lang := c.Language
		if lang != "en" {
			lang = "pt"
		}
		out = append(out, okf.Concept{
			ID: c.ID, Type: c.Type, Title: c.Title, Description: c.Title,
			Tags: c.Tags, Language: lang, SourceURI: c.SourceURI, Body: body,
		})
	}
	return out, nil
}
