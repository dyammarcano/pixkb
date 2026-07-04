package kbmcp

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"pixkb/internal/embed"
	"pixkb/internal/epoch"
	"pixkb/internal/store/postgres"
)

// TestServerReadTools exercises the read tools (stats, search) over an
// in-memory MCP transport against a live KB. Skipped without a DSN or under
// -short. Only reads — no Runner, so no writes touch the database.
func TestServerReadTools(t *testing.T) {
	if testing.Short() {
		t.Skip("needs a live Postgres KB")
	}
	dsn := os.Getenv("PIXKB_TEST_DSN")
	if dsn == "" {
		dsn = os.Getenv("PIXKB_DSN")
	}
	if dsn == "" {
		t.Skip("no PIXKB_TEST_DSN/PIXKB_DSN set")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	st, err := postgres.Open(ctx, dsn)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer st.Close()

	srv := NewServer(Deps{Store: st, Emb: embed.NewHashing(256), Bundle: "../../kb-data"})

	serverTr, clientTr := mcp.NewInMemoryTransports()
	if _, err := srv.Connect(ctx, serverTr, nil); err != nil {
		t.Fatalf("server connect: %v", err)
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "0"}, nil)
	cs, err := client.Connect(ctx, clientTr, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	defer func() { _ = cs.Close() }()

	// Tools are listed.
	tools, err := cs.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("list tools: %v", err)
	}
	if len(tools.Tools) < 4 {
		t.Fatalf("want >=4 tools, got %d", len(tools.Tools))
	}

	// stats
	res, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "stats", Arguments: map[string]any{}})
	if err != nil || res.IsError {
		t.Fatalf("stats call: err=%v isErr=%v", err, res.IsError)
	}

	// search
	res, err = cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "search",
		Arguments: map[string]any{"query": "criar cobrança imediata", "limit": 3},
	})
	if err != nil || res.IsError {
		t.Fatalf("search call: err=%v isErr=%v", err, res.IsError)
	}
	if len(res.Content) == 0 {
		t.Fatal("search returned no content")
	}
}

// TestServerSearch_MultiMode exercises search's mode="multi" over an
// in-memory MCP transport against a live KB. Skipped without a DSN or under
// -short, same as TestServerReadTools (read-only, no Runner needed).
func TestServerSearch_MultiMode(t *testing.T) {
	if testing.Short() {
		t.Skip("needs a live Postgres KB")
	}
	dsn := os.Getenv("PIXKB_TEST_DSN")
	if dsn == "" {
		dsn = os.Getenv("PIXKB_DSN")
	}
	if dsn == "" {
		t.Skip("no PIXKB_TEST_DSN/PIXKB_DSN set")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	st, err := postgres.Open(ctx, dsn)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer st.Close()

	srv := NewServer(Deps{Store: st, Emb: embed.NewHashing(256), Bundle: "../../kb-data"})
	serverTr, clientTr := mcp.NewInMemoryTransports()
	if _, err := srv.Connect(ctx, serverTr, nil); err != nil {
		t.Fatalf("server connect: %v", err)
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "0"}, nil)
	cs, err := client.Connect(ctx, clientTr, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	defer func() { _ = cs.Close() }()

	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "search",
		Arguments: map[string]any{"query": "como estornar um pix que recebi por engano", "mode": "multi", "limit": 5},
	})
	if err != nil || res.IsError {
		t.Fatalf("search (multi) call: err=%v isErr=%v", err, res.IsError)
	}
	if len(res.Content) == 0 {
		t.Fatal("search (multi) returned no content")
	}
}

// TestServerSearch_AsOfEpoch exercises search's as_of_epoch param over an
// in-memory MCP transport against a live KB. Skipped without a DSN or under
// -short, same as TestServerReadTools (read-only, no Runner needed).
func TestServerSearch_AsOfEpoch(t *testing.T) {
	if testing.Short() {
		t.Skip("needs a live Postgres KB")
	}
	dsn := os.Getenv("PIXKB_TEST_DSN")
	if dsn == "" {
		dsn = os.Getenv("PIXKB_DSN")
	}
	if dsn == "" {
		t.Skip("no PIXKB_TEST_DSN/PIXKB_DSN set")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	st, err := postgres.Open(ctx, dsn)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer st.Close()

	srv := NewServer(Deps{Store: st, Emb: embed.NewHashing(256), Bundle: "../../kb-data"})
	serverTr, clientTr := mcp.NewInMemoryTransports()
	if _, err := srv.Connect(ctx, serverTr, nil); err != nil {
		t.Fatalf("server connect: %v", err)
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "0"}, nil)
	cs, err := client.Connect(ctx, clientTr, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	defer func() { _ = cs.Close() }()

	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "search",
		Arguments: map[string]any{"query": "criar cobrança imediata", "as_of_epoch": 0, "limit": 3},
	})
	if err != nil || res.IsError {
		t.Fatalf("search (as_of_epoch) call: err=%v isErr=%v", err, res.IsError)
	}
	if len(res.Content) == 0 {
		t.Fatal("search (as_of_epoch) returned no content")
	}
}

// TestServerSearch_ExplainMode exercises search's explain=true param over an
// in-memory MCP transport against a live KB. Skipped without a DSN or under
// -short, same as TestServerReadTools (read-only, no Runner needed).
func TestServerSearch_ExplainMode(t *testing.T) {
	if testing.Short() {
		t.Skip("needs a live Postgres KB")
	}
	dsn := os.Getenv("PIXKB_TEST_DSN")
	if dsn == "" {
		dsn = os.Getenv("PIXKB_DSN")
	}
	if dsn == "" {
		t.Skip("no PIXKB_TEST_DSN/PIXKB_DSN set")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	st, err := postgres.Open(ctx, dsn)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer st.Close()

	srv := NewServer(Deps{Store: st, Emb: embed.NewHashing(256), Bundle: "../../kb-data"})
	serverTr, clientTr := mcp.NewInMemoryTransports()
	if _, err := srv.Connect(ctx, serverTr, nil); err != nil {
		t.Fatalf("server connect: %v", err)
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "0"}, nil)
	cs, err := client.Connect(ctx, clientTr, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	defer func() { _ = cs.Close() }()

	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "search",
		Arguments: map[string]any{"query": "criar cobrança imediata", "explain": true, "limit": 3},
	})
	if err != nil || res.IsError {
		t.Fatalf("search (explain) call: err=%v isErr=%v", err, res.IsError)
	}
	if len(res.Content) == 0 {
		t.Fatal("search (explain) returned no content")
	}
}

// TestServerSimilar_HybridMode exercises the similar tool over an in-memory
// MCP transport against a live KB. Skipped without a DSN or under -short,
// same pattern as TestServerReadTools.
func TestServerSimilar_HybridMode(t *testing.T) {
	if testing.Short() {
		t.Skip("needs a live Postgres KB")
	}
	dsn := os.Getenv("PIXKB_TEST_DSN")
	if dsn == "" {
		dsn = os.Getenv("PIXKB_DSN")
	}
	if dsn == "" {
		t.Skip("no PIXKB_TEST_DSN/PIXKB_DSN set")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	st, err := postgres.Open(ctx, dsn)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer st.Close()

	srv := NewServer(Deps{Store: st, Emb: embed.NewHashing(256), Bundle: "../../kb-data"})
	serverTr, clientTr := mcp.NewInMemoryTransports()
	if _, err := srv.Connect(ctx, serverTr, nil); err != nil {
		t.Fatalf("server connect: %v", err)
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "0"}, nil)
	cs, err := client.Connect(ctx, clientTr, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	defer func() { _ = cs.Close() }()

	// Search first to get a real concept id to query similarity against.
	sres, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "search", Arguments: map[string]any{"query": "criar cobrança imediata", "limit": 1}})
	if err != nil || sres.IsError {
		t.Fatalf("search call: err=%v isErr=%v", err, sres.IsError)
	}

	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "similar",
		Arguments: map[string]any{"id": "api/openapi/post-cob.md", "mode": "hybrid", "limit": 5},
	})
	if err != nil || res.IsError {
		t.Fatalf("similar call: err=%v isErr=%v", err, res.IsError)
	}
	if len(res.Content) == 0 {
		t.Fatal("similar returned no content")
	}
}

// testWriteStore opens a throwaway store and truncates it. Guards the prod KB.
func testWriteStore(t *testing.T) (*postgres.Store, string) {
	t.Helper()
	if testing.Short() {
		t.Skip("needs a live Postgres KB")
	}
	dsn := os.Getenv("PIXKB_TEST_DSN")
	if dsn == "" {
		t.Skip("no PIXKB_TEST_DSN set (write test needs a throwaway DB)")
	}
	if prod := os.Getenv("PIXKB_DSN"); prod != "" && prod == dsn {
		t.Fatal("PIXKB_TEST_DSN equals PIXKB_DSN (prod KB) — use a throwaway database")
	}
	ctx := context.Background()
	st, err := postgres.Open(ctx, dsn)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	if err := st.Truncate(ctx); err != nil {
		t.Fatalf("truncate: %v", err)
	}
	t.Cleanup(st.Close)
	return st, dsn
}

// TestServerUpsertSearchRoundTrip is the agent write-back e2e: an agent calls
// concept_upsert over MCP to enrich pixdb, then search finds the new concept —
// the gather/normalize -> write-back -> retrieve loop the fleet runs. Guarded by
// PIXKB_TEST_DSN (throwaway DB); skipped under -short.
func TestServerUpsertSearchRoundTrip(t *testing.T) {
	st, _ := testWriteStore(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	bundle := t.TempDir()
	runner := &epoch.Runner{
		Bundle: bundle, Store: st, Emb: embed.NewHashing(256),
		Git: epoch.NewGitCommitter(bundle),
	}
	srv := NewServer(Deps{Store: st, Emb: embed.NewHashing(256), Runner: runner, Bundle: bundle})

	serverTr, clientTr := mcp.NewInMemoryTransports()
	if _, err := srv.Connect(ctx, serverTr, nil); err != nil {
		t.Fatalf("server connect: %v", err)
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "0"}, nil)
	cs, err := client.Connect(ctx, clientTr, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	defer func() { _ = cs.Close() }()

	// concept_upsert: write a distinctive concept back into pixdb.
	const markerID = "reference/e2e/zephyr-marker.md"
	up, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "concept_upsert", Arguments: map[string]any{
		"concepts": []map[string]any{{
			"id":         markerID,
			"type":       "Reference",
			"title":      "Zephyr Marker Concept",
			"body":       "Distinctive token zephyrxyzmarker for the agent write-back round-trip.",
			"source_uri": "test:e2e",
		}},
		"source": "e2e-test",
	}})
	if err != nil || up.IsError {
		t.Fatalf("concept_upsert: err=%v isErr=%v", err, up.IsError)
	}

	// search: the upserted concept must be retrievable by its distinctive token.
	sr, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "search", Arguments: map[string]any{
		"query": "zephyrxyzmarker Zephyr Marker Concept", "limit": 5,
	}})
	if err != nil || sr.IsError {
		t.Fatalf("search: err=%v isErr=%v", err, sr.IsError)
	}

	// Decode the structured hits and assert the new concept is present.
	raw, err := json.Marshal(sr.StructuredContent)
	if err != nil {
		t.Fatalf("marshal structured: %v", err)
	}
	var out struct {
		Hits []struct {
			ID string `json:"id"`
		} `json:"hits"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("decode hits: %v", err)
	}
	found := false
	for _, h := range out.Hits {
		if h.ID == markerID {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("upserted concept %q not found in search hits: %s", markerID, raw)
	}

	// The concept also materialized in the bundle (the canonical source).
	if _, err := os.Stat(filepath.Join(bundle, filepath.FromSlash(markerID))); err != nil {
		t.Errorf("upserted concept not written to bundle: %v", err)
	}
}
