package codex

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"pixkb/pkg/agents"
)

// codexUsageBaseURL is the ChatGPT backend base the Codex CLI uses for account
// reads. Overridable in tests; honors CODEX_USAGE_BASE_URL for parity with a
// custom chatgpt_base_url. The CLI normalizes chatgpt.com -> .../backend-api and
// then uses the /wham/* (ChatGptApi) path style.
var codexUsageBaseURL = func() string {
	if b := os.Getenv("CODEX_USAGE_BASE_URL"); b != "" {
		return b
	}
	return "https://chatgpt.com/backend-api"
}()

// codexUserAgent mirrors the CLI's originator (DEFAULT_ORIGINATOR = codex_cli_rs).
const codexUserAgent = "codex_cli_rs"

// authFile is the subset of ~/.codex/auth.json the usage call needs.
type authFile struct {
	OpenAIAPIKey string `json:"OPENAI_API_KEY"`
	Tokens       struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		IDToken      string `json:"id_token"`
		AccountID    string `json:"account_id"`
	} `json:"tokens"`
}

// bearer is the token the CLI authenticates with: the ChatGPT OAuth access
// token when present, else the API key.
func (a authFile) bearer() string {
	if a.Tokens.AccessToken != "" {
		return a.Tokens.AccessToken
	}
	return a.OpenAIAPIKey
}

// loadAuth reads $CODEX_HOME/auth.json (default ~/.codex/auth.json), the same
// file the Codex CLI authenticates from.
func loadAuth() (authFile, error) {
	home, err := codexHome()
	if err != nil {
		return authFile{}, err
	}
	b, err := os.ReadFile(filepath.Join(home, "auth.json"))
	if err != nil {
		return authFile{}, fmt.Errorf("codex auth: %w", err)
	}
	var a authFile
	if err := json.Unmarshal(b, &a); err != nil {
		return authFile{}, fmt.Errorf("codex auth parse: %w", err)
	}
	if a.bearer() == "" {
		return authFile{}, fmt.Errorf("codex auth: no access token or API key (run `codex login`)")
	}
	return a, nil
}

// apiRateWindow is one window in the /wham/usage response (5h primary / weekly
// secondary). Field names match the codex-backend RateLimitWindowSnapshot.
type apiRateWindow struct {
	UsedPercent        int   `json:"used_percent"`
	LimitWindowSeconds int   `json:"limit_window_seconds"`
	ResetAfterSeconds  int   `json:"reset_after_seconds"`
	ResetAt            int64 `json:"reset_at"` // unix epoch seconds
}

// apiRateLimit is the rate_limit object holding the two windows.
type apiRateLimit struct {
	PrimaryWindow   *apiRateWindow `json:"primary_window"`
	SecondaryWindow *apiRateWindow `json:"secondary_window"`
}

// usageResponse is the /wham/usage body (the RateLimitStatusPayload, flattened
// with the reset-credits summary). Only the fields we surface are decoded.
type usageResponse struct {
	PlanType  string        `json:"plan_type"`
	RateLimit *apiRateLimit `json:"rate_limit"`
}

// FetchUsage performs the real Codex account rate-limit call — the same
// GET {chatgpt_base_url}/wham/usage the CLI's `/usage` and `/status` make —
// authenticating with the credentials in ~/.codex/auth.json. It returns the
// vendor-neutral agents.LimitStatus. This is a dedicated endpoint (NOT the
// per-turn /responses headers): codex-rs BackendClient::get_rate_limits_with_reset_credits.
func FetchUsage(ctx context.Context) (*agents.LimitStatus, error) {
	a, err := loadAuth()
	if err != nil {
		return nil, err
	}
	url := codexUsageBaseURL + "/wham/usage"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+a.bearer())
	req.Header.Set("User-Agent", codexUserAgent)
	if a.Tokens.AccountID != "" {
		req.Header.Set("ChatGPT-Account-Id", a.Tokens.AccountID)
	}

	client := &http.Client{Timeout: 12 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("codex usage: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("codex usage: HTTP %d", resp.StatusCode)
	}
	var u usageResponse
	if err := json.NewDecoder(resp.Body).Decode(&u); err != nil {
		return nil, fmt.Errorf("codex usage decode: %w", err)
	}

	s := &agents.LimitStatus{Plan: u.PlanType, Source: "wham/usage"}
	if u.RateLimit != nil {
		addWindow(s, "5h", u.RateLimit.PrimaryWindow)
		addWindow(s, "weekly", u.RateLimit.SecondaryWindow)
	}
	if len(s.Windows) == 0 {
		return nil, nil
	}
	return s, nil
}

// addWindow appends a decoded API window to the status. reset_at is an absolute
// unix epoch (seconds); fall back to now+reset_after_seconds if it is absent.
func addWindow(s *agents.LimitStatus, name string, w *apiRateWindow) {
	if w == nil {
		return
	}
	win := agents.LimitWindow{Name: name, UsedPercent: float64(w.UsedPercent)}
	switch {
	case w.ResetAt > 0:
		win.ResetsAt = time.Unix(w.ResetAt, 0)
	case w.ResetAfterSeconds > 0:
		win.ResetsAt = time.Now().Add(time.Duration(w.ResetAfterSeconds) * time.Second)
	}
	s.Windows = append(s.Windows, win)
}
