package ingest

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"pixkb/internal/okf"
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
	reAnexo    = regexp.MustCompile(`^ANEXO\s+(` + romanOrUnico + `|\d+)\b`)
)

// parseStatute splits Brazilian statute plain text into sections. It tracks the
// running Livro/Título/Capítulo/Seção/Subseção context and splits on "Art. N"
// markers, folding each article's following §§/incisos/alíneas into its body.
// Everything before the first article (and before the first structural heading)
// is the ementa. "ANEXO N" starts an anexo section that runs to the next anexo
// or EOF. A pure function of its input — no I/O.
// reBareArt matches a line that is only the "Art." marker (its number wrapped
// onto the following line by the PDF text layer).
var reBareArt = regexp.MustCompile(`^Art\.\s*$`)

// joinWrappedArt repairs the common PDF-extraction artifact where an article
// marker wraps as "Art." on one line and its number ("1º", "5", "31-A") on the
// next, which would otherwise defeat the line-anchored reArt. Only a bare "Art."
// immediately followed by a number-leading line is joined, so ordinary prose is
// untouched. Running-header prefixes (e.g. "LC 214/2025  Art. 5º") are NOT
// handled here — that is statute-specific and deferred to real-PDF validation
// (BACKLOG P2) to avoid tuning false positives blind.
func joinWrappedArt(text string) string {
	lines := strings.Split(text, "\n")
	out := make([]string, 0, len(lines))
	for i := 0; i < len(lines); i++ {
		if reBareArt.MatchString(strings.TrimSpace(lines[i])) && i+1 < len(lines) {
			next := strings.TrimSpace(lines[i+1])
			if next != "" && next[0] >= '0' && next[0] <= '9' {
				out = append(out, "Art. "+next)
				i++
				continue
			}
		}
		out = append(out, lines[i])
	}
	return strings.Join(out, "\n")
}

func parseStatute(text string) []statuteSection {
	text = joinWrappedArt(text)
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

	for raw := range strings.SplitSeq(text, "\n") {
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

type legislationSource struct {
	files  []string
	lei    string // statute slug, e.g. "lc-214-2025"
	domain string // e.g. "tax"
}

// NewLegislationSource builds a Source that extracts each statute PDF and emits
// one LegalArticle concept per artigo (plus a leading ementa concept and one
// Reference per Anexo), tagged with the statute (lei:), domain, and structural
// position. Offline: reads local PDFs only.
func NewLegislationSource(files []string, lei, domain string) Source {
	return &legislationSource{files: files, lei: lei, domain: domain}
}

func (s *legislationSource) Name() string { return "legislation" }

func (s *legislationSource) Fetch(_ context.Context) ([]okf.Concept, error) {
	var out []okf.Concept
	// One seen-id map shared across every file of this source, so the same lei
	// split across multiple legislation: entries disambiguates colliding article
	// IDs instead of emitting duplicates that trip GatherAll's hard dup-id abort.
	seen := map[string]bool{}
	for _, f := range s.files {
		text, err := extractPDFText(f)
		if err != nil {
			return nil, fmt.Errorf("legislation %s: %w", f, err)
		}
		secs := parseStatute(text)
		articleN := 0
		for _, sec := range secs {
			if sec.Kind == "article" {
				articleN++
			}
		}
		if articleN == 0 {
			slog.Warn("legislation: no articles parsed from statute", "path", f)
		}
		out = append(out, legislationConceptsSeen(secs, f, s.lei, s.domain, seen)...)
	}
	return out, nil
}

// legislationConcepts maps parsed statute sections to OKF concepts. Split out
// from Fetch so it is unit-testable without a real PDF.
func legislationConcepts(secs []statuteSection, resource, lei, domain string) []okf.Concept {
	return legislationConceptsSeen(secs, resource, lei, domain, map[string]bool{})
}

// legislationConceptsSeen is legislationConcepts with a caller-supplied seen-id
// map, so IDs can be disambiguated across multiple files of the same statute.
func legislationConceptsSeen(secs []statuteSection, resource, lei, domain string, seen map[string]bool) []okf.Concept {
	if lei == "" {
		lei = "lei"
	}
	var out []okf.Concept
	for _, sec := range secs {
		var id, typ string
		tags := []string{"legislacao", "lei:" + lei}
		if domain != "" {
			tags = append(tags, "domain:"+domain)
		}
		switch sec.Kind {
		case "ementa":
			id = fmt.Sprintf("legislation/%s/art-0000-ementa.md", lei)
			typ = "LegalArticle"
		case "article":
			id = fmt.Sprintf("legislation/%s/art-%s.md", lei, articleIDNum(sec.Number))
			typ = "LegalArticle"
			tags = appendStructuralTags(tags, sec)
		case "anexo":
			id = fmt.Sprintf("legislation/%s/anexo-%s.md", lei, slugify(sec.Number))
			typ = "Reference"
			tags = append(tags, "anexo")
		default:
			continue
		}
		// Disambiguate any accidental duplicate id (e.g. a mis-extracted number).
		if seen[id] {
			base := strings.TrimSuffix(id, ".md")
			for n := 2; ; n++ {
				alt := fmt.Sprintf("%s-dup%d.md", base, n)
				if !seen[alt] {
					id = alt
					break
				}
			}
		}
		seen[id] = true

		title := sec.Title
		body := "# " + title + "\n\n" + sec.Body
		out = append(out, okf.Concept{
			ID:          id,
			Type:        typ,
			Title:       title,
			Description: firstLine(sec.Body),
			Resource:    resource,
			Tags:        tags,
			Language:    "pt",
			SourceURI:   fmt.Sprintf("legislation:%s#%s", filepath.Base(resource), strings.TrimPrefix(strings.TrimPrefix(id, "legislation/"+lei+"/"), "art-")),
			Body:        body,
			ContentSHA:  okf.ComputeSHA(body),
		})
	}
	return out
}

func appendStructuralTags(tags []string, sec statuteSection) []string {
	for _, kv := range []struct{ k, v string }{
		{"livro", sec.Livro}, {"titulo", sec.Titulo}, {"capitulo", sec.Capitulo},
		{"secao", sec.Secao}, {"subsecao", sec.Subsecao},
	} {
		if kv.v != "" {
			tags = append(tags, kv.k+":"+strings.ToLower(kv.v))
		}
	}
	return tags
}

// articleIDNum turns a raw article number ("1º", "22", "31-A") into a
// zero-padded, sortable id fragment ("0001", "0022", "0031-a"). Non-numeric
// input falls back to a slug.
func articleIDNum(raw string) string {
	s := strings.ToLower(strings.TrimSpace(raw))
	num, suffix := s, ""
	if i := strings.IndexByte(s, '-'); i >= 0 {
		num, suffix = s[:i], s[i:]
	}
	num = strings.TrimRight(num, "º°.")
	n, err := strconv.Atoi(num)
	if err != nil {
		return slugify(raw)
	}
	return fmt.Sprintf("%04d%s", n, suffix)
}
