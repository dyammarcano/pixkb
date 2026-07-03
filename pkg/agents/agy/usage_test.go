package agy

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func writeCreds(t *testing.T, body string) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("GEMINI_DIR", dir)
	if err := os.WriteFile(filepath.Join(dir, "oauth_creds.json"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestFetchUsageRealCall(t *testing.T) {
	future := time.Now().Add(time.Hour).UnixMilli()
	writeCreds(t, `{"access_token":"ya29.tok","token_type":"Bearer","expiry_date":`+
		strconv.FormatInt(future, 10)+`,"refresh_token":"r"}`)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer ya29.tok" {
			t.Errorf("auth = %q", got)
		}
		if got := r.Header.Get("User-Agent"); got != antigravityUA {
			t.Errorf("user-agent = %q, want %q", got, antigravityUA)
		}
		switch r.URL.Path {
		case loadPath:
			_, _ = w.Write([]byte(`{"cloudaicompanionProject":"proj-123"}`))
		case quotaPath:
			body, _ := io.ReadAll(r.Body)
			if !strings.Contains(string(body), `"project":"proj-123"`) {
				t.Errorf("quota body missing project: %s", body)
			}
			// Shape recovered from the binary: groups -> buckets with remainingFraction.
			_, _ = w.Write([]byte(`{"quotaSummaryGroups":[{"displayName":"g","buckets":[` +
				`{"displayName":"5h","bucketInfo":{"remainingFraction":0.4}},` +
				`{"displayName":"weekly","consumed":80,"limit":100}` +
				`]}]}`))
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	}))
	defer srv.Close()
	quotaBaseURL = srv.URL
	defer func() { quotaBaseURL = "https://daily-cloudcode-pa.googleapis.com" }()

	s, err := FetchUsage(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if s == nil {
		t.Fatal("nil status")
	}
	if len(s.Windows) != 2 {
		t.Fatalf("windows = %d, want 2", len(s.Windows))
	}
	byName := map[string]float64{}
	for _, w := range s.Windows {
		byName[w.Name] = w.UsedPercent
	}
	if byName["5h"] != 60 { // 1-0.4 = 0.6 -> 60%
		t.Errorf("5h used = %v, want 60", byName["5h"])
	}
	if byName["weekly"] != 80 { // 80/100
		t.Errorf("weekly used = %v, want 80", byName["weekly"])
	}
	if s.Worst() != 80 {
		t.Errorf("worst = %v, want 80", s.Worst())
	}
}

func TestFetchUsageExpired(t *testing.T) {
	past := time.Now().Add(-time.Minute).UnixMilli()
	writeCreds(t, `{"access_token":"t","expiry_date":`+strconv.FormatInt(past, 10)+`}`)
	if _, err := FetchUsage(context.Background()); err == nil {
		t.Fatal("expected expired-token error")
	}
}

func TestFetchUsageNoCreds(t *testing.T) {
	t.Setenv("GEMINI_DIR", t.TempDir())
	if _, err := FetchUsage(context.Background()); err == nil {
		t.Fatal("expected error with no creds")
	}
}

// TestFetchUsageForbidden reproduces the real-world case: the ~/.gemini token
// (gemini-cli OAuth client) lacks the Antigravity-Pro entitlement, so the quota
// endpoint returns 403. We must surface a clear entitlement error, not a status.
func TestFetchUsageForbidden(t *testing.T) {
	future := time.Now().Add(time.Hour).UnixMilli()
	writeCreds(t, `{"access_token":"ya29.tok","expiry_date":`+strconv.FormatInt(future, 10)+`}`)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == loadPath {
			_, _ = w.Write([]byte(`{"cloudaicompanionProject":"proj-123"}`))
			return
		}
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":{"code":403,"status":"PERMISSION_DENIED"}}`))
	}))
	defer srv.Close()
	quotaBaseURL = srv.URL
	defer func() { quotaBaseURL = "https://daily-cloudcode-pa.googleapis.com" }()

	_, err := FetchUsage(context.Background())
	if err == nil || !strings.Contains(err.Error(), "entitlement") {
		t.Fatalf("want entitlement 403 error, got %v", err)
	}
}
