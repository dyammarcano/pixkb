package brcode

import (
	"strings"
	"testing"
)

func TestEncodeBoundaryLengths(t *testing.T) {
	// name exactly 25, city exactly 15 must succeed (the caps are inclusive).
	p := Payload{
		Key:          "k@e.com",
		MerchantName: strings.Repeat("A", 25),
		City:         strings.Repeat("B", 15),
	}
	code, err := p.Encode()
	if err != nil {
		t.Fatalf("boundary lengths should be valid: %v", err)
	}
	out, err := Parse(code)
	if err != nil || !out.CRCValid {
		t.Fatalf("boundary round-trip failed: err=%v crc=%v", err, out.CRCValid)
	}
}

func TestEncodeAmountFormats(t *testing.T) {
	ok := []string{"0.00", "100", "5.5", "1234567.89", "10"}
	for _, a := range ok {
		if _, err := (Payload{Key: "k", MerchantName: "A", City: "B", Amount: a}).Encode(); err != nil {
			t.Errorf("amount %q should be valid: %v", a, err)
		}
	}
	bad := []string{"10.000", "1,00", "-5.00", "abc", "10.", ".5", "1 0"}
	for _, a := range bad {
		if _, err := (Payload{Key: "k", MerchantName: "A", City: "B", Amount: a}).Encode(); err == nil {
			t.Errorf("amount %q should be rejected", a)
		}
	}
}

func TestParseLowercaseCRC(t *testing.T) {
	code, _ := Payload{Key: "k@e.com", MerchantName: "ACME", City: "SP"}.Encode()
	// CRC compare must be case-insensitive.
	lower := code[:len(code)-4] + strings.ToLower(code[len(code)-4:])
	out, err := Parse(lower)
	if err != nil {
		t.Fatal(err)
	}
	if !out.CRCValid {
		t.Fatal("lowercase CRC should still validate")
	}
}

func TestDescriptionPreserved(t *testing.T) {
	in := Payload{Key: "k@e.com", MerchantName: "ACME", City: "SP", Description: "Pedido 42"}
	code, err := in.Encode()
	if err != nil {
		t.Fatal(err)
	}
	out, err := Parse(code)
	if err != nil {
		t.Fatal(err)
	}
	if out.Description != "Pedido 42" {
		t.Fatalf("description not preserved: %q", out.Description)
	}
}

func TestParseToleratesUnknownFields(t *testing.T) {
	// A real code may carry fields our model does not map (e.g. 80 unreserved
	// templates); Parse must ignore them and still read the known ones + CRC.
	base, _ := Payload{Key: "k@e.com", MerchantName: "ACME", City: "SP", Amount: "1.00"}.Encode()
	// inject an unknown field "8004ABCD" right before the CRC tag.
	idx := strings.LastIndex(base, "6304")
	body := base[:idx] + "8004ABCD"
	injected := body + "6304" + sprintfCRC(body+"6304")
	out, err := Parse(injected)
	if err != nil {
		t.Fatal(err)
	}
	if !out.CRCValid || out.Key != "k@e.com" || out.Amount != "1.00" {
		t.Fatalf("unknown-field tolerance failed: %+v", out)
	}
}

func sprintfCRC(s string) string {
	const hex = "0123456789ABCDEF"
	c := CRC16(s)
	return string([]byte{hex[(c>>12)&0xF], hex[(c>>8)&0xF], hex[(c>>4)&0xF], hex[c&0xF]})
}
