# pixkb Ingestion Sources
<!-- rev:001 -->

Authoritative upstream BACEN/gov sources for the Pix/SPB knowledge base. Each row
records the URL, kind, target ingest adapter, and status. New URLs are added here
first (`pending`) and promoted to `ingested` once wired into `pixkb.yaml` and a
re-ingest has run.

## BACEN Pix specification sources

| # | URL | Kind | Adapter | Status | Notes |
|---|-----|------|---------|--------|-------|
| 1 | https://www.bcb.gov.br/content/estabilidadefinanceira/pix/Regulamento_Pix/II_ManualdePadroesparaIniciacaodoPix.pdf | PDF | pdf | ingested | Manual de Padrões para Iniciação (local copy in `pixkb.yaml pdfs`) |
| 2 | https://www.bcb.gov.br/estabilidadefinanceira/pix-seguranca | HTML | web→markdown | pending | Security requirements (mTLS, certs) — targets weak `seguranca` case |
| 3 | https://www.bcb.gov.br/estabilidadefinanceira/pix-normas | HTML | web→markdown | pending | Normas / regulations index |
| 4 | https://www.bcb.gov.br/content/estabilidadefinanceira/pix/API-DICT.html | HTML | apidoc | pending | DICT API reference (apidoc adapter) |
| 5 | https://www.bcb.gov.br/estabilidadefinanceira/pix-cobranca | HTML | web→markdown | pending | Cobrança (charges) overview |
| 6 | https://www.bcb.gov.br/estabilidadefinanceira/pix-automatico | HTML | web→markdown | pending | Pix Automático (recurrence) — targets weak `recorrencia` case |
| 7 | https://www.bcb.gov.br/estabilidadefinanceira/sistemapagamentosinstantaneos?ano=2026 | HTML | web→markdown | pending | SPI overview / settlement — targets weak `liquidacao-spi` case |
| 8 | https://www.bcb.gov.br/content/estabilidadefinanceira/spi-pdf/participantes-spi-20260623.pdf | PDF | pdf | pending | SPI participants list (snapshot 2026-06-23) |
| 9 | https://dadosabertos.bcb.gov.br/dataset/pix | Data portal | manual | pending | Pix open-data datasets (CSV/JSON endpoints — separate data adapter) |

## Government service references

| # | URL | Kind | Adapter | Status | Notes |
|---|-----|------|---------|--------|-------|
| 10 | https://www.gov.br/pt-br/servicos/emitir-relatorio-de-chaves-pix | HTML | web→markdown | pending | gov.br: emit Pix-keys report (citizen service) |

## Not ingestable

| URL | Reason |
|-----|--------|
| blob:https://www.bcb.gov.br/b55eb96d-3352-4352-9575-ecd441b5de52 | Browser-session `blob:` URL — content exists only in a live page session, not fetchable server-side. Needs the originating page's real download URL. |

## Ingestion notes

- **BLOCKER — BCB HTML pages are JavaScript-rendered SPAs.** The `bcb.gov.br`
  `estabilidadefinanceira/*` pages (#2,3,5,6,7,10) return an empty Angular shell
  to static fetchers — both `ctx_fetch_and_index` and a plain HTTP GET yield only
  the `Banco Central do Brasil` title, no body (verified 2026-06-23). Ingesting
  their content requires a **headless browser that executes JS** (Scout MCP /
  Playwright / headless Chrome) to render, then extract to Markdown. Static
  fetch is insufficient. Until a browser-render step is wired, prefer the
  underlying **PDF regulations** linked from those pages (e.g. the Manual #1,
  Resoluções BCB) which fetch cleanly.
- **HTML pages (once rendered)** → cleaned text written to a curated Markdown
  file under `ingest/web/`, added to the `markdown:` list in `pixkb.yaml`.
  Curating to Markdown (vs raw HTML) avoids nav chrome / boilerplate polluting
  search.
- **PDFs** are downloaded locally and added to the `pdfs:` list.
- **API-DICT.html** uses the dedicated `apidoc` adapter (`api_docs:`).
- The open-data portal (#9) exposes dataset files, not prose — defer until a
  structured-data adapter exists.
