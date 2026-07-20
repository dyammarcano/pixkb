# Ingest + Enrichment Expansion — Roadmap & Decisions

Captures the multi-subsystem expansion requested 2026-07-20 and the decisions taken,
so the sub-projects stay coherent and the air-gap posture is explicit.

## Sub-projects (build order)
1. **Dump/Ingest UI** — drag-drop files + paste URLs → staging inbox → explicit "Ingest
   now". Backed by a new generic `ingest.NewInboxSource`. (Designed + approved; building.)
2. **OpenRouter enrichment** — opt-in cloud provider for the `enrich`/`answerer` agents,
   for higher-quality KB enrichment. Selected explicitly; not the default.
3. **Vision analysis** — dropped images → description/OCR concepts (uses the cloud
   provider from #2 + the async job layer from #3).
4. **Whisper transcription** — dropped audio → transcript concepts, fully local via
   github.com/dyammarcano/go-whisper.cpp (cgo bindings, BSD-3). Needs the async job layer.

## Decisions (2026-07-20)
- **Air-gap stays the default.** OpenRouter + vision are **opt-in** cloud providers,
  chosen via provider flag/config + API key. The sealed-KB guarantee holds by default.
- **Build order:** #1 Dump UI → #2 OpenRouter → #4 Vision → #3 Whisper.

## Tensions to respect
- **Cloud break (OpenRouter/vision):** the KB is designed "no metered API" (`ask.go`).
  Cloud paths must be explicitly selected; never the default. `corral` registers only
  `codex/claude/agy` today — OpenRouter needs a provider adapter (verify corral
  extensibility) or a separate client outside the agency.
- **cgo break (whisper):** go-whisper.cpp needs `CGO_ENABLED=1`, a C toolchain at build,
  and a `ggml-*.bin` model at runtime (WAV 16 kHz mono in). Breaks the current pure-Go /
  distroless single-binary build — its own track. Runs offline (air-gap friendly).
- **Async layer:** transcription + vision are slow → a background job queue with status is
  shared infra for #3 and #4 (not needed for #1/#2).

## #1 Dump/Ingest UI — design (approved)
- **Backend:** `ingest.NewInboxSource(dir)` walks `<IngestDir>/inbox/`, classifies by
  extension, and delegates to existing extractors (`NewPDFSource`/`NewDocxSource`/
  `NewXlsxSource`/`NewOpenAPISource` each take `[]string`); `.md/.txt` → text concept;
  unknown → an **Attachment** concept (filename + size + mime; file stays on disk). Wired
  into `buildSources` so `pixkb ingest` folds the inbox in.
- **Server (extends `serve --ask`):** `POST /inbox/upload` (multipart), `POST /inbox/url`
  (fetch→markdown, else link-only), `GET /inbox` (list), `DELETE /inbox?name=`,
  `POST /inbox/ingest` (run pipeline, return epoch delta).
- **UI:** a second tab — drop zone, URL input, staged list, "Ingest now".
- **Guards:** ingest mutates the configured KB → behind the button + `--ask`; staged files
  under `IngestDir/inbox/` (gitignored) until ingested.
