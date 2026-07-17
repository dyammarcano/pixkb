# pixkb Ingestion Sources
<!-- rev:002 -->

Authoritative upstream BACEN/gov sources for the Pix/SPB knowledge base. Each row
records the URL, kind, target ingest adapter, and status. New URLs are added here
first (`pending`) and promoted to `ingested` once wired into `pixkb.yaml` and a
re-ingest has run.

## BACEN Pix specification sources

| # | URL | Kind | Adapter | Status | Notes |
|---|-----|------|---------|--------|-------|
| 1 | https://www.bcb.gov.br/content/estabilidadefinanceira/pix/Regulamento_Pix/II_ManualdePadroesparaIniciacaodoPix.pdf | PDF | pdf | ingested | Manual de Padrões para Iniciação (local copy in `pixkb.yaml pdfs`) |
| 2 | https://www.bcb.gov.br/estabilidadefinanceira/pix-seguranca | HTML | web→markdown | ingested | "Segurança no Pix" (MED / anti-fraud). Re-crawled 2026-07-17 (`scout knowledge --depth 0`), `web/estabilidadefinanceira-pix-seguranca`. Targets weak `seguranca` case |
| 3 | https://www.bcb.gov.br/estabilidadefinanceira/pix-normas | HTML | web→markdown | skip (no H1) | Normas index — renders as a link-only shell with no `# ` heading, so the scout-crawl extraction rule skips it. Ingest the linked **PDF regulations** instead |
| 4 | https://www.bcb.gov.br/content/estabilidadefinanceira/pix/API-DICT.html | HTML | apidoc | pending | DICT API reference (apidoc adapter) |
| 5 | https://www.bcb.gov.br/estabilidadefinanceira/pix-cobranca | HTML | web→markdown | ingested | "Pix Cobrança". Re-crawled 2026-07-17, `web/estabilidadefinanceira-pix-cobranca` |
| 6 | https://www.bcb.gov.br/estabilidadefinanceira/pix-automatico | HTML | web→markdown | ingested | "Pix Automático" (recurrence). Re-crawled 2026-07-17, `web/estabilidadefinanceira-pix-automatico`. Targets weak `recorrencia` case |
| 7 | https://www.bcb.gov.br/estabilidadefinanceira/sistemapagamentosinstantaneos?ano=2026 | HTML | web→markdown | skip (no H1) | SPI overview — link-only shell, no `# ` heading; extraction skips it. Prefer the SPI PDFs |
| 8 | https://www.bcb.gov.br/content/estabilidadefinanceira/spi-pdf/participantes-spi-20260623.pdf | PDF | pdf | pending | SPI participants list (snapshot 2026-06-23) |
| 9 | https://dadosabertos.bcb.gov.br/dataset/pix | Data portal | manual | pending | Pix open-data datasets (CSV/JSON endpoints — separate data adapter) |

## Government service references

| # | URL | Kind | Adapter | Status | Notes |
|---|-----|------|---------|--------|-------|
| 10 | https://www.gov.br/pt-br/servicos/emitir-relatorio-de-chaves-pix | HTML | web→markdown | blocked (domain) | gov.br: emit Pix-keys report. Crawls cleanly (H1 "Emitir Relatórios de Chaves Pix"), but `buildSources` hardcodes `baseURL=https://www.bcb.gov.br`, so ingesting it under the scout-crawl source records a wrong-domain `source_uri`. Needs a gov.br-based crawl source before it can be ingested with honest provenance |

## Not ingestable

| URL | Reason |
|-----|--------|
| blob:https://www.bcb.gov.br/b55eb96d-3352-4352-9575-ecd441b5de52 | Browser-session `blob:` URL — content exists only in a live page session, not fetchable server-side. Needs the originating page's real download URL. |

## Ingestion notes

- **RESOLVED (2026-07-17) — BCB SPA pages now render via `scout knowledge`.** The
  `bcb.gov.br/estabilidadefinanceira/*` pages are JS-rendered Angular SPAs that
  return an empty shell to static fetchers. `scout knowledge <url> --depth 0`
  renders them headless and writes per-page Markdown; `--depth 0` avoids the
  link-wandering that had polluted the KB with 47 off-topic `acessoinformacao`
  pages. Pages #2/#5/#6 re-crawled and ingested this way. Two follow-on limits
  surfaced:
  - **Some Pix pages carry no `# ` H1** (#3 pix-normas, #7 SPI) — they are
    link-index shells, and the scout-crawl extraction rule (which keys the title
    off the first `# ` line) skips them. Their real content is in the linked
    PDFs; ingest those instead.
  - **`ingest` is not runnable on a checkout lacking the manual PDF.** `pixkb.yaml`
    pins an absolute `pdfs:` path (`C:/Users/.../II_Manual...pdf`); `ingest`
    gathers ALL sources and is all-or-nothing, so a missing PDF aborts the whole
    run. The 2026-07-17 re-ingest was therefore applied via `reindex` from a
    bundle regenerated for the web source only. Tracked in BACKLOG.
- **HTML pages (once rendered)** → cleaned text written to a curated Markdown
  file under `ingest/web/`, added to the `markdown:` list in `pixkb.yaml`.
  Curating to Markdown (vs raw HTML) avoids nav chrome / boilerplate polluting
  search.
- **PDFs** are downloaded locally and added to the `pdfs:` list.
- **API-DICT.html** uses the dedicated `apidoc` adapter (`api_docs:`).
- The open-data portal (#9) exposes dataset files, not prose — defer until a
  structured-data adapter exists.
