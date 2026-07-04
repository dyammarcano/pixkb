// Package kbmcp exposes pixkb's verbs as MCP tools so an agent's ONLY tool is
// pixkb: it queries (search/related/stats/concept_get), enriches
// (concept_upsert), and rebuilds (reindex) the KB autonomously through this
// single, self-contained surface — no shell, no web, no other tools.
package kbmcp

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"pixkb/internal/embed"
	"pixkb/internal/epoch"
	"pixkb/internal/okf"
	"pixkb/internal/query"
	"pixkb/internal/similar"
	"pixkb/internal/store/postgres"
	"pixkb/pkg/agents"
)

// Deps are the live KB dependencies the tools operate over.
type Deps struct {
	Store  *postgres.Store
	Emb    embed.Embedder
	Runner *epoch.Runner // may be nil for read-only servers
	Bundle string
	Agency *agents.Agency // may be nil; enables the kb_ask (RAG) tool when set
}

// Version is reported in the MCP server implementation handshake.
const Version = "v0.1.0"

// NewServer builds the pixkb MCP server with every KB verb registered as a
// tool. Read tools always register; write tools (concept_upsert, reindex)
// register only when a Runner is supplied.
func NewServer(d Deps) *mcp.Server {
	s := mcp.NewServer(&mcp.Implementation{Name: "pixkb", Version: Version}, nil)

	registerSearch(s, d)
	registerRelated(s, d)
	registerSimilar(s, d)
	registerStats(s, d)
	registerConceptGet(s, d)
	registerHygieneScan(s, d)
	registerQRRead(s)
	registerQRWrite(s)
	registerQRDecode(s)
	registerQRValidate(s)
	if d.Runner != nil {
		registerConceptUpsert(s, d)
		registerReindex(s, d)
	}
	if d.Agency != nil {
		registerAsk(s, d)
	}
	return s
}

type explainOut struct {
	FTSRank    int     `json:"fts_rank"`
	VecRank    int     `json:"vec_rank"`
	VecScore   float64 `json:"vec_score"`
	TypeWeight float64 `json:"type_weight"`
	TitleBoost float64 `json:"title_boost"`
	FinalScore float64 `json:"final_score"`
	Arm        string  `json:"arm"`
}

type hitOut struct {
	ID      string      `json:"id"`
	Title   string      `json:"title"`
	Type    string      `json:"type"`
	Score   float64     `json:"score"`
	Rank    int         `json:"rank"`
	Explain *explainOut `json:"explain,omitempty"`
}

type searchIn struct {
	Query   string `json:"query" jsonschema:"natural-language or lexical query"`
	Type    string `json:"type,omitempty" jsonschema:"optional concept-type filter (ApiEndpoint, ManualSection, PacsMessage, ...)"`
	Limit   int    `json:"limit,omitempty" jsonschema:"max hits (default 10)"`
	Mode    string `json:"mode,omitempty" jsonschema:"retrieval mode: hybrid (default) or multi (expands the query into several deterministic subqueries for broader recall)"`
	Explain bool   `json:"explain,omitempty" jsonschema:"include per-hit ranking explanation (FTS/vector rank, scores, boosts, arm) — only supported with the default hybrid mode; ignored (best-effort) otherwise"`
}
type searchOut struct {
	Hits []hitOut `json:"hits"`
}

func registerSearch(s *mcp.Server, d Deps) {
	mcp.AddTool(s, &mcp.Tool{
		Name:        "search",
		Description: "Hybrid (lexical + vector) search over the Pix/SPB knowledge base. Returns ranked concept hits. mode=multi broadens recall via deterministic query expansion.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in searchIn) (*mcp.CallToolResult, searchOut, error) {
		limit := in.Limit
		if limit <= 0 {
			limit = 10
		}
		f := postgres.Filter{Type: in.Type, Limit: limit}
		var hits []postgres.Hit
		var explains []query.Explain
		var err error
		switch in.Mode {
		case "multi":
			var mh []query.MultiHit
			if mh, err = query.MultiHybrid(ctx, d.Store, d.Emb, in.Query, f); err == nil {
				hits = query.Hits(mh)
			}
		case "", "hybrid":
			if in.Explain {
				hits, explains, err = query.HybridExplain(ctx, d.Store, d.Emb, in.Query, f)
			} else {
				hits, err = query.Hybrid(ctx, d.Store, d.Emb, in.Query, f)
			}
		default:
			hits, err = query.Hybrid(ctx, d.Store, d.Emb, in.Query, f)
		}
		if err != nil {
			return nil, searchOut{}, err
		}
		out := searchOut{Hits: make([]hitOut, 0, len(hits))}
		for i, h := range hits {
			ho := hitOut{ID: h.ID, Title: h.Title, Type: h.Type, Score: h.Score, Rank: h.Rank}
			if i < len(explains) {
				e := explains[i]
				ho.Explain = &explainOut{
					FTSRank: e.FTSRank, VecRank: e.VecRank, VecScore: e.VecScore,
					TypeWeight: e.TypeWeight, TitleBoost: e.TitleBoost,
					FinalScore: e.FinalScore, Arm: e.Arm,
				}
			}
			out.Hits = append(out.Hits, ho)
		}
		return textResult(fmt.Sprintf("%d hits for %q", len(out.Hits), in.Query)), out, nil
	})
}

type relIn struct {
	ID string `json:"id" jsonschema:"concept id (bundle-relative path)"`
}
type relOut struct {
	Related []postgres.RelatedConcept `json:"related"`
}

func registerRelated(s *mcp.Server, d Deps) {
	mcp.AddTool(s, &mcp.Tool{
		Name:        "related",
		Description: "Graph neighbours of a concept (cross-links). Use to explore the concept graph.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in relIn) (*mcp.CallToolResult, relOut, error) {
		rel, err := d.Store.Related(ctx, in.ID)
		if err != nil {
			return nil, relOut{}, err
		}
		return textResult(fmt.Sprintf("%d neighbours of %s", len(rel), in.ID)), relOut{Related: rel}, nil
	})
}

type similarIn struct {
	ID           string `json:"id" jsonschema:"concept id (bundle-relative path) to find similar concepts for"`
	Mode         string `json:"mode,omitempty" jsonschema:"semantic|graph|hybrid (default)|more-like-this"`
	Type         string `json:"type,omitempty" jsonschema:"optional concept-type filter on results"`
	Limit        int    `json:"limit,omitempty" jsonschema:"max hits (default 20)"`
	// ExcludeGraph, not IncludeGraph: plain JSON bools can't distinguish
	// "omitted" from "explicitly false", so the field is named/phrased so its
	// zero value (false, or omitted entirely) IS the desired default — hybrid
	// mode includes graph neighbours unless a caller explicitly opts out.
	ExcludeGraph bool `json:"exclude_graph,omitempty" jsonschema:"hybrid mode: set true to exclude direct graph neighbours (default: graph included)"`
}
type similarHitOut struct {
	ID    string   `json:"id"`
	Title string   `json:"title"`
	Type  string   `json:"type"`
	Score float64  `json:"score"`
	Rank  int      `json:"rank"`
	Why   []string `json:"why"`
}
type similarOut struct {
	Hits []similarHitOut `json:"hits"`
}

// registerSimilar exposes concept-to-concept similarity: given a known
// concept id, ranked nearby concepts tagged with why each one matched
// (semantic/lexical/graph/domain). Mirrors registerSearch's shape.
func registerSimilar(s *mcp.Server, d Deps) {
	mcp.AddTool(s, &mcp.Tool{
		Name:        "similar",
		Description: "Find concepts similar to a known concept id, tagged with why each result matched (semantic, lexical, graph, domain). Modes: semantic, graph, hybrid (default), more-like-this.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in similarIn) (*mcp.CallToolResult, similarOut, error) {
		mode := in.Mode
		if mode == "" {
			mode = "hybrid"
		}
		limit := in.Limit
		if limit <= 0 {
			limit = 20
		}
		opts := similar.Options{
			Mode:         mode,
			IncludeGraph: !in.ExcludeGraph,
			Filter:       postgres.Filter{Type: in.Type, Limit: limit},
		}
		hits, err := similar.Similar(ctx, d.Store, d.Emb, d.Bundle, in.ID, opts)
		if err != nil {
			return nil, similarOut{}, err
		}
		out := similarOut{Hits: make([]similarHitOut, 0, len(hits))}
		for _, h := range hits {
			out.Hits = append(out.Hits, similarHitOut{ID: h.ID, Title: h.Title, Type: h.Type, Score: h.Score, Rank: h.Rank, Why: h.Why})
		}
		return textResult(fmt.Sprintf("%d concepts similar to %s", len(out.Hits), in.ID)), out, nil
	})
}

func registerStats(s *mcp.Server, d Deps) {
	mcp.AddTool(s, &mcp.Tool{
		Name:        "stats",
		Description: "KB health: concept/embedding/epoch counts and concepts-by-type. Use to verify the KB state.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, _ any) (*mcp.CallToolResult, postgres.Stats, error) {
		st, err := d.Store.Stats(ctx)
		if err != nil {
			return nil, postgres.Stats{}, err
		}
		return textResult(fmt.Sprintf("%d concepts, %d embeddings, %d epochs", st.Concepts, st.Embeddings, st.Epochs)), st, nil
	})
}

type conceptGetIn struct {
	ID string `json:"id" jsonschema:"concept id (bundle-relative path, e.g. api/openapi/post-cob.md)"`
}
type conceptGetOut struct {
	ID       string `json:"id"`
	Markdown string `json:"markdown"`
}

func registerConceptGet(s *mcp.Server, d Deps) {
	mcp.AddTool(s, &mcp.Tool{
		Name:        "concept_get",
		Description: "Read a concept's full markdown (frontmatter + body) from the canonical bundle.",
	}, func(_ context.Context, _ *mcp.CallToolRequest, in conceptGetIn) (*mcp.CallToolResult, conceptGetOut, error) {
		b, err := os.ReadFile(filepath.Join(d.Bundle, filepath.FromSlash(in.ID)))
		if err != nil {
			return nil, conceptGetOut{}, fmt.Errorf("read concept %q: %w", in.ID, err)
		}
		return textResult(fmt.Sprintf("concept %s (%d bytes)", in.ID, len(b))), conceptGetOut{ID: in.ID, Markdown: string(b)}, nil
	})
}

type conceptIn struct {
	ID          string   `json:"id" jsonschema:"bundle-relative id, e.g. reference/<doc>/<n>.md"`
	Type        string   `json:"type" jsonschema:"concept type, e.g. Reference, ManualSection, ApiEndpoint"`
	Title       string   `json:"title" jsonschema:"specific, meaningful title (never a fragment)"`
	Body        string   `json:"body" jsonschema:"markdown body (technical detail preserved)"`
	Tags        []string `json:"tags,omitempty"`
	Language    string   `json:"language,omitempty" jsonschema:"pt or en"`
	SourceURI   string   `json:"source_uri,omitempty" jsonschema:"provenance URL/URI (required for trust)"`
	IntentTerms string   `json:"intent_terms,omitempty" jsonschema:"agent-generated recall terms (synonyms, alternate phrasings) — indexed for search, NOT shown in the body"`
}
type upsertIn struct {
	Concepts []conceptIn `json:"concepts" jsonschema:"concepts to write back into pixdb"`
	Source   string      `json:"source,omitempty" jsonschema:"epoch source label (default agent-upsert)"`
}
type upsertOut struct {
	Epoch   int    `json:"epoch"`
	Changed int    `json:"changed"`
	Commit  string `json:"commit"`
}

func registerConceptUpsert(s *mcp.Server, d Deps) {
	mcp.AddTool(s, &mcp.Tool{
		Name:        "concept_upsert",
		Description: "Write agent-curated concepts back into pixdb (bundle + index) as a non-destructive epoch. The enrich verb.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in upsertIn) (*mcp.CallToolResult, upsertOut, error) {
		concepts := make([]okf.Concept, 0, len(in.Concepts))
		for _, c := range in.Concepts {
			oc, err := c.toConcept()
			if err != nil {
				return nil, upsertOut{}, err
			}
			concepts = append(concepts, oc)
		}
		src := in.Source
		if src == "" {
			src = "agent-upsert"
		}
		res, err := d.Runner.UpsertBatch(ctx, concepts, src)
		if err != nil {
			return nil, upsertOut{}, err
		}
		out := upsertOut{Epoch: res.Epoch, Changed: res.Changed, Commit: res.Commit}
		return textResult(fmt.Sprintf("epoch %d: ~%d commit %.7s", out.Epoch, out.Changed, out.Commit)), out, nil
	})
}

func (c conceptIn) toConcept() (okf.Concept, error) {
	if strings.TrimSpace(c.ID) == "" || strings.TrimSpace(c.Type) == "" ||
		strings.TrimSpace(c.Title) == "" || strings.TrimSpace(c.Body) == "" {
		return okf.Concept{}, fmt.Errorf("concept %q: id, type, title and body are required", c.ID)
	}
	body := c.Body
	if !strings.HasPrefix(strings.TrimSpace(body), "# ") {
		body = "# " + c.Title + "\n\n" + body
	}
	lang := c.Language
	if lang != "en" {
		lang = "pt"
	}
	return okf.Concept{
		ID:          c.ID,
		Type:        c.Type,
		Title:       c.Title,
		Description: c.Title,
		Tags:        c.Tags,
		Language:    lang,
		SourceURI:   c.SourceURI,
		IntentTerms: c.IntentTerms,
		Body:        body,
		ContentSHA:  okf.ComputeSHA(body),
	}, nil
}

type reindexOut struct {
	Status string `json:"status"`
}

func registerReindex(s *mcp.Server, d Deps) {
	mcp.AddTool(s, &mcp.Tool{
		Name:        "reindex",
		Description: "Rebuild the derived index from the canonical bundle. Use to verify/repair after write-back.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, _ any) (*mcp.CallToolResult, reindexOut, error) {
		if err := d.Runner.Reindex(ctx); err != nil {
			return nil, reindexOut{}, err
		}
		return textResult("reindex complete"), reindexOut{Status: "ok"}, nil
	})
}

func textResult(msg string) *mcp.CallToolResult {
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: msg}}}
}
