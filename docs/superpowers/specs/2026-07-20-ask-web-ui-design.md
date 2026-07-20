# Ask Web UI Design

**Goal:** A self-contained browser UI for `pixkb ask` — a question box, grounded/cited
answers, and a per-browser history of past questions — served by the pixkb binary itself.

## Context

`pixkb serve` already hosts a read-only JSON `/search` API (`cmd/pixkb/ops.go`,
`newServeCmd` + `newSearchHandler`). `pixkb ask` (`cmd/pixkb/ask.go`) runs the RAG
pipeline via `rag.Ask(ctx, Retriever, ConceptSource, Generator, q, Options)` and renders
`askJSON{answer, refused, citations:[{id,source}]}`. This feature reuses both.

Ambition tier: **T1 Personal**, air-gapped. Keep it minimal and self-contained.

## Design

### Wiring — extend `serve` (flag-gated)
- Add `--ask` (bool) and `--provider` (string, default `claude`) flags to `newServeCmd`.
- Default behavior is **unchanged**: only `/search` is registered.
- With `--ask`, also register:
  - `GET /` → serve one embedded HTML page (`//go:embed web/ask.html`), `text/html`.
  - `POST /ask` → JSON in `{ "q": "...", "type": "" }`, JSON out `askJSON`.
- The `corral.Agency` is created **once** at serve start (reusing `ask.go`'s wiring:
  `corral.NewAgency(provider, cwd)`) and closed on shutdown — never per request.
- `/ask` invokes the agent fleet, so it is not part of the "read-only" contract — that
  is why it is flag-gated rather than always on.

### Handler shape (testable)
`newAskHandler(ask func(ctx, q, typ string) (rag.Answer, rag.Grounding, error)) http.HandlerFunc`
- Decodes the JSON body; empty `q` → 400.
- Calls `ask`; on `rag.ErrRateLimited` → 429; other errors → 500.
- On success → 200 JSON `askJSON` (same builder as `renderAnswer`'s JSON path).
- `serve` builds the closure capturing store, embedder, agency, bundle dir, latest epoch,
  and a shared answer cache; the default PII filter stays on.

### The page (`web/ask.html`)
Self-contained (inline CSS + JS, no CDNs — air-gap clean):
- Question `<textarea>` + Ask button; Ctrl/⌘+Enter submits.
- Answer panel: answer text, a "refused" badge when `refused`, and a citations list
  (`id` + bundle `source` path).
- Sidebar **history** list from `localStorage` (key `pixkb.ask.history`): array of
  `{q, answer, refused, citations, ts}`, newest first, capped (e.g. 100). Clicking an
  entry re-displays it; a Clear button empties it.
- Loading and error states handled; POSTs to `/ask`.

## Files
- `cmd/pixkb/web/ask.html` — the UI (new).
- `cmd/pixkb/ops.go` — flags, `//go:embed`, `newAskHandler`, route registration, agency
  lifetime.
- `cmd/pixkb/ops_test.go` — `newAskHandler` httptest coverage with a stub `ask` closure:
  success JSON shape, empty-`q` 400, rate-limited 429, generic error 500; plus serve
  wiring (`--ask`/`--provider` flags present) and the page served as `text/html`.

## Testing
Handler tests use a stub `ask` closure — no live DB, no agent fleet — so they run in
`-short`. Follows the existing `httptest` pattern in `ops_test.go`.

## Out of scope
Server-side history persistence, auth, multi-user, streaming responses, editing/replay
beyond re-display. (Deferred — T1 personal.)
