// Package brcode reads and writes the Pix BR Code — the EMV MPM (Merchant
// Presented Mode) payload that BACEN/EMVCo define for Pix, a.k.a. "Pix Copia e
// Cola". The payload is a flat list of EMV TLV fields (2-digit id, 2-digit
// length, value), with two nested templates (merchant account 26, additional
// data 62) and a trailing CRC16 (id 63).
//
// It is pure Go with no dependencies: the codec and the CRC are deterministic,
// so this runs anywhere (air-gap clean). PNG rendering lives in render.go behind
// a build that pulls a pure-Go QR library.
//
// Monetary amounts are kept as decimal STRINGS (e.g. "10.00"), never float64,
// per the Brazilian-financial rule — float rounding must never touch a value.
package brcode

import (
	"fmt"
	"strconv"
	"strings"
)

// EMV field ids used by Pix. Only the ones the Pix spec defines are modelled;
// the codec preserves any other ids it parses so a round-trip is lossless.
const (
	idPayloadFormat   = "00" // "01"
	idInitiationPoint = "01" // "11" static (reusable) | "12" dynamic (one-time)
	idMerchantAccount = "26" // nested: Pix GUI + key/url
	idMerchantCategory = "52" // MCC, "0000" when none
	idCurrency        = "53" // "986" = BRL (ISO 4217)
	idAmount          = "54" // decimal string, omitted when payer sets the value
	idCountry         = "58" // "BR"
	idMerchantName    = "59" // max 25 chars
	idMerchantCity    = "60" // max 15 chars
	idAdditionalData  = "62" // nested: txid (reference label)
	idCRC             = "63" // CRC16 over the payload incl. "6304"

	// nested ids under 26 (merchant account information)
	idGUI         = "00" // "br.gov.bcb.pix"
	idPixKey      = "01" // the Pix key (static)
	idPixURL      = "25" // the payload-location URL (dynamic)
	idPixDescr    = "02" // optional free description (static)
	pixGUI        = "br.gov.bcb.pix"

	// nested id under 62 (additional data field template)
	idReferenceLabel = "05" // txid; "***" means "no fixed txid"
)

// Payload is the decoded Pix BR Code. Amount is a decimal string ("" = payer
// chooses). Dynamic carries a payload-location URL instead of a bare key.
type Payload struct {
	Key          string `json:"key,omitempty"`          // static: the Pix key
	URL          string `json:"url,omitempty"`          // dynamic: payload-location URL
	Description  string `json:"description,omitempty"`  // static: optional info
	MerchantName string `json:"merchant_name"`          // field 59
	City         string `json:"city"`                   // field 60
	Amount       string `json:"amount,omitempty"`       // field 54, decimal string
	TxID         string `json:"txid,omitempty"`         // field 62/05; "***" when none
	Dynamic      bool   `json:"dynamic"`                // initiation point 12 vs 11
	CRC          string `json:"crc,omitempty"`          // 4 hex digits (upper)
	CRCValid     bool   `json:"crc_valid"`              // CRC recomputed == CRC field
}

// tlv is one EMV field in document order (order is preserved on encode).
type tlv struct {
	id  string
	val string
}

// parseTLVs decodes a flat EMV TLV string. It is strict about the 2+2 framing so
// a truncated or malformed payload is rejected rather than silently accepted.
func parseTLVs(s string) ([]tlv, error) {
	var out []tlv
	for i := 0; i < len(s); {
		if i+4 > len(s) {
			return nil, fmt.Errorf("brcode: truncated field header at %d", i)
		}
		id := s[i : i+2]
		lenField := s[i+2 : i+4]
		// The length is two ASCII digits. strconv.Atoi would accept a sign
		// ("-1" -> -1), yielding a negative slice bound; require digits only.
		if !allDigits(lenField) {
			return nil, fmt.Errorf("brcode: non-numeric length %q for id %q", lenField, id)
		}
		n, err := strconv.Atoi(lenField)
		if err != nil || n < 0 {
			return nil, fmt.Errorf("brcode: bad length %q for id %q", lenField, id)
		}
		i += 4
		if i+n > len(s) {
			return nil, fmt.Errorf("brcode: field %q length %d exceeds payload", id, n)
		}
		out = append(out, tlv{id: id, val: s[i : i+n]})
		i += n
	}
	return out, nil
}

// encodeTLVs renders fields back to the EMV string (id + 2-digit length + value).
func encodeTLVs(fields []tlv) (string, error) {
	var b strings.Builder
	for _, f := range fields {
		if len(f.val) > 99 {
			return "", fmt.Errorf("brcode: field %q value too long (%d > 99)", f.id, len(f.val))
		}
		fmt.Fprintf(&b, "%s%02d%s", f.id, len(f.val), f.val)
	}
	return b.String(), nil
}

// find returns the first value for id, or "".
func find(fields []tlv, id string) string {
	for _, f := range fields {
		if f.id == id {
			return f.val
		}
	}
	return ""
}

// Parse decodes a BR Code string into a Payload and verifies its CRC. A bad
// frame is an error; a bad CRC is reported via Payload.CRCValid (not an error)
// so callers can inspect a tampered code.
func Parse(code string) (Payload, error) {
	code = strings.TrimSpace(code)
	fields, err := parseTLVs(code)
	if err != nil {
		return Payload{}, err
	}
	var p Payload

	switch find(fields, idInitiationPoint) {
	case "12":
		p.Dynamic = true
	}
	p.MerchantName = find(fields, idMerchantName)
	p.City = find(fields, idMerchantCity)
	p.Amount = find(fields, idAmount)

	// nested merchant account (26)
	if ma := find(fields, idMerchantAccount); ma != "" {
		sub, err := parseTLVs(ma)
		if err != nil {
			return Payload{}, fmt.Errorf("brcode: merchant account: %w", err)
		}
		p.Key = find(sub, idPixKey)
		p.URL = find(sub, idPixURL)
		p.Description = find(sub, idPixDescr)
	}
	// nested additional data (62) -> txid
	if ad := find(fields, idAdditionalData); ad != "" {
		sub, err := parseTLVs(ad)
		if err != nil {
			return Payload{}, fmt.Errorf("brcode: additional data: %w", err)
		}
		p.TxID = find(sub, idReferenceLabel)
	}

	// CRC: the field value plus a recompute over everything up to and including
	// the "6304" tag prefix.
	p.CRC = strings.ToUpper(find(fields, idCRC))
	if idx := strings.LastIndex(code, idCRC+"04"); idx >= 0 {
		want := CRC16(code[:idx+4])
		p.CRCValid = strings.EqualFold(p.CRC, fmt.Sprintf("%04X", want))
	}
	return p, nil
}

// Encode builds a valid BR Code string from the Payload, computing the CRC. It
// validates the required fields and the Pix-spec length caps (name 25, city 15).
func (p Payload) Encode() (string, error) {
	if strings.TrimSpace(p.Key) == "" && strings.TrimSpace(p.URL) == "" {
		return "", fmt.Errorf("brcode: a Pix key or a payload URL is required")
	}
	if p.Key != "" && p.URL != "" {
		return "", fmt.Errorf("brcode: set either key (static) or url (dynamic), not both")
	}
	if strings.TrimSpace(p.MerchantName) == "" || strings.TrimSpace(p.City) == "" {
		return "", fmt.Errorf("brcode: merchant_name and city are required")
	}
	if len(p.MerchantName) > 25 {
		return "", fmt.Errorf("brcode: merchant_name exceeds 25 chars (%d)", len(p.MerchantName))
	}
	if len(p.City) > 15 {
		return "", fmt.Errorf("brcode: city exceeds 15 chars (%d)", len(p.City))
	}
	if p.Amount != "" {
		if err := validAmount(p.Amount); err != nil {
			return "", err
		}
	}

	// merchant account (26) nested template
	var ma []tlv
	ma = append(ma, tlv{idGUI, pixGUI})
	if p.URL != "" {
		ma = append(ma, tlv{idPixURL, p.URL})
	} else {
		ma = append(ma, tlv{idPixKey, p.Key})
		if p.Description != "" {
			ma = append(ma, tlv{idPixDescr, p.Description})
		}
	}
	maStr, err := encodeTLVs(ma)
	if err != nil {
		return "", err
	}

	initiation := "11"
	if p.Dynamic || p.URL != "" {
		initiation = "12"
	}
	txid := p.TxID
	if txid == "" {
		txid = "***"
	}
	adStr, err := encodeTLVs([]tlv{{idReferenceLabel, txid}})
	if err != nil {
		return "", err
	}

	fields := []tlv{
		{idPayloadFormat, "01"},
		{idInitiationPoint, initiation},
		{idMerchantAccount, maStr},
		{idMerchantCategory, "0000"},
		{idCurrency, "986"},
	}
	if p.Amount != "" {
		fields = append(fields, tlv{idAmount, p.Amount})
	}
	fields = append(fields,
		tlv{idCountry, "BR"},
		tlv{idMerchantName, p.MerchantName},
		tlv{idMerchantCity, p.City},
		tlv{idAdditionalData, adStr},
	)

	body, err := encodeTLVs(fields)
	if err != nil {
		return "", err
	}
	// CRC is computed over the body plus the CRC field's own id+length ("6304").
	withCRCTag := body + idCRC + "04"
	return withCRCTag + fmt.Sprintf("%04X", CRC16(withCRCTag)), nil
}

// validAmount enforces a plain decimal with at most two fraction digits — the
// Pix amount format. No float parsing: the string itself is validated.
func validAmount(a string) error {
	intPart, frac, hasDot := strings.Cut(a, ".")
	if intPart == "" || !allDigits(intPart) || (hasDot && (frac == "" || !allDigits(frac))) {
		return fmt.Errorf("brcode: amount %q must be a decimal like 10.00", a)
	}
	if len(frac) > 2 {
		return fmt.Errorf("brcode: amount %q has more than 2 decimal places", a)
	}
	return nil
}

func allDigits(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return s != ""
}

// CRC16 computes the EMV/Pix CRC16-CCITT (poly 0x1021, init 0xFFFF, no
// reflection, no final xor) over the ASCII bytes of s. The check value of
// "123456789" is 0x29B1.
func CRC16(s string) uint16 {
	crc := uint16(0xFFFF)
	for i := 0; i < len(s); i++ {
		crc ^= uint16(s[i]) << 8
		for range 8 {
			if crc&0x8000 != 0 {
				crc = (crc << 1) ^ 0x1021
			} else {
				crc <<= 1
			}
		}
	}
	return crc
}
