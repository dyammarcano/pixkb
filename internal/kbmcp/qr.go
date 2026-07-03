package kbmcp

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"pixkb/internal/brcode"
)

type qrReadIn struct {
	Code string `json:"code" jsonschema:"the Pix BR Code string (EMV MPM / 'Copia e Cola')"`
}

// registerQRRead exposes Pix BR Code parsing as a read-only tool: agents can
// decode a 'Copia e Cola' string into its fields and confirm the CRC. Pure Go,
// no DB or network.
func registerQRRead(s *mcp.Server) {
	mcp.AddTool(s, &mcp.Tool{
		Name:        "qr_read",
		Description: "Parse a Pix BR Code (EMV MPM / 'Pix Copia e Cola') into its fields (key/url, merchant, city, amount, txid) and verify the CRC16.",
	}, func(_ context.Context, _ *mcp.CallToolRequest, in qrReadIn) (*mcp.CallToolResult, brcode.Payload, error) {
		p, err := brcode.Parse(in.Code)
		if err != nil {
			return nil, brcode.Payload{}, err
		}
		return textResult(fmt.Sprintf("parsed Pix BR Code (crc valid=%v): %s %s", p.CRCValid, p.MerchantName, p.City)), p, nil
	})
}

type qrDecodeIn struct {
	Path string `json:"path" jsonschema:"path to a PNG/JPEG image file containing a Pix QR code"`
}

// registerQRDecode exposes reading a Pix BR Code from an image file: decode the
// QR, then parse it into fields. Pure Go (gozxing), no network.
func registerQRDecode(s *mcp.Server) {
	mcp.AddTool(s, &mcp.Tool{
		Name:        "qr_decode",
		Description: "Decode a Pix QR code from a PNG/JPEG image file into its fields (key/url, merchant, city, amount, txid) with CRC verification.",
	}, func(_ context.Context, _ *mcp.CallToolRequest, in qrDecodeIn) (*mcp.CallToolResult, brcode.Payload, error) {
		p, err := brcode.ParseImageFile(in.Path)
		if err != nil {
			return nil, brcode.Payload{}, err
		}
		return textResult(fmt.Sprintf("decoded Pix QR from %s (crc valid=%v): %s %s", in.Path, p.CRCValid, p.MerchantName, p.City)), p, nil
	})
}

type qrWriteIn struct {
	Key          string `json:"key,omitempty" jsonschema:"Pix key for a static code (set key OR url)"`
	URL          string `json:"url,omitempty" jsonschema:"payload-location URL for a dynamic code (set key OR url)"`
	MerchantName string `json:"merchant_name" jsonschema:"merchant name, max 25 chars (required)"`
	City         string `json:"city" jsonschema:"merchant city, max 15 chars (required)"`
	Amount       string `json:"amount,omitempty" jsonschema:"amount as a decimal string e.g. 10.00 (omit to let the payer choose)"`
	TxID         string `json:"txid,omitempty" jsonschema:"transaction id / reference label (default ***)"`
	Description  string `json:"description,omitempty" jsonschema:"optional free description (static codes)"`
}
type qrWriteOut struct {
	Code     string `json:"code"`
	CRCValid bool   `json:"crc_valid"`
}

// registerQRWrite exposes Pix BR Code generation as a tool: agents build a valid
// 'Copia e Cola' string (with CRC16) from fields. Returns the string only — PNG
// rendering is a CLI concern (`pixkb qr write --png`).
func registerQRWrite(s *mcp.Server) {
	mcp.AddTool(s, &mcp.Tool{
		Name:        "qr_write",
		Description: "Build a Pix BR Code (EMV MPM / 'Pix Copia e Cola') string from fields, computing the CRC16. Set either key (static) or url (dynamic).",
	}, func(_ context.Context, _ *mcp.CallToolRequest, in qrWriteIn) (*mcp.CallToolResult, qrWriteOut, error) {
		code, err := brcode.Payload{
			Key: in.Key, URL: in.URL, MerchantName: in.MerchantName, City: in.City,
			Amount: in.Amount, TxID: in.TxID, Description: in.Description,
		}.Encode()
		if err != nil {
			return nil, qrWriteOut{}, err
		}
		return textResult(code), qrWriteOut{Code: code, CRCValid: true}, nil
	})
}
