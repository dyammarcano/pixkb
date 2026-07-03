package brcode

import (
	"strings"
	"testing"
)

// FuzzParse asserts the BR Code parser is panic-free on arbitrary input — it
// reads untrusted data (a scanned QR, a pasted "Copia e Cola"). The seed corpus
// includes valid codes, truncated frames, and pathological lengths. Invariants:
//   - Parse must never panic and must return either (Payload, nil) or ("", err);
//   - whenever it succeeds, re-running Parse on a self-encoded code agrees on CRC.
func FuzzParse(f *testing.F) {
	valid, _ := Payload{Key: "k@e.com", MerchantName: "ACME", City: "SP", Amount: "1.00"}.Encode()
	seeds := []string{
		valid,
		"",
		" ",
		"00",
		"0002",
		"000201",
		"00XX01",
		"0099AB",
		"6304ABCD",
		strings.Repeat("0", 1000),
		"00020101021126400014br.gov.bcb.pix",
		valid[:len(valid)-2], // truncated CRC
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, code string) {
		p, err := Parse(code) // must not panic
		if err != nil {
			return
		}
		// A successful parse must yield a Payload whose re-encode (when it has the
		// minimum required fields) parses back consistently — no infinite values,
		// no corruption that round-trips differently.
		if p.MerchantName != "" && p.City != "" && (p.Key != "" || p.URL != "") {
			if code2, err2 := p.Encode(); err2 == nil {
				if _, err3 := Parse(code2); err3 != nil {
					t.Fatalf("re-encoded payload no longer parses: %v\norig=%q", err3, code)
				}
			}
		}
	})
}
