package codex

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"pixkb/pkg/agents"
)

// Window is one rate-limit window (the 5h primary or the weekly secondary),
// mirroring what the codex `/status` view shows.
type Window struct {
	UsedPercent   float64 `json:"used_percent"`
	WindowMinutes int     `json:"window_minutes"`
	ResetsAt      int64   `json:"resets_at"`
}

// LeftPercent is the headroom ("% left" in /status).
func (w Window) LeftPercent() float64 { return 100 - w.UsedPercent }

// ResetTime is the window reset as a local time.
func (w Window) ResetTime() time.Time { return time.Unix(w.ResetsAt, 0) }

// Usage is the latest Codex rate-limit snapshot, the same data `/status`
// renders. Primary is the rolling 5h window; Secondary is the weekly cap.
type Usage struct {
	PlanType  string
	Primary   *Window
	Secondary *Window
	Source    string // session rollout file the snapshot came from
}

// Exhausted reports whether either window is at/over its limit — the signal the
// control loop uses to pause Codex work before it errors out.
func (u *Usage) Exhausted() bool {
	return (u.Primary != nil && u.Primary.UsedPercent >= 100) ||
		(u.Secondary != nil && u.Secondary.UsedPercent >= 100)
}

// Status converts the Codex snapshot to the vendor-neutral agents.LimitStatus
// the Agency monitors. Returns nil when there is no snapshot.
func (u *Usage) Status() *agents.LimitStatus {
	if u == nil {
		return nil
	}
	s := &agents.LimitStatus{Plan: u.PlanType, Source: u.Source}
	if u.Primary != nil {
		s.Windows = append(s.Windows, agents.LimitWindow{
			Name: "5h", UsedPercent: u.Primary.UsedPercent, ResetsAt: u.Primary.ResetTime(),
		})
	}
	if u.Secondary != nil {
		s.Windows = append(s.Windows, agents.LimitWindow{
			Name: "weekly", UsedPercent: u.Secondary.UsedPercent, ResetsAt: u.Secondary.ResetTime(),
		})
	}
	return s
}

// codexHome resolves the Codex home dir (CODEX_HOME override, else ~/.codex).
func codexHome() (string, error) {
	if h := os.Getenv("CODEX_HOME"); h != "" {
		return h, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".codex"), nil
}

// ReadUsage returns the most recent Codex rate-limit snapshot by scanning
// the session rollouts under <codex-home>/sessions newest-first. `codex exec`
// records these `rate_limits` events, so this works without an interactive
// session. Returns nil (no error) when no snapshot is found.
func ReadUsage() (*Usage, error) {
	home, err := codexHome()
	if err != nil {
		return nil, err
	}
	dir := filepath.Join(home, "sessions")
	var files []string
	_ = filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if !d.IsDir() && strings.HasSuffix(d.Name(), ".jsonl") && strings.Contains(d.Name(), "rollout-") {
			files = append(files, p)
		}
		return nil
	})
	if len(files) == 0 {
		return nil, nil
	}
	sort.Slice(files, func(i, j int) bool { return fileModTime(files[i]) > fileModTime(files[j]) })

	for _, f := range files {
		if u := scanSessionUsage(f); u != nil {
			return u, nil
		}
	}
	return nil, nil
}

func fileModTime(p string) int64 {
	fi, err := os.Stat(p)
	if err != nil {
		return 0
	}
	return fi.ModTime().UnixNano()
}

// scanSessionUsage returns the LAST rate_limits snapshot in a rollout file (the
// most recent turn), or nil.
func scanSessionUsage(path string) *Usage {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer func() { _ = f.Close() }()

	var last *Usage
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	for sc.Scan() {
		line := sc.Bytes()
		if !strings.Contains(string(line), `"rate_limits"`) {
			continue
		}
		var obj any
		if json.Unmarshal(line, &obj) != nil {
			continue
		}
		if rl := findRateLimits(obj); rl != nil {
			if u := usageFromMap(rl); u != nil {
				u.Source = filepath.Base(path)
				last = u
			}
		}
	}
	return last
}

// findRateLimits walks a decoded JSON value for the rate_limits object (the one
// carrying primary/secondary windows).
func findRateLimits(v any) map[string]any {
	switch t := v.(type) {
	case map[string]any:
		if rl, ok := t["rate_limits"].(map[string]any); ok {
			if _, ok := rl["primary"]; ok {
				return rl
			}
		}
		if _, ok := t["primary"]; ok {
			if _, ok := t["used_percent"]; !ok { // a windows-bearing object
				return t
			}
		}
		for _, val := range t {
			if r := findRateLimits(val); r != nil {
				return r
			}
		}
	case []any:
		for _, val := range t {
			if r := findRateLimits(val); r != nil {
				return r
			}
		}
	}
	return nil
}

func usageFromMap(rl map[string]any) *Usage {
	u := &Usage{Primary: parseWindow(rl["primary"]), Secondary: parseWindow(rl["secondary"])}
	if s, ok := rl["plan_type"].(string); ok {
		u.PlanType = s
	}
	if u.Primary == nil && u.Secondary == nil {
		return nil
	}
	return u
}

func parseWindow(v any) *Window {
	m, ok := v.(map[string]any)
	if !ok {
		return nil
	}
	return &Window{
		UsedPercent:   toFloat(m["used_percent"]),
		WindowMinutes: int(toFloat(m["window_minutes"])),
		ResetsAt:      int64(toFloat(m["resets_at"])),
	}
}

func toFloat(v any) float64 {
	if f, ok := v.(float64); ok {
		return f
	}
	return 0
}

// Format renders the usage like the codex /status view.
func (u *Usage) Format() string {
	var b strings.Builder
	fmt.Fprintf(&b, "Codex usage (plan: %s)\n", u.PlanType)
	row := func(label string, w *Window) {
		if w == nil {
			fmt.Fprintf(&b, "  %-13s n/a\n", label)
			return
		}
		fmt.Fprintf(&b, "  %-13s %3.0f%% left (used %.0f%%, resets %s)\n",
			label, w.LeftPercent(), w.UsedPercent, w.ResetTime().Format("Mon 02 Jan 15:04"))
	}
	row("5h limit:", u.Primary)
	row("weekly limit:", u.Secondary)
	if u.Source != "" {
		fmt.Fprintf(&b, "  source: %s\n", u.Source)
	}
	return b.String()
}
