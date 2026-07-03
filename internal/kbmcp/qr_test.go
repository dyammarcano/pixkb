package kbmcp

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"pixkb/internal/brcode"
)

// qrTestClient wires an in-memory MCP client to a server with only the QR tools
// exercised. The QR tools are pure (no DB), so Deps can be empty and the test
// runs in -short without Postgres.
func qrTestClient(t *testing.T) (context.Context, *mcp.ClientSession) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)

	srv := NewServer(Deps{})
	serverTr, clientTr := mcp.NewInMemoryTransports()
	if _, err := srv.Connect(ctx, serverTr, nil); err != nil {
		t.Fatalf("server connect: %v", err)
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "0"}, nil)
	cs, err := client.Connect(ctx, clientTr, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	t.Cleanup(func() { _ = cs.Close() })
	return ctx, cs
}

func structured[T any](t *testing.T, res *mcp.CallToolResult) T {
	t.Helper()
	var out T
	raw, err := json.Marshal(res.StructuredContent)
	if err != nil {
		t.Fatalf("marshal structured: %v", err)
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("unmarshal structured: %v", err)
	}
	return out
}

func TestQRTools_WriteReadDecode(t *testing.T) {
	ctx, cs := qrTestClient(t)

	// qr_write -> a valid BR Code string.
	wr, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "qr_write", Arguments: map[string]any{
		"merchant_name": "ACME LTDA", "city": "SAO PAULO",
		"key": "loja@pix.com", "amount": "10.00", "txid": "PED1",
	}})
	if err != nil || wr.IsError {
		t.Fatalf("qr_write: err=%v isErr=%v", err, wr.IsError)
	}
	wout := structured[struct {
		Code     string `json:"code"`
		CRCValid bool   `json:"crc_valid"`
	}](t, wr)
	if !strings.HasPrefix(wout.Code, "0002") || !wout.CRCValid {
		t.Fatalf("qr_write bad output: %+v", wout)
	}

	// qr_read -> parse the code back into fields with CRC verified.
	rd, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "qr_read", Arguments: map[string]any{
		"code": wout.Code,
	}})
	if err != nil || rd.IsError {
		t.Fatalf("qr_read: err=%v isErr=%v", err, rd.IsError)
	}
	p := structured[brcode.Payload](t, rd)
	if !p.CRCValid || p.Key != "loja@pix.com" || p.Amount != "10.00" || p.City != "SAO PAULO" {
		t.Fatalf("qr_read mismatch: %+v", p)
	}

	// qr_decode -> render the code to a PNG, decode it back through the tool.
	png, err := brcode.RenderPNG(wout.Code, 256)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "qr.png")
	if err := os.WriteFile(path, png, 0o644); err != nil {
		t.Fatal(err)
	}
	dc, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "qr_decode", Arguments: map[string]any{
		"path": path,
	}})
	if err != nil || dc.IsError {
		t.Fatalf("qr_decode: err=%v isErr=%v", err, dc.IsError)
	}
	dp := structured[brcode.Payload](t, dc)
	if !dp.CRCValid || dp.Key != "loja@pix.com" || dp.TxID != "PED1" {
		t.Fatalf("qr_decode mismatch: %+v", dp)
	}
}

func TestQRWrite_ValidationError(t *testing.T) {
	ctx, cs := qrTestClient(t)
	// Missing key/url -> the tool returns an error result.
	res, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "qr_write", Arguments: map[string]any{
		"merchant_name": "ACME", "city": "SP",
	}})
	if err == nil && (res == nil || !res.IsError) {
		t.Fatal("expected an error for qr_write with no key/url")
	}
}
