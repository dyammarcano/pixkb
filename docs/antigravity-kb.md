# Antigravity (`agy`) — Knowledge Base

Catalog of Antigravity's RPC surface, backend hosts, auth/config, and engine,
extracted with **unravel** (`garble detect`/`strings`, `detect`, `knowledge`)
from `%LOCALAPPDATA%\agy\bin\agy.exe` (Go 1.27 PE, 146.5 MB, unobfuscated,
symbol-stripped). Companion to [antigravity-architecture.md](antigravity-architecture.md)
and [agents-usage-signals.md](agents-usage-signals.md). Provider tooling context —
NOT part of the BACEN KB.

> unravel's automated KB pipeline (`knowledge` / `kb apps`) does **not** persist a
> stripped Go binary's gRPC surface — its Go-PE analyzer yields metadata only
> ("no identity derivable", 0 endpoints), and the MCP capture tools need
> `UNRAVEL_KB_DSN` set at server start (was empty). The substance below was
> recovered via `unravel garble strings` (354,444 unique strings) + targeted
> grep, and is the de-facto KB.

## Binary
- Go `go1.27-20260615-RC00 … X:boringcrypto,simd`, windows/amd64, static,
  no DWARF/symtab, build_info present (Deps stripped). garble=NONE.
- Embedded `protodes` / `google_i` protobuf descriptor sections.
- Self-updating from `antigravity-cli-auto-updater-974169037036.us-central1.run.app`.

## Local RPC surface (embedded language server)
The TUI drives an embedded gRPC server over proto.

### `exa.language_server_pb.LanguageServerService` (Windsurf/Codeium "Cascade")
~4,125 strings. Notable methods/messages:
- **Cascade agent loop:** `StartCascade`, `CancelCascadeInvocation`,
  `CancelCascadeSteps`, `AcknowledgeCascadeCodeEdit`, `AcknowledgeCodeActionStep`.
- **Browser control:** `CaptureScreenshot`, `CaptureConsoleLogs`,
  `AddToBrowserWhitelist`, `CheckDevToolsActivePort`,
  `BrowserValidateCascadeOrCancelOverlay`.
- **Repo/VCS:** `CheckoutWorktree`, `GitStage`, `BranchInfo`, `CheckoutSummary`,
  `AddTrackedWorkspace`, `AddEnvironmentToProject`.
- **Auth/account:** `HasAuthToken`, `AuthLogout`, `AcceptTermsOfService`,
  `FetchUserInfo`, `AuthResult`; plus `jetski/language_server_pb` (OAuth state).
- **Other:** `GetMcpPrompt` (MCP), `AgentTeamTask`, `AgentMessageOrigin`,
  `AudioStreamReady/Complete`, `BattleMode` (model A/B), `FigSync`/`FigAmend`.

### `gemini_coder_go_proto.*` (agentic step runner)
~4,497 strings. `Init`, `Execute`, `ExecuteCommand`, `AddSteps`, `CancelSteps`,
`CancelExecution`, `Revert`, `ConversationState`, `ConversationKey`,
`ExecutionStatus`, `ReactiveStateUpdate`, `TaskDetails`, `Step`, `FromAgent`,
`agent_ui_toolkit`. This applies/reverts code edits step by step.

## Backend — Google Code Assist (`/v1internal:`)
Namespace `google.internal.cloud.code.v1internal`. Host
`cloudcode-pa.googleapis.com` (canary `daily-cloudcode-pa…`).
- **RPCs:** `loadCodeAssist`, `onboardUser`, `fetchUserInfo`, `generateContent`,
  `generateChat`, `generateCode`, `countTokens`, `listAgents`, `listModelConfigs`,
  `listRemoteRepositories`, `buildWithGooglePlugins`, `agentPlugins`, `Health`,
  `recordClientEvent`, **`retrieveUserQuotaSummary`** (the `/usage` data).
- **Messages:** `Agent`, `ChatMessage`, `ToolDefinition`, `UserTier`,
  `TurboModeSetting`, `UsageMetadata`, `QuotaSummaryGroup`/`Bucket`.

## Backend hosts (~358 `.googleapis.com` + Google)
`cloudcode-pa`, `aiplatform`, `businessaicode`, `secretmanager`, `cloudkms`,
`modelarmor`, `speech`, `cloudaudit`, `iamcredentials`, `play`,
`alkalimakersuiteapplets.pa` (`.googleapis.com`); `accounts.google.com`,
`oauth2.googleapis.com` (+ `oauth2.mtls`); internal `jetski.corp.google.com`,
`jetski-autopush`, `gaiastaging.corp.google.com`. Integrations: `github.com`,
`raw.githubusercontent.com`, `api.figma.com`.

## Auth & config
- **Google OAuth2** (`oauth2.Config/Token/TokenSource`, `mcp.oauthManager`) via
  `accounts.google.com` + `oauth2.googleapis.com`.
- Config dir **`~/.gemini`** (imports gemini-cli): `oauth_creds.json` (bearer),
  `google_accounts.json`, `settings.json`, `installation_id`, `projects.json`,
  `jetski/brain/`, `antigravity*` working dirs.

## Vendored (name-only; excluded from analysis)
`google3/third_party/golang/{oauth2, go/auth, grpc, protobuf}`, `jspbp`.

## Re-extract / query
```
unravel garble strings C:/Users/dyamm/AppData/Local/agy/bin/agy.exe --min-len 8
unravel garble detect  C:/Users/dyamm/AppData/Local/agy/bin/agy.exe
unravel detect         C:/Users/dyamm/AppData/Local/agy/bin/agy.exe
unravel knowledge      C:/Users/dyamm/AppData/Local/agy/bin/agy.exe -o <out>
```
Raw categorized dump cached under context-mode source `execute:go`
(`ctx_search source:"execute:go"`).
