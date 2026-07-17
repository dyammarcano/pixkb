package ingest

import (
	"regexp"
	"strings"
)

// statuteSection is one unit of a parsed statute: the leading ementa, one
// artigo (with its §§/incisos/alíneas folded into Body), or one Anexo. Livro..
// Subsecao carry the structural context (roman numeral or "ÚNICO") in force at
// the article; empty means that level is not set.
type statuteSection struct {
	Kind     string // "ementa" | "article" | "anexo"
	Number   string // article raw number ("1º", "31-A"); "" for ementa; anexo label for anexo
	Title    string // display title (e.g. "Art. 1º", "ANEXO I", "Ementa")
	Body     string
	Livro    string
	Titulo   string
	Capitulo string
	Secao    string
	Subsecao string
}

const romanOrUnico = `[IVXLCDM]+|[ÚUÙ]NIC[OA]`

var (
	reLivro    = regexp.MustCompile(`(?i)^LIVRO\s+(` + romanOrUnico + `)\b`)
	reTitulo   = regexp.MustCompile(`(?i)^T[ÍI]TULO\s+(` + romanOrUnico + `)\b`)
	reCapitulo = regexp.MustCompile(`(?i)^CAP[ÍI]TULO\s+(` + romanOrUnico + `)\b`)
	reSecao    = regexp.MustCompile(`(?i)^SE[ÇC][ÃA]O\s+(` + romanOrUnico + `)\b`)
	reSubsecao = regexp.MustCompile(`(?i)^SUBSE[ÇC][ÃA]O\s+(` + romanOrUnico + `)\b`)
	reArt      = regexp.MustCompile(`^Art\.\s*(\d+[º°]?(?:-[A-Za-z])?)`)
	reAnexo    = regexp.MustCompile(`(?i)^ANEXO\s+(\S+)`)
)

// parseStatute splits Brazilian statute plain text into sections. It tracks the
// running Livro/Título/Capítulo/Seção/Subseção context and splits on "Art. N"
// markers, folding each article's following §§/incisos/alíneas into its body.
// Everything before the first article (and before the first structural heading)
// is the ementa. "ANEXO N" starts an anexo section that runs to the next anexo
// or EOF. A pure function of its input — no I/O.
func parseStatute(text string) []statuteSection {
	var out []statuteSection
	var ctx statuteSection // holds current Livro..Subsecao
	var cur *statuteSection
	inAnexo := false

	flush := func() {
		if cur == nil {
			return
		}
		cur.Body = strings.TrimSpace(cur.Body)
		if cur.Kind == "ementa" && cur.Body == "" {
			cur = nil
			return
		}
		out = append(out, *cur)
		cur = nil
	}

	// Seed an ementa collector; it flushes at the first structural heading or Art.
	cur = &statuteSection{Kind: "ementa", Title: "Ementa"}

	for _, raw := range strings.Split(text, "\n") {
		ln := strings.TrimSpace(raw)

		// Anexo heading: starts a new anexo section (structural headings inside
		// anexo bodies are ignored — anexos are tables, not articles).
		if m := reAnexo.FindStringSubmatch(ln); m != nil {
			flush()
			inAnexo = true
			cur = &statuteSection{Kind: "anexo", Number: m[1], Title: ln}
			continue
		}
		if inAnexo {
			cur.Body += raw + "\n"
			continue
		}

		// Structural headings update context and end the current section. They
		// reset all lower levels.
		if m := reLivro.FindStringSubmatch(ln); m != nil {
			flush()
			ctx.Livro, ctx.Titulo, ctx.Capitulo, ctx.Secao, ctx.Subsecao = up(m[1]), "", "", "", ""
			continue
		}
		if m := reTitulo.FindStringSubmatch(ln); m != nil {
			flush()
			ctx.Titulo, ctx.Capitulo, ctx.Secao, ctx.Subsecao = up(m[1]), "", "", ""
			continue
		}
		if m := reCapitulo.FindStringSubmatch(ln); m != nil {
			flush()
			ctx.Capitulo, ctx.Secao, ctx.Subsecao = up(m[1]), "", ""
			continue
		}
		if m := reSubsecao.FindStringSubmatch(ln); m != nil {
			flush()
			ctx.Subsecao = up(m[1])
			continue
		}
		if m := reSecao.FindStringSubmatch(ln); m != nil {
			flush()
			ctx.Secao, ctx.Subsecao = up(m[1]), ""
			continue
		}

		// Article heading: start a new article, inheriting the current context.
		if m := reArt.FindStringSubmatch(ln); m != nil {
			flush()
			cur = &statuteSection{
				Kind:     "article",
				Number:   m[1],
				Title:    "Art. " + m[1],
				Livro:    ctx.Livro,
				Titulo:   ctx.Titulo,
				Capitulo: ctx.Capitulo,
				Secao:    ctx.Secao,
				Subsecao: ctx.Subsecao,
			}
			cur.Body += raw + "\n"
			continue
		}

		// Any other line accumulates into the current section (ementa or article).
		// Lines between a structural heading and the next article (e.g. the
		// heading's descriptive name) have cur == nil and are dropped.
		if cur != nil {
			cur.Body += raw + "\n"
		}
	}
	flush()

	// If no articles or anexos were found, return empty (even if an ementa was collected).
	var hasContent bool
	for _, sec := range out {
		if sec.Kind != "ementa" {
			hasContent = true
			break
		}
	}
	if !hasContent {
		return nil
	}
	return out
}

// up upper-cases a roman-numeral/ÚNICO context token for stable tag slugs.
func up(s string) string { return strings.ToUpper(strings.TrimSpace(s)) }
