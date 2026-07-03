// Package hygiene is the deterministic, air-gap-pure KB health engine: it scans
// OKF concepts for problems an agent should fix (junk titles, stub bodies,
// duplicates, broken cross-links, missing provenance) and — crucially — for
// BACEN-charter DEVIATIONS: implementation-specific content that violates the
// normative-only scope (app/service names, brokers, infra, DB schemas, internal
// IDs).
//
// It needs no LLM and no network, so it runs anywhere. It is used two ways:
//   - TRIGGER: scan the existing bundle to find what the curate loop must fix.
//   - GATE: re-scan a concept an agent PROPOSES, before write-back, so a fix can
//     never introduce a new deviation. Same detector, both directions.
package hygiene

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"pixkb/internal/okf"
)

// Severity grades a finding. Error = must fix/block; Warn = should fix.
type Severity string

const (
	SeverityError Severity = "error"
	SeverityWarn  Severity = "warn"
)

// Check identifies the rule that fired.
type Check string

const (
	CheckDeviation   Check = "deviation"    // BACEN-charter violation (implementation-specific)
	CheckJunkTitle   Check = "junk-title"   // fragment / non-meaningful title
	CheckStubBody    Check = "stub-body"    // empty or near-empty body
	CheckDuplicate   Check = "duplicate"    // same content or identical title as another concept
	CheckBrokenLink  Check = "broken-link"  // cross-link to a non-existent concept id
	CheckMissingProv Check = "missing-prov" // no source_uri (provenance)
	CheckMissingType Check = "missing-type" // no concept type
	CheckSampleData  Check = "sample-data"  // OCR example fragment (placeholder name/taxid), not a real concept

	// CheckMissingIntentTerms flags a concept with no intent_terms (recall
	// synonyms / alternate phrasings). It is DELIBERATELY kept out of the default
	// Scan — it is not a hygiene defect, it is an enrichment opportunity — and is
	// surfaced only by MissingIntentTerms for the curate enrich loop, so routine
	// hygiene sweeps are not flooded with one finding per un-enriched concept.
	CheckMissingIntentTerms Check = "missing-intent-terms"
)

// Finding is one problem with one concept.
type Finding struct {
	Check     Check    `json:"check"`
	ConceptID string   `json:"concept_id"`
	Severity  Severity `json:"severity"`
	Detail    string   `json:"detail"`
	// Fixable: an agent can repair this in place (rewrite). When false the
	// finding needs human/agent judgment to drop or supply data (e.g. provenance
	// cannot be invented).
	Fixable bool `json:"fixable"`
}

// Report is the full scan result.
type Report struct {
	Concepts int       `json:"concepts"`
	Findings []Finding `json:"findings"`
}

// Errors returns the error-severity findings (the must-fix / block set).
func (r Report) Errors() []Finding {
	var out []Finding
	for _, f := range r.Findings {
		if f.Severity == SeverityError {
			out = append(out, f)
		}
	}
	return out
}

// Clean reports whether there are zero error-severity findings — the gate
// predicate for write-back.
func (r Report) Clean() bool { return len(r.Errors()) == 0 }

const stubBodyMinChars = 40

// deviationPatterns are high-precision markers of implementation-specific
// content the BACEN charter forbids (OUT OF SCOPE). Kept deliberately specific
// to avoid false positives on legitimate normative prose. Each entry: a label
// and a compiled, case-insensitive, word-bounded pattern.
var deviationPatterns = func() []struct {
	label string
	re    *regexp.Regexp
} {
	raw := []struct{ label, pat string }{
		{"message broker", `\b(pulsar|kafka|rabbitmq|activemq)\b`},
		{"broker topic", `\btopic[:=]\s*\S`},
		{"deploy/infra", `\b(argocd|kubernetes|k8s|kustomize|helm chart|dockerfile)\b`},
		{"k8s namespace", `\bnamespace[:=]\s*\S`},
		{"microservice naming", `\b[a-z0-9]+-(svc|service|go)-[a-z0-9-]+\b`},
		{"internal correlation id", `\b(correlation[_-]?id|trace[_-]?id|span[_-]?id)\b`},
		{"SQL/db schema", `\b(select\s+.+\s+from|insert\s+into|create\s+table|alter\s+table)\b`},
		{"db column ref", `\b(table|column)\s+["` + "`" + `][a-z_]+["` + "`" + `]`},
		{"company proto pkg", `\b[a-z]+\.[a-z]+\.v[0-9]+\.[A-Z][A-Za-z]+(Request|Response|Service)\b`},
	}
	out := make([]struct {
		label string
		re    *regexp.Regexp
	}, 0, len(raw))
	for _, r := range raw {
		out = append(out, struct {
			label string
			re    *regexp.Regexp
		}{r.label, regexp.MustCompile(`(?i)` + r.pat)})
	}
	return out
}()

// sampleDataPatterns catch concepts that are OCR'd EXAMPLE fragments rather than
// normative content: a worked screenshot whose "title" is a placeholder merchant
// or person name and whose body is a payment-screen capture. These pass the
// junk-title and stub checks (proper-noun title, body over threshold) yet pollute
// search by lexically matching real topics. Kept high-precision: classic
// Brazilian placeholder names + sample tax-id literals.
var sampleDataPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)\b(fulano|beltrano|sicrano)\b`),     // placeholder person names
	regexp.MustCompile(`(?i)\bde tal\b`),                        // "Fulano DE TAL"
	regexp.MustCompile(`\b00[.\s]?123[.\s]?456`),                // sample CNPJ stem 00.123.456/...
	regexp.MustCompile(`\b0{3}[.\s]?0{3}[.\s]?0{3}[-\s]?0{2}\b`), // 000.000.000-00 placeholder CPF
	regexp.MustCompile(`\b0{2}[.\s]?0{3}[.\s]?0{3}/0{4}[-\s]?0{2}\b`), // 00.000.000/0000-00 placeholder CNPJ
}

// junkTitlePatterns catch fragment / non-meaningful titles (noisy PDF headers).
var junkTitlePatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)^anexo\b`),              // "ANEXO IV"
	regexp.MustCompile(`^[0-9.\s]+$`),               // page/section numbers only
	regexp.MustCompile(`(?i)\b(é|de|da|do|e|a|o)$`), // ends on a stop-word fragment ("CONCLUÍDA é")
	regexp.MustCompile(`(?i)^(figura|tabela|quadro)\s+\d+$`),
}

// Scan runs every check over the concept set and returns a Report. Findings are
// sorted error-first, then by check, then by concept id — deterministic output.
func Scan(concepts []okf.Concept) Report {
	rep := Report{Concepts: len(concepts)}
	ids := make(map[string]struct{}, len(concepts))
	for _, c := range concepts {
		ids[c.ID] = struct{}{}
	}
	shaSeen := map[string]string{}   // content_sha -> first id
	titleSeen := map[string]string{} // lower(title) -> first id

	for _, c := range concepts {
		rep.Findings = append(rep.Findings, scanConcept(c, ids, shaSeen, titleSeen)...)
	}

	sort.SliceStable(rep.Findings, func(i, j int) bool {
		a, b := rep.Findings[i], rep.Findings[j]
		if a.Severity != b.Severity {
			return a.Severity == SeverityError // errors first
		}
		if a.Check != b.Check {
			return a.Check < b.Check
		}
		return a.ConceptID < b.ConceptID
	})
	return rep
}

// scanConcept runs the single-concept checks. shaSeen/titleSeen are shared maps
// so duplicates across the set are caught on the second sighting.
func scanConcept(c okf.Concept, ids map[string]struct{}, shaSeen, titleSeen map[string]string) []Finding {
	var fs []Finding
	add := func(ch Check, sev Severity, fixable bool, detail string) {
		fs = append(fs, Finding{Check: ch, ConceptID: c.ID, Severity: sev, Fixable: fixable, Detail: detail})
	}

	// Deviation (BACEN charter) — scan title + body.
	hay := c.Title + "\n" + c.Body
	for _, p := range deviationPatterns {
		if m := p.re.FindString(hay); m != "" {
			add(CheckDeviation, SeverityError, true,
				fmt.Sprintf("implementation-specific (%s): %q — strip to the BACEN-canonical concept", p.label, m))
		}
	}

	if strings.TrimSpace(c.Type) == "" {
		add(CheckMissingType, SeverityError, false, "concept has no type")
	}

	// Sample-data / OCR example fragment — scan title + description (where the
	// placeholder merchant/taxid lands) so worked-example captures are flagged.
	meta := c.Title + "\n" + c.Description
	for _, p := range sampleDataPatterns {
		if m := p.FindString(meta); m != "" {
			add(CheckSampleData, SeverityWarn, true,
				fmt.Sprintf("OCR example fragment (placeholder %q) — retitle to the normative topic or drop", m))
			break
		}
	}

	// Junk title.
	title := strings.TrimSpace(c.Title)
	switch {
	case title == "":
		add(CheckJunkTitle, SeverityError, true, "empty title")
	case len(title) < 4:
		add(CheckJunkTitle, SeverityWarn, true, fmt.Sprintf("title too short: %q", title))
	case isAllCaps(title) && len(strings.Fields(title)) <= 3:
		add(CheckJunkTitle, SeverityWarn, true, fmt.Sprintf("all-caps fragment title: %q", title))
	default:
		for _, p := range junkTitlePatterns {
			if p.MatchString(title) {
				add(CheckJunkTitle, SeverityWarn, true, fmt.Sprintf("fragment title: %q", title))
				break
			}
		}
	}

	// Stub body (strip a leading markdown H1 before measuring).
	if n := len(strings.TrimSpace(stripH1(c.Body))); n < stubBodyMinChars {
		add(CheckStubBody, SeverityWarn, true, fmt.Sprintf("body too thin (%d chars)", n))
	}

	// Missing provenance.
	if strings.TrimSpace(c.SourceURI) == "" {
		add(CheckMissingProv, SeverityWarn, false, "no source_uri (provenance)")
	}

	// Broken cross-links.
	for _, l := range okf.ParseLinks(c.Body) {
		if _, ok := ids[l]; !ok {
			add(CheckBrokenLink, SeverityWarn, true, fmt.Sprintf("link to missing concept %q", l))
		}
	}

	// Duplicates (second sighting reports against this concept).
	if c.ContentSHA != "" {
		if first, ok := shaSeen[c.ContentSHA]; ok {
			add(CheckDuplicate, SeverityWarn, true, fmt.Sprintf("identical body to %q (same content_sha)", first))
		} else {
			shaSeen[c.ContentSHA] = c.ID
		}
	}
	if t := strings.ToLower(title); t != "" {
		if first, ok := titleSeen[t]; ok && first != c.ID {
			add(CheckDuplicate, SeverityWarn, true, fmt.Sprintf("identical title to %q", first))
		} else if !ok {
			titleSeen[t] = c.ID
		}
	}
	return fs
}

// MissingIntentTerms returns one WARN/fixable finding per concept that has no
// intent_terms (the FTS-woven recall synonyms / alternate phrasings, migration
// 0003 / ADR 0001). It is SEPARATE from Scan on purpose: an empty intent_terms
// is an enrichment opportunity, not a hygiene defect, so it must not surface in
// routine hygiene sweeps or block the write-back gate. The curate enrich loop
// uses it to route un-enriched concepts to the enrich agent. Output is sorted by
// concept id for deterministic batching.
func MissingIntentTerms(concepts []okf.Concept) []Finding {
	var out []Finding
	for _, c := range concepts {
		if strings.TrimSpace(c.IntentTerms) == "" {
			out = append(out, Finding{
				Check:     CheckMissingIntentTerms,
				ConceptID: c.ID,
				Severity:  SeverityWarn,
				Fixable:   true,
				Detail:    "no intent_terms (recall synonyms / alternate phrasings) — generate from title+body",
			})
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].ConceptID < out[j].ConceptID })
	return out
}

// IntentTermsDeviations scans a concept's intent_terms string for BACEN-charter
// deviations (implementation specifics: brokers, infra, DB schemas, internal ids)
// and returns ERROR findings for any match. The default Scan only looks at
// title+body, so recall terms generated by the enrich agent are NOT otherwise
// gated — this is the enrich loop's charter gate, ensuring a synonym list can
// never smuggle implementation detail into the FTS index.
func IntentTermsDeviations(c okf.Concept) []Finding {
	var fs []Finding
	terms := c.IntentTerms
	if strings.TrimSpace(terms) == "" {
		return fs
	}
	for _, p := range deviationPatterns {
		if m := p.re.FindString(terms); m != "" {
			fs = append(fs, Finding{
				Check: CheckDeviation, ConceptID: c.ID, Severity: SeverityError, Fixable: true,
				Detail: fmt.Sprintf("intent_terms carries implementation-specific (%s): %q", p.label, m),
			})
		}
	}
	return fs
}

func isAllCaps(s string) bool {
	hasLetter := false
	for _, r := range s {
		if r >= 'a' && r <= 'z' {
			return false
		}
		if r >= 'A' && r <= 'Z' {
			hasLetter = true
		}
	}
	return hasLetter
}

// stripH1 removes a leading "# Title" line so body-length checks measure content.
func stripH1(body string) string {
	s := strings.TrimLeft(body, "\n ")
	if rest, ok := strings.CutPrefix(s, "# "); ok {
		if _, after, found := strings.Cut(rest, "\n"); found {
			return after
		}
		return ""
	}
	return body
}
