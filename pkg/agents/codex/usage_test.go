package codex

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadUsage(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CODEX_HOME", home)
	dir := filepath.Join(home, "sessions", "2026", "06", "23")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	// A rollout line carrying a rate_limits event (nested under payload).
	line := `{"type":"token_count","payload":{"rate_limits":{"limit_id":"codex",` +
		`"primary":{"used_percent":61.0,"window_minutes":300,"resets_at":1782252797},` +
		`"secondary":{"used_percent":26.0,"window_minutes":10080,"resets_at":1782766156},` +
		`"plan_type":"plus"}}}` + "\n"
	if err := os.WriteFile(filepath.Join(dir, "rollout-2026-06-23T18-57-07-abc.jsonl"), []byte(line), 0o644); err != nil {
		t.Fatal(err)
	}

	u, err := ReadUsage()
	if err != nil {
		t.Fatal(err)
	}
	if u == nil {
		t.Fatal("expected a usage snapshot")
	}
	if u.PlanType != "plus" {
		t.Errorf("plan = %q, want plus", u.PlanType)
	}
	if u.Primary == nil || u.Primary.UsedPercent != 61 || u.Primary.WindowMinutes != 300 {
		t.Errorf("primary wrong: %+v", u.Primary)
	}
	if u.Primary.LeftPercent() != 39 {
		t.Errorf("5h left = %.0f, want 39", u.Primary.LeftPercent())
	}
	if u.Secondary == nil || u.Secondary.UsedPercent != 26 {
		t.Errorf("secondary wrong: %+v", u.Secondary)
	}
	if u.Exhausted() {
		t.Error("should not be exhausted at 61/26%")
	}
}

func TestReadUsageNone(t *testing.T) {
	t.Setenv("CODEX_HOME", t.TempDir())
	u, err := ReadUsage()
	if err != nil || u != nil {
		t.Fatalf("want nil,nil for empty home; got %v,%v", u, err)
	}
}
