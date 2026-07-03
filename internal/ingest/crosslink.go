package ingest

import (
	"fmt"
	"sort"
	"strings"

	"pixkb/internal/okf"
)

// CrossLink builds the OKF concept graph by appending a "## Related" section of
// markdown links to each concept and setting its Links. Two concepts are linked
// when they share salient Pix domain terms (e.g. "cobranca", "devolucao",
// "txid", "dict") found in their title/description/tags, plus an endpoint
// path-family link (every /cob* endpoint relates to the others). Links use the
// full bundle-relative target ID so ingest- and reindex-time edge resolution
// agree. The function is deterministic and bounded (at most maxLinks per
// concept), so re-running it produces a stable bundle.
func CrossLink(concepts []okf.Concept) []okf.Concept {
	const (
		maxLinks   = 6  // cap related links per concept
		genericCap = 25 // ignore terms shared by more than this many concepts
		minOverlap = 1  // minimum shared terms to relate
	)

	// 1. Derive each concept's salient term set.
	termsOf := make([]map[string]bool, len(concepts))
	index := map[string][]int{} // term -> concept indices (built order = input order)
	for i, c := range concepts {
		ts := conceptTerms(c)
		termsOf[i] = ts
		for t := range ts {
			index[t] = append(index[t], i)
		}
	}

	for i := range concepts {
		// 2. Score candidates by shared-term count, skipping over-generic terms.
		score := map[int]int{}
		for t := range termsOf[i] {
			peers := index[t]
			if len(peers) > genericCap {
				continue
			}
			for _, j := range peers {
				if j != i {
					score[j]++
				}
			}
		}
		if len(score) == 0 {
			continue
		}

		// 3. Rank by (score desc, concept ID asc) for determinism, take top N.
		cand := make([]int, 0, len(score))
		for j, s := range score {
			if s >= minOverlap {
				cand = append(cand, j)
			}
		}
		sort.Slice(cand, func(a, b int) bool {
			ja, jb := cand[a], cand[b]
			if score[ja] != score[jb] {
				return score[ja] > score[jb]
			}
			return concepts[ja].ID < concepts[jb].ID
		})
		if len(cand) > maxLinks {
			cand = cand[:maxLinks]
		}

		// 4. Append a Related section + set Links (full bundle-relative IDs).
		var b strings.Builder
		b.WriteString("\n\n## Related\n")
		links := make([]string, 0, len(cand))
		for _, j := range cand {
			fmt.Fprintf(&b, "- [%s](%s)\n", concepts[j].Title, concepts[j].ID)
			links = append(links, concepts[j].ID)
		}
		concepts[i].Body += b.String()
		concepts[i].Links = links
		concepts[i].ContentSHA = okf.ComputeSHA(concepts[i].Body)
	}
	return concepts
}

// salientTerms are curated Pix/SPB domain keywords (accent-stripped, lowercase)
// used to relate concepts across manuals, ISO messages, and API endpoints.
var salientTerms = map[string]bool{
	"cobranca": true, "cobv": true, "cobr": true, "cob": true,
	"devolucao": true, "pix": true, "dict": true, "qrcode": true, "qr": true,
	"txid": true, "lote": true, "webhook": true, "recorrencia": true,
	"agendamento": true, "vinculo": true, "chave": true, "pagador": true,
	"recebedor": true, "psp": true, "spi": true, "pacs": true, "camt": true,
	"liquidacao": true, "arranjo": true, "participante": true, "saque": true,
	"troco": true, "reembolso": true, "conciliacao": true,
}

// conceptTerms extracts the salient terms present in a concept, plus an endpoint
// path-family term (the first path segment) so /cob* endpoints relate.
func conceptTerms(c okf.Concept) map[string]bool {
	out := map[string]bool{}
	text := normalizeTerms(c.Title + " " + c.Description + " " + strings.Join(c.Tags, " "))
	for tok := range strings.FieldsSeq(text) {
		if salientTerms[tok] {
			out[tok] = true
		}
	}
	if c.Type == "ApiEndpoint" {
		if seg := endpointPathRoot(c.Title); seg != "" {
			out["path:"+seg] = true
		}
	}
	return out
}

// endpointPathRoot returns the first path segment of an "METHOD /seg/..." title.
func endpointPathRoot(title string) string {
	_, rest, ok := strings.Cut(title, "/")
	if !ok {
		return ""
	}
	if j := strings.IndexAny(rest, "/ {"); j >= 0 {
		rest = rest[:j]
	}
	return strings.ToLower(strings.TrimSpace(rest))
}

// normalizeTerms lowercases, strips Portuguese diacritics, and replaces
// non-alphanumeric runs with spaces so Fields yields clean tokens.
func normalizeTerms(s string) string {
	s = strings.ToLower(s)
	var b strings.Builder
	for _, r := range s {
		switch r {
		case 'á', 'â', 'ã', 'à', 'ä':
			b.WriteRune('a')
		case 'é', 'ê', 'è', 'ë':
			b.WriteRune('e')
		case 'í', 'î', 'ì', 'ï':
			b.WriteRune('i')
		case 'ó', 'ô', 'õ', 'ò', 'ö':
			b.WriteRune('o')
		case 'ú', 'û', 'ù', 'ü':
			b.WriteRune('u')
		case 'ç':
			b.WriteRune('c')
		default:
			if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
				b.WriteRune(r)
			} else {
				b.WriteRune(' ')
			}
		}
	}
	return b.String()
}
