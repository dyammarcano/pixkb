# Official Sources — Trusted, Periodically-Gathered BACEN Sources

Design for a registry of authoritative BACEN sources that are refreshed on a
schedule and treated specially in the KB. Requested 2026-07-20.

## The sources (operator-declared official)
- Repos: `bacen/pix-api`, `bacen/pix-dict-api` (already mirrored in `repos:`)
- Repo issues: `bacen/pix-api/issues`, `bacen/pix-dict-api/issues`
- Org: `github.com/bacen`
- Pages: `bacen.github.io/pix-api`, `www.bcb.gov.br`,
  `.../pix/API-DICT.html`, `.../estabilidadefinanceira/pix`

Common host roots: `github.com/bacen`, `bacen.github.io`, `bcb.gov.br`.

## Decisions (2026-07-20)
- **Scheduling:** built-in daemon — `serve --gather-every <dur>` (0/empty = off).
- **Trust rule (all four):** official provenance tag + periodic refresh; relax
  curation gates (auto-trust); rank boost in search; track changes / notify on update.
- **Air-gap:** periodic gather needs network, so it is opt-in (off by default);
  the sealed default holds unless the operator sets an interval.

## Config
```yaml
official_sources:
  gather_every: "24h"          # daemon interval; empty/0 disables
  hosts:                        # provenance match roots
    - github.com/bacen
    - bacen.github.io
    - bcb.gov.br
  issues:                      # GitHub repos whose issues to gather
    - bacen/pix-api
    - bacen/pix-dict-api
```

## Build increments
1. **Foundation (this pass):** config (`OfficialSources{Hosts, GatherEvery, Issues}`);
   `ingest.TagOfficial(concepts, hosts)` post-gather step that adds the
   `trusted:official` tag to any concept whose SourceURI/Resource matches a host;
   `serve --gather-every <dur>` daemon goroutine running the full ingest pipeline
   on a ticker (flag overrides config; 0 = off, preserving today's behavior).
2. **GitHub issues source:** `ingest.NewIssuesSource(repos)` → one concept per open
   issue via the GitHub API (`gh`/REST), tagged official.
3. **Rank boost:** a relevance multiplier for `trusted:official` concepts in the
   hybrid ranker.
4. **Change tracking:** persist a per-source content hash/etag; on gather, record
   and log what actually changed (new issue, edited page, new release).
5. **Gate relaxation:** the curation/quality gates auto-pass `trusted:official`
   concepts (authoritative provenance) instead of applying ad-hoc-drop scrutiny.

Increments 2–5 each get their own commit; the tag from #1 is the seam they hang on.
