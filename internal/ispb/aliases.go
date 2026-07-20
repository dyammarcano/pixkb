package ispb

import "strings"

// brandAliases maps common trade/brand names to a fragment of the institution's
// official BACEN name. The STR and Pix registries list institutions by legal
// name — e.g. Nubank is "NU PAGAMENTOS", PagBank is "PAGSEGURO" — so a literal
// search for the brand finds nothing. Each value is a lowercase fragment of the
// official name that IS present in the registry, fed back through the same
// accent-insensitive substring search. Keying to a name fragment (not a
// hardcoded ISPB code) keeps this robust to registry changes and avoids
// asserting codes that could drift.
var brandAliases = map[string]string{
	"nubank":        "nu pagamentos",
	"nu bank":       "nu pagamentos",
	"pagbank":       "pagseguro",
	"pag bank":      "pagseguro",
	"mercadopago":   "mercado pago",
	"mercado livre": "mercado pago",
	"willbank":      "will",
	"will bank":     "will",
	"ame":           "ame digital",
	"iti":           "itau unibanco", // iti is Itaú's digital wallet
	"c6":            "c6 bank",
	"recargapay":    "recarga",
	// Rebrands and state-bank trade names (verified against the live registry:
	// institution_name abbreviates, but legal_name carries the full form).
	"modal":   "genial", // Banco Modal became Banco Genial
	"bancoob": "sicoob", // Bancoob rebranded to Sicoob
	"banese":  "estado de sergipe",
	"banpara": "estado do para",
}

// AliasFragments returns additional official-name fragments to search for the
// given free-text query, expanding brand/trade names the BACEN registry does not
// carry. It also rewrites a leading "banco " to BACEN's "bco " abbreviation
// (institution names use "BCO ..."), so "banco do brasil" reaches "BCO DO
// BRASIL". Returns nil when nothing applies. The returned fragments are meant to
// be searched IN ADDITION to the raw query, with results merged and de-duped by
// ISPB code.
func AliasFragments(query string) []string {
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		return nil
	}
	var out []string
	seen := map[string]bool{}
	add := func(s string) {
		if s != "" && s != q && !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}

	// Curated brand aliases. Match when the brand key contains the query or the
	// query contains the brand key (so both "nu" and "nubank" resolve), guarded
	// by a minimum length so a 1–2 char query does not fan out to everything.
	if len(q) >= 3 {
		for brand, frag := range brandAliases {
			if strings.Contains(brand, q) || strings.Contains(q, brand) {
				add(frag)
			}
		}
	}

	// BACEN abbreviates "BANCO" as "BCO" in institution names.
	if rest, ok := strings.CutPrefix(q, "banco "); ok {
		add("bco " + rest)
	}

	return out
}
