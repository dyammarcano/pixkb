// Package rag (pii.go): a deterministic, regex-based PII/LGPD redaction pass
// over synthesized answer text. This exists because the answerer agent is only
// PROMPT-instructed to avoid personal data (CPF, CNPJ, phone, email) — nothing
// in code enforced that today. A false positive (redacting a non-PII digit run
// that happens to be 11 or 14 digits long) is an acceptable cost in a compliance
// context; a false negative (a leaked CPF slipping through) is not. Only ever
// applied to Answer.Text — Citations (concept ids) are never passed through
// this and so are structurally unaffected by any regex here.
package rag

import "regexp"

var (
	// Formatted forms first: their punctuation makes them unambiguous and
	// distinguishes a CPF from a CNPJ before either bare-digit fallback runs.
	reCNPJFormatted = regexp.MustCompile(`\b\d{2}\.\d{3}\.\d{3}/\d{4}-\d{2}\b`)
	reCPFFormatted  = regexp.MustCompile(`\b\d{3}\.\d{3}\.\d{3}-\d{2}\b`)
	// Bare-digit fallbacks: \b...\b anchors mean these can only match a digit
	// run of EXACTLY that length (a longer or shorter run has no internal word
	// boundary to match against), so an unformatted 14-digit CNPJ can never be
	// partially caught by the 11-digit CPF pattern.
	reCNPJBare = regexp.MustCompile(`\b\d{14}\b`)
	reCPFBare  = regexp.MustCompile(`\b\d{11}\b`)
	// Brazilian phone: optional +55 country code, optional parenthesized DDD,
	// 8 or 9 digit local number with an optional separator.
	rePhone = regexp.MustCompile(`(?:\+55\s?)?\(?\d{2}\)?[\s.-]?\d{4,5}-?\d{4}\b`)
	reEmail = regexp.MustCompile(`\b[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}\b`)
)

// RedactPII replaces CPF, CNPJ, Brazilian phone numbers, and email addresses
// in s with a labeled placeholder. Order matters: formatted CPF/CNPJ and both
// bare-digit fallbacks run BEFORE the phone pattern, so an ambiguous unformatted
// 11-digit run (which also structurally matches as a phone number) is redacted
// once as CPF, not double-tagged — once a span is replaced with a
// "[REDACTED:...]" placeholder it no longer contains digits, so no later
// pattern in this function can re-match it.
func RedactPII(s string) string {
	s = reCNPJFormatted.ReplaceAllString(s, "[REDACTED:CNPJ]")
	s = reCPFFormatted.ReplaceAllString(s, "[REDACTED:CPF]")
	s = reCNPJBare.ReplaceAllString(s, "[REDACTED:CNPJ]")
	s = reCPFBare.ReplaceAllString(s, "[REDACTED:CPF]")
	s = rePhone.ReplaceAllString(s, "[REDACTED:PHONE]")
	s = reEmail.ReplaceAllString(s, "[REDACTED:EMAIL]")
	return s
}
