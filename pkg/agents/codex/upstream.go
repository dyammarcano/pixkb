package codex

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

//go:embed upstream/*
var upstreamFS embed.FS

// UpstreamRef pins one openai/codex source file that the Codex usage tracker
// (ReadUsage) and the /status logic are derived from. Pinning the git blob
// SHA at acquisition time lets CheckDrift detect when upstream changes so the
// parsing can be re-verified. Mirrors the cc-authoring "local mirror + SHA +
// acquired-date" drift-detection pattern.
type UpstreamRef struct {
	Repo     string // "openai/codex"
	Path     string // repo-relative path
	BlobSHA  string // git blob SHA at acquisition
	Acquired string // YYYY-MM-DD
	Purpose  string // why we track it
}

// StatusRefs are the upstream files behind ReadUsage and the codex
// `/status` view. Pinned 2026-06-23 from openai/codex@main; re-check with
// `pixkb agents upstream --check`.
var StatusRefs = []UpstreamRef{
	{
		Repo: "openai/codex", Acquired: "2026-06-23",
		Path:    "codex-rs/codex-backend-openapi-models/src/models/rate_limit_window_snapshot.rs",
		BlobSHA: "b2a6c0c228572521b4e247d2f4f47258add71c8c",
		Purpose: "per-window fields (used_percent / window / reset)",
	},
	{
		Repo: "openai/codex", Acquired: "2026-06-23",
		Path:    "codex-rs/codex-backend-openapi-models/src/models/rate_limit_status_details.rs",
		BlobSHA: "ca9fdfe2406d5d03a557cd3b8018c88abe80476d",
		Purpose: "primary (5h) + secondary (weekly) windows",
	},
	{
		Repo: "openai/codex", Acquired: "2026-06-23",
		Path:    "codex-rs/codex-backend-openapi-models/src/models/rate_limit_status_payload.rs",
		BlobSHA: "066a81b524ed1c49faddd5bbb7f012ffd22f16cb",
		Purpose: "plan_type / rate_limit / credits payload",
	},
	{
		Repo: "openai/codex", Acquired: "2026-06-23",
		Path:    "codex-rs/tui/src/slash_command.rs",
		BlobSHA: "646380a2fa59c1e88df2a0479c5a0ec900b2270f",
		Purpose: "/status + /usage slash-command enum and descriptions",
	},
	{
		Repo: "openai/codex", Acquired: "2026-06-23",
		Path:    "codex-rs/tui/src/chatwidget/rate_limits.rs",
		BlobSHA: "77a0c5c7eceb2148f71870b2f749d4e50d98996b",
		Purpose: "rate-limit window math (get_limits_duration) behind /status",
	},
	{
		Repo: "openai/codex", Acquired: "2026-06-23",
		Path:    "codex-rs/tui/src/chatwidget/usage.rs",
		BlobSHA: "53925e86d90a62fdb94def98aa3a4e8d69d50f1b",
		Purpose: "/usage menu — token activity + rate-limit-reset credits",
	},
	{
		Repo: "openai/codex", Acquired: "2026-06-23",
		Path:    "codex-rs/tui/src/status/mod.rs",
		BlobSHA: "b69d6910d407ad51aa45c1aac2063114497a95f2",
		Purpose: "/status view rendering of the limit windows",
	},
	{
		Repo: "openai/codex", Acquired: "2026-06-23",
		Path:    "codex-rs/backend-client/src/client/rate_limit_resets.rs",
		BlobSHA: "b22a4ec91ad0a027f2ed8b4688a42aef7509b32e",
		Purpose: "GET /wham/usage — the dedicated rate-limit endpoint FetchUsage calls",
	},
	{
		Repo: "openai/codex", Acquired: "2026-06-23",
		Path:    "codex-rs/backend-client/src/client.rs",
		BlobSHA: "be444b6247c4f24744baacd8a1e5c97b21434887",
		Purpose: "BackendClient base-url/path-style + auth headers for /wham/usage",
	},
	{
		Repo: "openai/codex", Acquired: "2026-06-23",
		Path:    "codex-rs/app-server/src/request_processors/account_processor.rs",
		BlobSHA: "6aa624153de42fb1acb485db9a441728f5c42e42",
		Purpose: "app-server GetAccountRateLimits handler -> get_rate_limits_with_reset_credits",
	},
}

// Drift is the comparison of one pinned ref against the current upstream SHA.
type Drift struct {
	Path    string
	Pinned  string
	Current string // "" => path not found upstream
	Changed bool
}

// CheckDrift compares the pinned blob SHAs to current ones (path -> sha, e.g.
// from FetchCurrentSHAs). A Changed entry means re-verify the /status parsing in
// codexusage.go against a fresh session (see upstream/SESSION-SHAPE.md).
func CheckDrift(current map[string]string) []Drift {
	out := make([]Drift, 0, len(StatusRefs))
	for _, r := range StatusRefs {
		cur := current[r.Path]
		out = append(out, Drift{Path: r.Path, Pinned: r.BlobSHA, Current: cur, Changed: cur != r.BlobSHA})
	}
	return out
}

// FetchCurrentSHAs pulls openai/codex@main's git tree and returns path -> blob
// SHA for the pinned files. Network call — used by `agents upstream --check`,
// not by tests.
func FetchCurrentSHAs(ctx context.Context) (map[string]string, error) {
	want := make(map[string]struct{}, len(StatusRefs))
	for _, r := range StatusRefs {
		want[r.Path] = struct{}{}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		"https://api.github.com/repos/openai/codex/git/trees/main?recursive=1", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch codex tree: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("github tree: status %d", resp.StatusCode)
	}

	var tree struct {
		Tree []struct {
			Path string `json:"path"`
			Sha  string `json:"sha"`
		} `json:"tree"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tree); err != nil {
		return nil, fmt.Errorf("decode tree: %w", err)
	}
	out := make(map[string]string, len(want))
	for _, t := range tree.Tree {
		if _, ok := want[t.Path]; ok {
			out[t.Path] = t.Sha
		}
	}
	return out, nil
}

// UpstreamMirror returns an embedded mirror file (e.g.
// "codex-rate_limit_window_snapshot.rs", "SESSION-SHAPE.md").
func UpstreamMirror(name string) ([]byte, error) {
	return upstreamFS.ReadFile("upstream/" + name)
}

// UpstreamMirrors lists the embedded mirror filenames.
func UpstreamMirrors() ([]string, error) {
	entries, err := upstreamFS.ReadDir("upstream")
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names, nil
}
