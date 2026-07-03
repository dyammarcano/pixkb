package brcode

import (
	"bytes"
	"testing"
)

func TestRenderPNG(t *testing.T) {
	code, _, err := Payload{Key: "k@e.com", MerchantName: "ACME", City: "SP", Amount: "1.00"}.EncodePNG(256)
	if err != nil {
		t.Fatal(err)
	}
	if code == "" {
		t.Fatal("empty code")
	}
	png, err := RenderPNG(code, 256)
	if err != nil {
		t.Fatal(err)
	}
	// PNG magic number.
	if !bytes.HasPrefix(png, []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}) {
		t.Fatalf("output is not a PNG (len %d)", len(png))
	}
}
