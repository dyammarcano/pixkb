package agy

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"pixkb/pkg/agents"
)

// quotaBaseURL is the Google Code Assist backend `agy` reads quota from. The
// installed agy 1.0.11 uses the DAILY channel host (captured live via MITM); the
// prod host cloudcode-pa.googleapis.com returns 403 for this flow. Overridable
// via ANTIGRAVITY_BASE_URL (and in tests).
var quotaBaseURL = func() string {
	if b := os.Getenv("ANTIGRAVITY_BASE_URL"); b != "" {
		return b
	}
	return "https://daily-cloudcode-pa.googleapis.com"
}()

// quotaPath / loadPath are the v1internal RPCs (JSON transcoded) agy calls.
const (
	quotaPath = "/v1internal:retrieveUserQuotaSummary"
	loadPath  = "/v1internal:loadCodeAssist"
)

// antigravityUA is the exact User-Agent agy sends; the quota endpoint gates on
// it (captured: "antigravity/cli/1.0.11 windows/amd64").
const antigravityUA = "antigravity/cli/1.0.11 windows/amd64"

// geminiCreds is the subset of ~/.gemini/oauth_creds.json the quota call needs.
// Antigravity shares gemini-cli's Google OAuth credential file.
type geminiCreds struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	TokenType    string `json:"token_type"`
	ExpiryDate   int64  `json:"expiry_date"` // epoch MILLISECONDS
}

// geminiDir resolves ~/.gemini (the dir antigravity stores its imported
// gemini-cli config + Google OAuth creds in).
func geminiDir() (string, error) {
	if d := os.Getenv("GEMINI_DIR"); d != "" {
		return d, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".gemini"), nil
}

// loadCreds reads the Google OAuth bearer from ~/.gemini/oauth_creds.json.
func loadCreds() (geminiCreds, error) {
	dir, err := geminiDir()
	if err != nil {
		return geminiCreds{}, err
	}
	b, err := os.ReadFile(filepath.Join(dir, "oauth_creds.json"))
	if err != nil {
		return geminiCreds{}, fmt.Errorf("antigravity creds: %w", err)
	}
	var c geminiCreds
	if err := json.Unmarshal(b, &c); err != nil {
		return geminiCreds{}, fmt.Errorf("antigravity creds parse: %w", err)
	}
	if c.AccessToken == "" {
		return geminiCreds{}, fmt.Errorf("antigravity creds: no access token (run `agy` / login)")
	}
	return c, nil
}

// loadProject resolves the cloudaicompanionProject the quota call must be scoped
// to. GOOGLE_CLOUD_PROJECT overrides; otherwise it is fetched from loadCodeAssist
// exactly as agy does (the project is account-specific, e.g. "stable-imprint-…").
func loadProject(ctx context.Context, token string) (string, error) {
	if p := os.Getenv("GOOGLE_CLOUD_PROJECT"); p != "" {
		return p, nil
	}
	payload, _ := json.Marshal(map[string]any{"metadata": map[string]any{"pluginType": "GEMINI"}})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, quotaBaseURL+loadPath, bytes.NewReader(payload))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", antigravityUA)
	resp, err := (&http.Client{Timeout: 12 * time.Second}).Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("loadCodeAssist HTTP %d", resp.StatusCode)
	}
	var out struct {
		CloudaicompanionProject string `json:"cloudaicompanionProject"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", err
	}
	return out.CloudaicompanionProject, nil
}

// FetchUsage performs the real Antigravity quota call, exact request captured
// live from agy 1.0.11 via MITM:
//
//	POST {daily-cloudcode-pa}/v1internal:retrieveUserQuotaSummary
//	User-Agent: antigravity/cli/1.0.11 windows/amd64
//	body: {"project":"<cloudaicompanionProject>"}
//
// IMPORTANT — entitlement: the call needs agy's OWN access token, which carries
// the Antigravity-Pro entitlement. The token in ~/.gemini/oauth_creds.json is
// gemini-cli's (different OAuth client) and the same request returns 403
// PERMISSION_DENIED with it — agy mints its entitled token through a separate
// auth flow not persisted to a readable file. So this returns a clear error on
// 403 rather than pretending. See docs/agents-usage-signals.md.
//
// The response is decoded with a tolerant walk (the gzipped 200 body's exact
// field nesting was not decodable from the capture; the walker finds any
// bucket-shaped object — displayName + remainingFraction / consumed+limit).
func FetchUsage(ctx context.Context) (*agents.LimitStatus, error) {
	c, err := loadCreds()
	if err != nil {
		return nil, err
	}
	if c.ExpiryDate > 0 && time.Now().UnixMilli() >= c.ExpiryDate {
		return nil, fmt.Errorf("antigravity oauth token expired (run `agy` to refresh)")
	}

	project, err := loadProject(ctx, c.AccessToken)
	if err != nil {
		return nil, fmt.Errorf("antigravity usage: resolve project: %w", err)
	}
	payload, err := json.Marshal(map[string]any{"project": project})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, quotaBaseURL+quotaPath, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.AccessToken)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", antigravityUA)

	client := &http.Client{Timeout: 12 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("antigravity usage: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusForbidden {
		return nil, fmt.Errorf("antigravity usage: 403 — the ~/.gemini token lacks Antigravity-Pro entitlement; usage needs agy's own login token (not standalone-reproducible)")
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("antigravity usage: HTTP %d", resp.StatusCode)
	}
	var raw any
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("antigravity usage decode: %w", err)
	}

	s := &agents.LimitStatus{Plan: "antigravity", Source: "retrieveUserQuotaSummary"}
	collectBuckets(raw, s)
	if len(s.Windows) == 0 {
		return nil, nil
	}
	return s, nil
}

// collectBuckets walks the decoded quota response and appends a LimitWindow for
// every "bucket-shaped" object — one carrying a name plus either a
// remainingFraction or a consumed+limit pair. Tolerant of the exact nesting.
func collectBuckets(v any, s *agents.LimitStatus) {
	switch t := v.(type) {
	case map[string]any:
		if w, ok := bucketWindow(t); ok {
			s.Windows = append(s.Windows, w)
		}
		for _, child := range t {
			collectBuckets(child, s)
		}
	case []any:
		for _, child := range t {
			collectBuckets(child, s)
		}
	}
}

// bucketWindow turns a quota-bucket object into a LimitWindow, reporting whether
// it looked like one.
func bucketWindow(m map[string]any) (agents.LimitWindow, bool) {
	name := firstString(m, "displayName", "name", "quotaId")
	used, ok := usedPercent(m)
	if name == "" || !ok {
		return agents.LimitWindow{}, false
	}
	w := agents.LimitWindow{Name: name, UsedPercent: used}
	if rt := firstString(m, "resetTime", "resetAt"); rt != "" {
		if t, err := time.Parse(time.RFC3339, rt); err == nil {
			w.ResetsAt = t
		}
	}
	return w, true
}

// usedPercent derives 0..100 from a bucket: prefer remainingFraction (0..1),
// else a remaining/limit or consumed/limit pair, looking in the object and a
// nested bucketInfo.
func usedPercent(m map[string]any) (float64, bool) {
	for _, src := range []map[string]any{m, nestedMap(m, "bucketInfo")} {
		if src == nil {
			continue
		}
		if f, ok := toFloat(src["remainingFraction"]); ok {
			return clampPct((1 - f) * 100), true
		}
		limit, lok := toFloat(src["limit"])
		if lok && limit > 0 {
			if consumed, ok := toFloat(src["consumed"]); ok {
				return clampPct(consumed / limit * 100), true
			}
			if rem, ok := toFloat(src["remainingAmount"]); ok {
				return clampPct((limit - rem) / limit * 100), true
			}
		}
	}
	return 0, false
}

func nestedMap(m map[string]any, key string) map[string]any {
	if v, ok := m[key].(map[string]any); ok {
		return v
	}
	return nil
}

func firstString(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if s, ok := m[k].(string); ok && strings.TrimSpace(s) != "" {
			return s
		}
	}
	return ""
}

func toFloat(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case json.Number:
		f, err := n.Float64()
		return f, err == nil
	case string:
		var f float64
		_, err := fmt.Sscan(n, &f)
		return f, err == nil
	}
	return 0, false
}

func clampPct(p float64) float64 {
	if p < 0 {
		return 0
	}
	if p > 100 {
		return 100
	}
	return p
}
