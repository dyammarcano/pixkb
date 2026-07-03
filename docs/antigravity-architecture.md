# Antigravity (`agy`) — How the App Works

Reverse-engineered with unravel + binary strings from the installed
`%LOCALAPPDATA%\agy\bin\agy.exe` (2026-06-23). Captured here because pixkb's
`agy` provider and its usage call (`pkg/agents/agy/`) target this app; this is
the BACEN-irrelevant tooling context, kept separate from the KB.

## Binary
- **Go 1.27** PE binary (`go1.27-20260615-RC00 … X:boringcrypto,simd`), 153 MB.
- **Not obfuscated** (unravel garble_detect = NONE, 0.25). Symbol table stripped
  (`-s -w`) but build info present and all strings readable.
- Self-updating: installed by `install.cmd` from
  `https://antigravity-cli-auto-updater-974169037036.us-central1.run.app` to
  `%LOCALAPPDATA%\agy\bin\agy.exe`; updates itself in the background.

## Architecture (three layers in one binary)
1. **TUI frontend** — what the user drives in the terminal (statusline/title
   examples in the public `antigravity-cli` repo feed off its stdin JSON).
2. **Embedded `exa` LanguageServerService** — the **Windsurf/Codeium "Cascade"**
   engine (Google acquired the Windsurf team). gRPC service
   `exa.language_server_pb.*` with hundreds of RPCs: `Cascade*` (the agent loop),
   browser control (`CaptureScreenshot`, `CaptureConsoleLogs`,
   `AddToBrowserWhitelist`, DevTools), git worktrees (`CheckoutWorktree`),
   agent teams (`AgentTeamTask`), audio streaming, auth (`AcceptTermsOfService`,
   `AuthLogout`). Config/state under `~/.gemini/jetski/brain/`.
3. **`gemini_coder` execution engine** — the agentic step runner:
   `Init`, `Execute`, `ExecuteCommand`, `AddSteps`, `CancelSteps`, `Revert`,
   `ConversationState`, `ExecutionStatus`. This applies/reverts code edits.

The TUI talks to the local language server over proto; the language server
proxies model + account calls to Google's backend.

## Backend — Google Code Assist
- **`cloudcode-pa.googleapis.com`** (canary: `daily-cloudcode-pa…`) — the Gemini
  **Code Assist** API, namespace `google.internal.cloud.code.v1internal`.
  Key `/v1internal:` RPCs: `loadCodeAssist`, `onboardUser`, `fetchUserInfo`,
  `generateContent` / `generateChat` / `generateCode`, `countTokens`,
  `listAgents`, `listModelConfigs`, `recordClientEvent`, and
  **`retrieveUserQuotaSummary`** (the `/usage` data — see
  [agents-usage-signals.md](agents-usage-signals.md)).
- Onboarding flow (gemini-cli parity): `loadCodeAssist` → `onboardUser` resolves
  the `cloudaicompanionProject`, then content/quota calls are scoped to it.
- Other hosts seen: `businessaicode.googleapis.com`,
  `alkalimakersuiteapplets.pa.googleapis.com`, `iamcredentials.googleapis.com`,
  `play.googleapis.com`, and internal Google `jetski.corp.google.com` /
  `gaiastaging.corp.google.com` (the "jetski/brain" backend).

## Auth & config (shared with gemini-cli)
- **Google OAuth2** via `accounts.google.com` + `oauth2.googleapis.com`
  (mTLS variant `oauth2.mtls.googleapis.com`).
- Config dir **`~/.gemini`** (it imports gemini-cli config): `oauth_creds.json`
  (bearer — `access_token`/`refresh_token`/`expiry_date` ms), `google_accounts.json`
  (`active`/`old`), `settings.json` (experimental/hooks/mcpServers/security/
  statusLine), `installation_id`, `projects.json`, plus `~/.gemini/jetski/brain/`
  and `~/.gemini/antigravity*` working dirs.

## Integrations
- `github.com` / `raw.githubusercontent.com` (repos), `api.figma.com` (design),
  MCP servers (via `settings.json` `mcpServers`).

## Takeaway for pixkb
Antigravity is **Windsurf's Cascade agent rebadged onto Gemini via Google Code
Assist**. Its usage/quota is the Code Assist `retrieveUserQuotaSummary` call with
the user's Google OAuth token — which is exactly what `pkg/agents/agy/usage.go`
reproduces. Nothing here belongs in the BACEN KB; this doc is provider tooling
context only.
