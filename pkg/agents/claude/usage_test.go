package claude

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// writeCreds drops a .credentials.json into a temp CLAUDE_CONFIG_DIR.
func writeCreds(t *testing.T, body string) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", dir)
	if err := os.WriteFile(filepath.Join(dir, ".credentials.json"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestReadUsageRealCall(t *testing.T) {
	future := time.Now().Add(time.Hour).UnixMilli()
	writeCreds(t, `{"claudeAiOauth":{"accessToken":"tok-abc","refreshToken":"r","expiresAt":`+
		itoa(future)+`,"scopes":["user:inference"],"subscriptionType":"max"}}`)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/oauth/usage" || r.Method != http.MethodGet {
			t.Errorf("bad request line: %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer tok-abc" {
			t.Errorf("auth header = %q", got)
		}
		if got := r.Header.Get("anthropic-beta"); got != oauthBeta {
			t.Errorf("beta header = %q", got)
		}
		_, _ = w.Write([]byte(`{"five_hour":{"utilization":42.7,"resets_at":"2026-06-23T20:00:00Z"},` +
			`"seven_day":{"utilization":80,"resets_at":"2026-06-29T00:00:00Z"},` +
			`"seven_day_opus":null}`))
	}))
	defer srv.Close()
	usageBaseURL = srv.URL
	defer func() { usageBaseURL = "https://api.anthropic.com" }()

	s, err := ReadUsage(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if s == nil {
		t.Fatal("nil status")
	}
	if s.Plan != "max" {
		t.Errorf("plan = %q, want max", s.Plan)
	}
	if len(s.Windows) != 2 { // opus null -> dropped
		t.Fatalf("windows = %d, want 2", len(s.Windows))
	}
	if s.Windows[0].Name != "5h" || s.Windows[0].UsedPercent != 42.7 {
		t.Errorf("5h window wrong: %+v", s.Windows[0])
	}
	if s.Worst() != 80 {
		t.Errorf("worst = %v, want 80", s.Worst())
	}
	if s.Windows[0].ResetsAt.IsZero() {
		t.Error("resets_at not parsed")
	}
}

func TestReadUsageExpiredToken(t *testing.T) {
	past := time.Now().Add(-time.Minute).UnixMilli()
	writeCreds(t, `{"claudeAiOauth":{"accessToken":"tok","expiresAt":`+itoa(past)+`}}`)
	if _, err := ReadUsage(context.Background()); err == nil {
		t.Fatal("expected expired-token error")
	}
}

func TestReadUsageNoCreds(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir()) // empty dir, no file
	if _, err := ReadUsage(context.Background()); err == nil {
		t.Fatal("expected error when no credentials present")
	}
}

// itoa avoids strconv import churn in the table above.
func itoa(v int64) string {
	if v == 0 {
		return "0"
	}
	neg := v < 0
	if neg {
		v = -v
	}
	var b [20]byte
	i := len(b)
	for v > 0 {
		i--
		b[i] = byte('0' + v%10)
		v /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}
