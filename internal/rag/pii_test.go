package rag

import (
	"strings"
	"testing"
)

func TestRedactPII_CPFFormatted(t *testing.T) {
	got := RedactPII("Meu CPF é 123.456.789-01, obrigado.")
	if strings.Contains(got, "123.456.789-01") || !strings.Contains(got, "[REDACTED:CPF]") {
		t.Fatalf("formatted CPF not redacted: %q", got)
	}
}

func TestRedactPII_CPFBare(t *testing.T) {
	got := RedactPII("CPF: 11122233344 registrado.")
	if strings.Contains(got, "11122233344") || !strings.Contains(got, "[REDACTED:CPF]") {
		t.Fatalf("bare CPF not redacted: %q", got)
	}
}

func TestRedactPII_CNPJFormatted(t *testing.T) {
	got := RedactPII("CNPJ 12.345.678/0001-95 ativo.")
	if strings.Contains(got, "12.345.678/0001-95") || !strings.Contains(got, "[REDACTED:CNPJ]") {
		t.Fatalf("formatted CNPJ not redacted: %q", got)
	}
}

func TestRedactPII_CNPJBare(t *testing.T) {
	got := RedactPII("CNPJ 12345678000195 ativo.")
	if strings.Contains(got, "12345678000195") || !strings.Contains(got, "[REDACTED:CNPJ]") {
		t.Fatalf("bare CNPJ not redacted: %q", got)
	}
}

func TestRedactPII_PhoneParens(t *testing.T) {
	got := RedactPII("Ligue para (11) 98765-4321 em horário comercial.")
	if strings.Contains(got, "98765-4321") || !strings.Contains(got, "[REDACTED:PHONE]") {
		t.Fatalf("phone not redacted: %q", got)
	}
}

func TestRedactPII_PhoneCountryCode(t *testing.T) {
	got := RedactPII("WhatsApp: +55 11 98765-4321")
	if strings.Contains(got, "98765-4321") || !strings.Contains(got, "[REDACTED:PHONE]") {
		t.Fatalf("phone with +55 not redacted: %q", got)
	}
}

func TestRedactPII_Email(t *testing.T) {
	got := RedactPII("Envie para contato@exemplo.com.br para suporte.")
	if strings.Contains(got, "contato@exemplo.com.br") || !strings.Contains(got, "[REDACTED:EMAIL]") {
		t.Fatalf("email not redacted: %q", got)
	}
}

func TestRedactPII_LeavesConceptIDsAndProseAlone(t *testing.T) {
	in := "Veja pix-glossary.md e api-endpoint-1.md para detalhes sobre a chave Pix."
	got := RedactPII(in)
	if got != in {
		t.Fatalf("prose/concept-id-like text must be untouched, got %q", got)
	}
}

func TestRedactPII_MultiplePIITypesInOneString(t *testing.T) {
	got := RedactPII("CPF 123.456.789-01, email contato@exemplo.com, tel (11) 91234-5678.")
	for _, want := range []string{"[REDACTED:CPF]", "[REDACTED:EMAIL]", "[REDACTED:PHONE]"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %s in %q", want, got)
		}
	}
}
