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

func TestEncodeOmitInitiationPoint_MatchesRealWorldSample(t *testing.T) {
	// Real "Copia e Cola" issued by a Mercado Livre-style generator that never
	// emits field 01 for static codes.
	const want = "00020126540014br.gov.bcb.pix0132pix_marketplace@mercadolibre.com" +
		"5204000053039865406214.575802BR5911@30448819476009Sao Paulo" +
		"62250521mpqrinter1367527320626304A88B"

	got, err := Payload{
		Key:                 "pix_marketplace@mercadolibre.com",
		MerchantName:        "@3044881947",
		City:                "Sao Paulo",
		Amount:              "214.57",
		TxID:                "mpqrinter136752732062",
		OmitInitiationPoint: true,
	}.Encode()
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("encode mismatch:\n got: %s\nwant: %s", got, want)
	}

	out, err := Parse(got)
	if err != nil {
		t.Fatal(err)
	}
	if out.Dynamic {
		t.Fatal("omitted field 01 must still parse as static")
	}
	if !out.CRCValid {
		t.Fatal("CRC invalid")
	}
}

func TestEncodeOmitInitiationPoint_IgnoredForDynamic(t *testing.T) {
	// A dynamic code always needs "12" so wallets know to fetch the payload —
	// OmitInitiationPoint must not suppress it.
	code, err := Payload{
		URL: "pix.example.com/qr/v2/abc", MerchantName: "ACME", City: "RECIFE",
		OmitInitiationPoint: true,
	}.Encode()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(code, "0102"+"12") {
		t.Fatalf("expected initiation point 12 to survive OmitInitiationPoint for a dynamic code: %s", code)
	}
	out, err := Parse(code)
	if err != nil {
		t.Fatal(err)
	}
	if !out.Dynamic {
		t.Fatal("expected dynamic")
	}
}

func TestPayloadValidate_SameChecksAsEncode(t *testing.T) {
	cases := []struct {
		name string
		p    Payload
		ok   bool
	}{
		{"valid", Payload{Key: "k", MerchantName: "A", City: "B"}, true},
		{"no key or url", Payload{MerchantName: "A", City: "B"}, false},
		{"both key and url", Payload{Key: "k", URL: "u", MerchantName: "A", City: "B"}, false},
		{"missing name", Payload{Key: "k", City: "B"}, false},
		{"name too long", Payload{Key: "k", MerchantName: strings.Repeat("X", 26), City: "B"}, false},
		{"bad amount", Payload{Key: "k", MerchantName: "A", City: "B", Amount: "10.000"}, false},
		{"mixed case ASCII ok", Payload{Key: "k", MerchantName: "@3044881947", City: "Sao Paulo"}, true},
		{"accented merchant name", Payload{Key: "k", MerchantName: "ITAÚ", City: "B"}, false},
		{"accented city", Payload{Key: "k", MerchantName: "A", City: "SÃO PAULO"}, false},
		{"accented key", Payload{Key: "fulanoção@x.com", MerchantName: "A", City: "B"}, false},
		{"accented description", Payload{Key: "k", MerchantName: "A", City: "B", Description: "café"}, false},
		{"accented txid", Payload{Key: "k", MerchantName: "A", City: "B", TxID: "pedido-café"}, false},
		{"accented url", Payload{URL: "pix.example.com/café", MerchantName: "A", City: "B"}, false},
		{"control char in name", Payload{Key: "k", MerchantName: "A\tB", City: "B"}, false},
		{"emoji in name", Payload{Key: "k", MerchantName: "ACME 🚀", City: "B"}, false},
	}
	for _, c := range cases {
		err := c.p.Validate()
		if c.ok && err != nil {
			t.Errorf("%s: unexpected error: %v", c.name, err)
		}
		if !c.ok && err == nil {
			t.Errorf("%s: expected error", c.name)
		}
	}
}

func TestPayloadValidate_ASCIIErrorNamesTheField(t *testing.T) {
	err := Payload{Key: "k", MerchantName: "ITAÚ", City: "B"}.Validate()
	if err == nil || !strings.Contains(err.Error(), "merchant_name") {
		t.Fatalf("expected error naming merchant_name, got %v", err)
	}
}

func TestValidateCode_ValidAndTampered(t *testing.T) {
	code, err := Payload{Key: "k@e.com", MerchantName: "ACME", City: "SP"}.Encode()
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateCode(code); err != nil {
		t.Fatalf("expected valid code, got %v", err)
	}

	bad := code[:len(code)-1] + flip(code[len(code)-1])
	if err := ValidateCode(bad); err == nil {
		t.Fatal("expected tampered CRC to fail validation")
	}

	if err := ValidateCode("00XX01"); err == nil {
		t.Fatal("expected malformed frame to fail validation")
	}
}

func flip(b byte) string {
	if b == '0' {
		return "1"
	}
	return "0"
}
