package brcode

import (
	"strings"
	"testing"
)

// CRC16 check value: the EMV/Pix CRC16-CCITT of "123456789" is 0x29B1.
func TestCRC16CheckValue(t *testing.T) {
	if got := CRC16("123456789"); got != 0x29B1 {
		t.Fatalf("CRC16 check value = %04X, want 29B1", got)
	}
}

func TestEncodeParseRoundTrip(t *testing.T) {
	in := Payload{
		Key:          "fulano@example.com",
		MerchantName: "ACME LTDA",
		City:         "SAO PAULO",
		Amount:       "10.00",
		TxID:         "PEDIDO123",
	}
	code, err := in.Encode()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(code, "0002010102") {
		t.Fatalf("unexpected prefix: %s", code)
	}
	out, err := Parse(code)
	if err != nil {
		t.Fatal(err)
	}
	if !out.CRCValid {
		t.Fatalf("round-trip CRC invalid for %s", code)
	}
	if out.Key != in.Key || out.MerchantName != in.MerchantName || out.City != in.City ||
		out.Amount != in.Amount || out.TxID != in.TxID {
		t.Fatalf("round-trip mismatch: %+v vs %+v", out, in)
	}
	if out.Dynamic {
		t.Fatal("static payload parsed as dynamic")
	}
}

func TestEncodeDynamicURL(t *testing.T) {
	in := Payload{URL: "pix.example.com/qr/v2/abc", MerchantName: "ACME", City: "RECIFE"}
	code, err := in.Encode()
	if err != nil {
		t.Fatal(err)
	}
	out, err := Parse(code)
	if err != nil {
		t.Fatal(err)
	}
	if !out.Dynamic {
		t.Fatal("URL payload should be dynamic (initiation 12)")
	}
	if out.URL != in.URL || out.Key != "" {
		t.Fatalf("dynamic round-trip mismatch: %+v", out)
	}
	if !out.CRCValid {
		t.Fatal("dynamic CRC invalid")
	}
}

func TestParseDefaultTxIDAndNoAmount(t *testing.T) {
	// No amount, no txid -> "***" reference label, amount omitted.
	code, err := Payload{Key: "+5561999999999", MerchantName: "FULANO", City: "BRASILIA"}.Encode()
	if err != nil {
		t.Fatal(err)
	}
	out, err := Parse(code)
	if err != nil {
		t.Fatal(err)
	}
	if out.Amount != "" {
		t.Fatalf("expected no amount, got %q", out.Amount)
	}
	if out.TxID != "***" {
		t.Fatalf("expected default txid ***, got %q", out.TxID)
	}
}

func TestParseDetectsTamperedCRC(t *testing.T) {
	code, _ := Payload{Key: "k@e.com", MerchantName: "ACME", City: "SP"}.Encode()
	// flip the last CRC hex digit
	bad := code[:len(code)-1] + flip(code[len(code)-1])
	out, err := Parse(bad)
	if err != nil {
		t.Fatal(err)
	}
	if out.CRCValid {
		t.Fatal("tampered CRC reported as valid")
	}
}

func TestParseRejectsMalformedFrame(t *testing.T) {
	if _, err := Parse("00XX01"); err == nil {
		t.Fatal("expected error on bad length field")
	}
	if _, err := Parse("0099AB"); err == nil {
		t.Fatal("expected error on length exceeding payload")
	}
}

func TestEncodeValidation(t *testing.T) {
	cases := []struct {
		name string
		p    Payload
	}{
		{"no key or url", Payload{MerchantName: "A", City: "B"}},
		{"both key and url", Payload{Key: "k", URL: "u", MerchantName: "A", City: "B"}},
		{"missing name", Payload{Key: "k", City: "B"}},
		{"name too long", Payload{Key: "k", MerchantName: strings.Repeat("X", 26), City: "B"}},
		{"city too long", Payload{Key: "k", MerchantName: "A", City: strings.Repeat("Y", 16)}},
		{"bad amount", Payload{Key: "k", MerchantName: "A", City: "B", Amount: "10.000"}},
		{"non-numeric amount", Payload{Key: "k", MerchantName: "A", City: "B", Amount: "1,00"}},
	}
	for _, c := range cases {
		if _, err := c.p.Encode(); err == nil {
			t.Errorf("%s: expected error", c.name)
		}
	}
}

func flip(b byte) string {
	if b == '0' {
		return "1"
	}
	return "0"
}
