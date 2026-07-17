# Scout Crawl Ingest Source — Design Spec

**Date:** 2026-07-03
**Project:** `pixkb` (`C:\weaver-sync\development\personal\projects\bacen`)
**Status:** Draft — awaiting user approval

## Goal

Ingest BACEN's public website (`bcb.gov.br`) into the `pixkb` knowledge base, sourced
from a Scout `knowledge` crawl snapshot (markdown + PDF + screenshots + HAR +
accessibility-tree per page). Add a new `ingest.Source` — `internal/ingest/scoutcrawl.go`
— that turns the crawl's `pages/**/*.md` tree into one `okf.Concept` per page.

## Background — why this isn't a plain `NewMarkdownSource` reuse

The crawl was originally captured with `--stealth` mode (`E:\bcb\run-bcb-sitemap.ps1`)
against `www.bcb.gov.br`, an Angular SPA. Investigation during this design (verified via
the accessibility-tree snapshot, which showed `main [ref=...]` with **zero children**)
found that `--stealth` mode causes the crawl to capture the DOM before Angular finishes
rendering the route's content into `<main>` — so ~40 of 50 pages' markdown/PDF exports
were pure, near-identical site chrome (nav mega-menu + footer + cookie banner), with no
page-specific content at all.

**Fix, verified empirically:** re-running the same crawl *without* `--stealth` (via the
globally-installed `scout` binary, not `E:\bcb\scout.exe`) reliably renders real content.
Confirmed across a fresh 50-page re-crawl into `mirrors/bcb/knowledge/` (gitignored,
matching the project's existing `mirrors/` convention): 47 of 50 pages now have
substantial unique content; one page (`en.md`) still failed outright (no `<main>`
content at all — an isolated per-page timing miss, not the stealth bug); a few pages
(e.g. `acessoinformacao/cmnatasreun.md`) are legitimately thin (document-list pages with
almost no prose — a real content characteristic, not a capture failure).

This means `NewMarkdownSource` (which expects clean, pre-curated markdown and splits by
H2) is the wrong fit even on the fixed crawl: every page still carries the same ~900+
line nav/breadcrumb preamble and footer sitemap before and after the real content.

## Structural pattern found (verified across `index.md`, `estruturabc.md`,
`organograma.md`, `sml.md`, `cmnatasreun.md`, and `historiacontada/index.html.md`)

- The real page content is always bracketed by the **first line starting with `# `**
  (single-hash H1 — the page's own title; nothing before it in the file uses H1) through
  the **first occurrence of the literal string `Siga o BC`** (a fixed footer marker).
- If no `# ` line exists in the file at all, the page's capture failed outright (e.g.
  `en.md`) — skip it.
- If no `Siga o BC` marker is found after the H1 (seen once: `historiacontada/*`, a
  differently-templated static microsite, not the Angular SPA), take content through
  end-of-file instead of erroring.
- If the extracted body (excluding the H1 line itself) has fewer than ~40 non-whitespace
  characters, the page is a legitimate list/stub page with no real prose — skip it.
- The file's path relative to `pages/` mirrors the site's URL path exactly (e.g.
  `estabilidadefinanceira/sml.md` → `https://www.bcb.gov.br/estabilidadefinanceira/sml`;
  the crawl root `index.md` → `https://www.bcb.gov.br/`).

## Architecture

```
internal/ingest/
  scoutcrawl.go       NewScoutCrawlSource(dir, baseURL string) Source
                       walks dir for *.md, applies the H1→footer extraction above,
                       emits one "WebPage" concept per page (skips failed/stub pages)
  scoutcrawl_test.go   fixtures reproducing: normal page, stub page (H1 immediately
                       followed by footer), no-H1 page (skip), no-footer page (EOF
                       fallback), nested subdirectory path → URL mapping

cmd/pixkb/
  config.go            + Config.ScoutCrawlDir string `yaml:"scout_crawl_dir"`
                        (empty = source disabled; loaded the same way as MirrorDir)
  commands.go           buildSources(cfg): if cfg.ScoutCrawlDir != "", append
                        ingest.NewScoutCrawlSource(cfg.ScoutCrawlDir, "https://www.bcb.gov.br")
```

`baseURL` is a constructor parameter (testable, not hardcoded inside the source), but
`buildSources` passes the literal `"https://www.bcb.gov.br"` — this source is BCB-crawl
specific for now; no `pixkb.yaml` knob for the base URL until a second crawled site
exists (YAGNI).

## Concept shape

```go
okf.Concept{
    ID:          "web/" + slugify(relPathNoExt) + ".md",   // e.g. "web/estabilidadefinanceira-sml.md"
    Type:        "WebPage",
    Title:       h1Text,                                    // e.g. "Pagamento em moeda local"
    Description: firstLine(body),
    Resource:    absoluteFilePath,
    Tags:        []string{"web", "bcb"} + [firstPathSegment if nested],
    Language:    detectMarkdownLang(body),                  // reuse markdown.go's heuristic
    SourceURI:   sourceURL,                                 // baseURL + "/" + relPath (bare baseURL+"/" for root index.md)
    Body:        "# " + h1Text + "\n\n" + body,
    ContentSHA:  okf.ComputeSHA(body),
}
```

`slugify` and `firstLine` are existing shared helpers in `internal/ingest/pdf.go` —
reused as-is, not redefined.

## Extraction algorithm (exact)

```
1. lines := split file on "\n"
2. h1Idx := index of first line with prefix "# "   → if none: skip file
3. title := trim(line[h1Idx][2:])
4. footerIdx := index of first line containing "Siga o BC", searched from h1Idx+1
               → if none found: footerIdx = len(lines)
5. body := trim(join(lines[h1Idx+1:footerIdx], "\n"))
6. meaningfulChars := len(join(fields(body), ""))   // whitespace-insensitive count
   if meaningfulChars < 40: skip file (stub/list-only page)
7. emit concept (see shape above)
```

## Testing

`scoutcrawl_test.go`, testify, `t.TempDir()` + `os.WriteFile` fixtures (matching
`markdown_test.go`'s style) under a synthetic `pages/` tree:
- Normal page (nav preamble + H1 + real paragraphs + `Siga o BC` footer) → concept
  emitted with correct Title/Body/SourceURI.
- Nested subdirectory page (e.g. `estabilidadefinanceira/sml.md`) → SourceURI path
  mapping verified.
- Root `index.md` → SourceURI is the bare base URL (`https://www.bcb.gov.br/`), not
  `.../index`.
- Stub page (H1 immediately followed by the footer marker, no real body) → skipped,
  no concept emitted.
- No-H1 page → skipped, no concept emitted, no error returned.
- No-footer page (content runs to EOF) → full trailing content captured, no error.

`cmd/pixkb` level: a config test confirming `scout_crawl_dir` loads from `pixkb.yaml`/env
the same way other path fields do, and that `buildSources` only includes the source when
the field is non-empty.

## Out of scope (YAGNI)

- No re-crawl automation/scheduling wired into pixkb itself — the crawl stays an
  external, manually-run (or separately-scheduled) step producing a directory pixkb
  reads from, exactly like the existing `mirror_dir`/`repos` git-mirror convention.
- No PDF/HAR/screenshot/accessibility-snapshot ingestion in v1 — markdown only. The
  accessibility snapshot was used only as a diagnostic during this design.
- No per-page freshness/re-crawl-diff tracking — a full re-run of `pixkb ingest`
  reprocesses every file each time, same as every other source.
- No `pixkb.yaml` knob for `baseURL` — hardcoded to `https://www.bcb.gov.br` in
  `buildSources` until a second crawled site exists.
- No fix to `E:\bcb\run-bcb-sitemap.ps1` itself as part of this work — that script lives
  outside this repo; the finding (drop `--stealth`) is documented here for whoever
  maintains it next.
