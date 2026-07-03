package codex

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func writeAuth(t *testing.T, body string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("CODEX_HOME", home)
	if err := os.WriteFile(filepath.Join(home, "auth.json"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestFetchUsageRealCall(t *testing.T) {
	writeAuth(t, `{"OPENAI_API_KEY":"","tokens":{"access_token":"tok-xyz","account_id":"acc-1"}}`)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/wham/usage" || r.Method != http.MethodGet {
			t.Errorf("bad request: %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer tok-xyz" {
			t.Errorf("auth = %q", got)
		}
		if got := r.Header.Get("ChatGPT-Account-Id"); got != "acc-1" {
			t.Errorf("account header = %q", got)
		}
		_, _ = w.Write([]byte(`{"plan_type":"plus","rate_limit":{` +
			`"primary_window":{"used_percent":61,"limit_window_seconds":18000,"reset_at":1782252797},` +
			`"secondary_window":{"used_percent":26,"limit_window_seconds":604800,"reset_at":1782766156}},` +
			`"rate_limit_reset_credits":{"available_count":0}}`))
	}))
	defer srv.Close()
	codexUsageBaseURL = srv.URL
	defer func() { codexUsageBaseURL = "https://chatgpt.com/backend-api" }()

	s, err := FetchUsage(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if s == nil {
		t.Fatal("nil status")
	}
	if s.Plan != "plus" {
		t.Errorf("plan = %q", s.Plan)
	}
	if len(s.Windows) != 2 {
		t.Fatalf("windows = %d, want 2", len(s.Windows))
	}
	if s.Windows[0].Name != "5h" || s.Windows[0].UsedPercent != 61 {
		t.Errorf("primary wrong: %+v", s.Windows[0])
	}
	if s.Windows[1].Name != "weekly" || s.Windows[1].UsedPercent != 26 {
		t.Errorf("secondary wrong: %+v", s.Windows[1])
	}
	if s.Windows[0].ResetsAt.Unix() != 1782252797 {
		t.Errorf("reset_at not decoded: %v", s.Windows[0].ResetsAt)
	}
	if s.Worst() != 61 {
		t.Errorf("worst = %v, want 61", s.Worst())
	}
}

func TestFetchUsageNoAuth(t *testing.T) {
	t.Setenv("CODEX_HOME", t.TempDir()) // no auth.json
	if _, err := FetchUsage(context.Background()); err == nil {
		t.Fatal("expected error with no auth.json")
	}
}

func TestAuthBearerPrefersAccessToken(t *testing.T) {
	a := authFile{OpenAIAPIKey: "sk-key"}
	if a.bearer() != "sk-key" {
		t.Errorf("api-key bearer = %q", a.bearer())
	}
	a.Tokens.AccessToken = "oauth-tok"
	if a.bearer() != "oauth-tok" {
		t.Errorf("oauth bearer = %q", a.bearer())
	}
}
