package brcode

import (
	"bytes"
	"testing"
)

// Full image loop: build a payload -> render PNG -> decode the PNG -> parse ->
// the fields and CRC survive the round trip through a real QR image.
func TestImageRoundTrip(t *testing.T) {
	in := Payload{
		Key:          "fulano@example.com",
		MerchantName: "ACME LTDA",
		City:         "SAO PAULO",
		Amount:       "10.00",
		TxID:         "PED123",
	}
	code, png, err := in.EncodePNG(512)
	if err != nil {
		t.Fatal(err)
	}

	text, err := DecodeImage(bytes.NewReader(png))
	if err != nil {
		t.Fatal(err)
	}
	if text != code {
		t.Fatalf("decoded text != encoded code:\n got %q\nwant %q", text, code)
	}

	out, err := ParseImage(bytes.NewReader(png))
	if err != nil {
		t.Fatal(err)
	}
	if !out.CRCValid {
		t.Fatal("CRC invalid after image round trip")
	}
	if out.Key != in.Key || out.Amount != in.Amount || out.City != in.City ||
		out.MerchantName != in.MerchantName || out.TxID != in.TxID {
		t.Fatalf("field mismatch after image round trip: %+v", out)
	}
}

func TestDecodeImageRejectsNonImage(t *testing.T) {
	if _, err := DecodeImage(bytes.NewReader([]byte("not an image"))); err == nil {
		t.Fatal("expected error decoding non-image bytes")
	}
}
