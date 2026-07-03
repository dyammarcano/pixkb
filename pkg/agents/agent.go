package agents

// Kind classifies an agent's role in the KB lifecycle.
type Kind string

const (
	KindControl       Kind = "control"       // orchestrates the other agents
	KindGather        Kind = "gather"        // runs deterministic source adapters
	KindScraper       Kind = "scraper"       // fetches/renders web pages (incl. JS SPAs)
	KindNormalization Kind = "normalization" // raw text -> clean OKF concepts
	KindQuality       Kind = "quality"       // scores concept quality, flags weak ones
	KindGovernance    Kind = "governance"    // enforces OKF/provenance rules, gates the bundle
	KindResearch      Kind = "research"      // fills gaps surfaced by the judge
	KindJudge         Kind = "judge"         // evaluates search quality (relevance/precision)
	KindDiagram       Kind = "diagram"       // renders BACEN flows as mermaid/drawio
	KindHygiene       Kind = "hygiene"       // fixes mechanical KB problems (junk titles, links, dups)
	KindDeviation     Kind = "deviation"     // corrects BACEN-charter deviations (strips implementation specifics)
	KindEnrich        Kind = "enrich"        // generates intent_terms (recall synonyms) for un-enriched concepts
	KindAnswerer      Kind = "answerer"      // RAG: synthesizes a cited answer strictly from retrieved context
)

// Agent is a declarative agent definition: identity, role, model hint, allowed
// tools, system prompt, and an optional JSON-Schema for structured output. The
// Agency runs an Agent by handing it to a Provider; the definition itself holds
// no runtime state, so the roster can be assembled at init() time.
type Agent struct {
	Name        string   // unique, kebab-case (e.g. "judge", "scraper")
	Kind        Kind     // lifecycle role
	Description string   // one line, shown by `pixkb agents list`
	Model       string   // provider model hint ("" = provider default)
	Tools       []string // tool names the agent may use (provider-dependent)
	System      string   // system prompt / role definition
	Schema      string   // JSON Schema for structured output ("" = freeform text)
}
