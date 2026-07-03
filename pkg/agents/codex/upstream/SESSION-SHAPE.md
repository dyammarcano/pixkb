# Codex session `rate_limits` shape (what ReadCodexUsage actually parses)

Acquired 2026-06-23 from a live `~/.codex/sessions/**/rollout-*.jsonl` (codex
v0.142.0). NOTE: the session serialization differs from the OpenAPI model — it
uses `window_minutes`/`resets_at` (not `limit_window_seconds`/`reset_at`). The
parser in `codexusage.go` keys on THIS shape:

```json
{
  "limit_id": "codex",
  "primary":   { "used_percent": 61.0, "window_minutes": 300,   "resets_at": 1782252797 },
  "secondary": { "used_percent": 26.0, "window_minutes": 10080, "resets_at": 1782766156 },
  "plan_type": "plus",
  "rate_limit_reached_type": null
}
```

- `primary`  = rolling 5h window (`window_minutes` 300)
- `secondary`= weekly cap (`window_minutes` 10080 = 7 days)
- `used_percent` → "% left" in /status = `100 - used_percent`
- `resets_at` = unix seconds

If `pixkb agents upstream --check` reports the pinned model files changed,
RE-VERIFY this shape against a fresh session before trusting `agents usage`.
