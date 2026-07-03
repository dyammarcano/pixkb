# Agent Usage / Limit Signals (per vendor)
<!-- rev:005 -->

Where each subscription coding agent exposes usage, and what pixkb's
`agents.UsageReporter` can read for session-limit monitoring (the Agency gates
every run on it — see `pkg/agents/monitor.go`). Researched 2026-06-23 against
the upstream repos.

| Vendor | CLI source public? | Subscription limit query? | `UsageReporter`? |
|--------|--------------------|---------------------------|------------------|
| **Codex** (openai/codex) | Yes (Rust, `codex-rs/`) | **Yes — `GET /wham/usage`** | **Implemented** (real API call) |
| **Claude Code** (anthropics/claude-code) | No (minified npm) | **Yes — `GET /api/oauth/usage`** | **Implemented** (real API call) |
| **Antigravity** (google-antigravity/antigravity-cli) | No (binary; Code Assist API) | Yes — `:retrieveUserQuotaSummary` (entitlement-gated) | **Implemented**, but 403 with the shared `~/.gemini` token |

How each `Usage()` reuses the agent's OWN config + the real call it makes:
- **Claude** loads `~/.claude/.credentials.json` (`claudeAiOauth.accessToken`) and
  issues the exact `GET https://api.anthropic.com/api/oauth/usage` the `/usage`
  panel makes (`Authorization: Bearer` + `anthropic-beta: oauth-2025-04-20`). It
  does NOT refresh the token (refresh rotates the one-time refresh_token and the
  agent persists it; refreshing out-of-band would corrupt the login) — an expired
  token surfaces a clear error instead.
- **Codex** loads `~/.codex/auth.json` (`tokens.access_token` or `OPENAI_API_KEY`,
  + `tokens.account_id`) and issues the dedicated
  `GET https://chatgpt.com/backend-api/wham/usage` the CLI's `/usage` and
  `/status` make (`Authorization: Bearer` + `ChatGPT-Account-Id`,
  `User-Agent: codex_cli_rs`). When that call fails (offline / not logged in) it
  falls back to the `/responses` rate-limit headers the last turn persisted to
  `~/.codex/sessions`. (Both carry the same primary=5h / secondary=weekly
  windows.)

## Codex — `/status` and `/usage`

- `/status` ("show current session configuration and token usage") renders the
  rate-limit **windows**: primary = rolling 5h, secondary = weekly.
  Source: `codex-rs/tui/src/slash_command.rs`, `chatwidget/rate_limits.rs`,
  `status/mod.rs`; window schema in
  `codex-rs/codex-backend-openapi-models/src/models/rate_limit_*`.
- `/usage` ("view account usage or use a usage limit reset") is a menu:
  **Show usage** (recent account token activity, `GET /wham/profiles/me`) +
  **Redeem usage limit reset** (`POST /wham/rate-limit-reset-credits/consume`).
- **The dedicated rate-limit endpoint** (what `/status` and the usage menu read):
  `BackendClient::get_rate_limits_with_reset_credits` →
  `GET {chatgpt_base_url}/wham/usage`
  (`codex-rs/backend-client/src/client/rate_limit_resets.rs`). `chatgpt_base_url`
  defaults to `https://chatgpt.com/backend-api`; the client appends `/wham/usage`
  for the ChatGPT path style. Headers: `Authorization: Bearer <token>`,
  `ChatGPT-Account-Id: <account_id>`, `User-Agent: codex_cli_rs` (no
  originator/beta on this GET). Response = `RateLimitStatusPayload`:
  `{plan_type, rate_limit:{primary_window, secondary_window}}` where each window
  is `{used_percent:int, limit_window_seconds:int, reset_after_seconds:int,
  reset_at:epoch-seconds}`. primary = 5h, secondary = weekly.
- pixkb makes that exact call in `pkg/agents/codex/api.go` (`FetchUsage`). The
  `/responses`-header reader (`ReadUsage`, from `~/.codex/sessions`) is kept only
  as an offline fallback. The window/payload structs are pinned with blob SHAs in
  `pkg/agents/codex/upstream.go`; `pixkb agents upstream --check` detects drift.

## Claude Code — `/status`, `/usage`

- The `anthropics/claude-code` repo is docs/issues only; the handler was
  recovered from the published npm bundle (v2.0.30 `cli.js`, the last JS build
  before the native binary).
- `/usage` makes a real dedicated call:
  `GET https://api.anthropic.com/api/oauth/usage`,
  `Authorization: Bearer <claudeAiOauth.accessToken>`,
  `anthropic-beta: oauth-2025-04-20`, `User-Agent: claude-code/<ver>` (no
  `anthropic-version` on the `/api/oauth/*` route).
- Credentials: `~/.claude/.credentials.json` (`$CLAUDE_CONFIG_DIR` overrides),
  `{claudeAiOauth:{accessToken,refreshToken,expiresAt(ms),scopes,subscriptionType}}`;
  macOS keychain fallback service `Claude Code-credentials`. Refresh endpoint
  `POST https://console.anthropic.com/v1/oauth/token` (client_id
  `9d1c250a-e61b-44d9-88ed-5944d1962f5e`) — pixkb does NOT call it (see above).
- Response: `{five_hour, seven_day, seven_day_opus}`, each
  `{utilization:0..100, resets_at:ISO-8601}` (any may be null). pixkb maps these
  to windows `5h` / `weekly` / `weekly-opus`. Implemented in
  `pkg/agents/claude/usage.go`.

## Antigravity — `/usage`

Antigravity rides Google's **Code Assist API** (the gemini-cli backend). The
`agy` binary embeds the Codeium/Windsurf "exa" language server, which proxies to
`cloudcode-pa.googleapis.com` — but the quota call is reproducible standalone
with the user's own Google OAuth token (recovered from `agy.exe` strings + the
on-disk `~/.gemini` config).

- **Endpoint:** `POST https://cloudcode-pa.googleapis.com/v1internal:retrieveUserQuotaSummary`
  (proto `google.internal.cloud.code.v1internal.RetrieveUserQuotaSummaryRequest`/
  `Response`; local gRPC `exa.language_server_pb.RetrieveUserQuotaSummary`). The
  request carries `cloudaicompanionProject` (pixkb sends it only when
  `GOOGLE_CLOUD_PROJECT` is set).
- **Credentials:** `~/.gemini/oauth_creds.json` (shared with gemini-cli) —
  `{access_token, refresh_token, token_type, id_token, expiry_date(ms), scope}`.
  Bearer = `access_token`. pixkb does NOT refresh (Google rotates/persists; the
  agent owns it) — an expired token surfaces a clear error.
- **Response:** quota groups (`QuotaSummaryGroup`) → buckets
  (`QuotaSummaryBucket`) with `displayName`, `consumed`, `limit`, and
  `bucketInfo.{remaining,remainingAmount,remainingFraction}`. used_percent =
  `(1-remainingFraction)*100` else `consumed/limit*100`.
- pixkb implements this in `pkg/agents/agy/usage.go` (`FetchUsage`). The exact
  field nesting was recovered from the binary (not a live response), so the
  decoder walks the JSON tolerantly for any bucket-shaped object; failures are
  non-blocking.

### Live MITM capture (2026-06-24) — the exact call + the entitlement blocker

A self-signed MITM proxy (Go, CA trusted in the user store, `HTTPS_PROXY` →
`agy`) captured the real request on a cold `agy` start:

```
POST https://daily-cloudcode-pa.googleapis.com/v1internal:retrieveUserQuotaSummary
User-Agent: antigravity/cli/1.0.11 windows/amd64
body: {"project":"<cloudaicompanionProject>"}   ← from loadCodeAssist
→ 200 (gzip)
```

Corrections this proved vs the earlier strings-only guess: the host is the
**daily** channel (`daily-cloudcode-pa`, not prod `cloudcode-pa` — prod 403s),
the body field is `project` (not `cloudaicompanionProject`), and the
**`antigravity/cli/...` User-Agent is required**. `pkg/agents/agy/usage.go` now
sends exactly this.

**The blocker (why agy usage is not standalone-reproducible):** the call needs
agy's OWN access token, which carries the Antigravity-Pro entitlement. The token
in `~/.gemini/oauth_creds.json` is **gemini-cli's** (its refresh_token is bound
to the gemini-cli OAuth client `681255809395-…`; the two antigravity client IDs
embedded in `agy.exe` return `401 unauthorized_client` for it). Replaying the
exact captured request with the `~/.gemini` token returns **403
PERMISSION_DENIED**. agy mints its entitled token through a separate auth flow
(jetski / its own OAuth client) that is not persisted to any readable file, so
`FetchUsage` surfaces a clear 403-entitlement error instead of pretending.
Codex + Claude usage, by contrast, are fully working and verified live.

## Design consequence

`UsageReporter` is an **optional** capability. Only Codex implements it; the
Agency's `checkLimit` never blocks work on a provider that lacks it, nor on a
missing snapshot — monitoring only ever pauses on a positive over-limit signal.
If Claude/Antigravity later expose a subscription-limit query (file or endpoint),
add a `Usage()` to that provider and pin its source the same way Codex does.
