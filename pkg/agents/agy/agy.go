package agy

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"time"

	"pixkb/pkg/agents"
)

func init() {
	agents.RegisterProvider(func() agents.Provider { return New(Config{}) }, "agy", "antigravity")
}

// Driver drives the Antigravity CLI (`agy`) by attaching it to a pseudo-console
// (ConPTY on Windows). agy has no working headless mode — `agy --print` hangs or
// no-ops in a non-TTY (antigravity-cli issue #318) — so a real PTY is allocated,
// the prompt is run once, and the rendered answer is captured and stripped of
// ANSI escapes. It satisfies Provider, so the Agency can run any agent through
// agy as a subscription coding-agent backend. Each call spawns its own agy
// process, so callers may run several concurrently.

// Config configures the agy driver. Zero values get sane defaults.
type Config struct {
	Bin     string        // agy binary; "" → "agy" (resolved via PATH)
	Model   string        // --model; "" → agy's default
	Timeout time.Duration // per-call wall clock; 0 → 120s
}

func (c Config) withDefaults() Config {
	if c.Bin == "" {
		c.Bin = "agy"
	}
	if c.Timeout <= 0 {
		c.Timeout = 120 * time.Second
	}
	return c
}

// Driver runs agy prompts through a ConPTY.
type Driver struct{ cfg Config }

// New builds an agy driver.
func New(cfg Config) *Driver { return &Driver{cfg: cfg.withDefaults()} }

// Model returns the configured model ("" = agy default).
func (d *Driver) Model() string { return d.cfg.Model }

// Name identifies this provider.
func (d *Driver) Name() string { return "agy" }

// Usage satisfies agents.UsageReporter via the real Antigravity quota call
// (POST cloudcode-pa.googleapis.com/v1internal:retrieveUserQuotaSummary) using
// the Google OAuth bearer in ~/.gemini/oauth_creds.json. See usage.go. Failures
// (offline / not logged in) are non-blocking for the Agency monitor.
func (d *Driver) Usage() (*agents.LimitStatus, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 13*time.Second)
	defer cancel()
	return FetchUsage(ctx)
}

// Run executes one agent turn through agy and returns its output. agy cannot
// enforce an output schema natively, so any schema is embedded in the prompt.
// The agent's working Dir is not yet honored (agy runs in the process cwd).
func (d *Driver) Run(ctx context.Context, req agents.RunRequest) (agents.RunResult, error) {
	prompt := req.ComposePrompt(true)
	if strings.TrimSpace(prompt) == "" {
		return agents.RunResult{}, errors.New("agy: empty prompt")
	}
	out, err := d.run(ctx, prompt)
	if err != nil {
		return agents.RunResult{}, err
	}
	return agents.RunResult{Text: out, Provider: "agy"}, nil
}

// Open returns a warm agy session. On Windows it keeps a ConPTY-attached agy
// process alive across turns so the model is warmed up once, not per call;
// elsewhere it falls back to a one-shot session. Satisfies SessionOpener, so an
// Agency drives agy through the long-running SessionPool.
func (d *Driver) Open(ctx context.Context, _ agents.Agent) (agents.Session, error) {
	return d.openSession(ctx)
}

// ansiRE strips the CSI/OSC escape sequences agy emits for its TUI render.
var ansiRE = regexp.MustCompile("\x1b\\[[0-9;?]*[ -/]*[@-~]|\x1b\\][^\x07]*\x07|\x1b[=>PX^_].*?\x1b\\\\|\x1b[=>]")

// clean strips ANSI escapes and trims surrounding whitespace.
func clean(s string) string {
	s = ansiRE.ReplaceAllString(s, "")
	s = strings.ReplaceAll(s, "\r\n", "\n")
	return strings.TrimSpace(s)
}

// stripEcho removes the prompt the agy TUI echoes back before its answer: it
// drops everything up to and including the first line of the prompt within out.
func stripEcho(out, prompt string) string {
	first, _, _ := strings.Cut(prompt, "\n")
	first = strings.TrimSpace(first)
	if first == "" {
		return out
	}
	if _, after, found := strings.Cut(out, first); found {
		return after
	}
	return out
}

// winQuote double-quotes an argument containing shell-significant characters,
// escaping embedded quotes. Used to build the CreateProcess command line.
func winQuote(s string) string {
	if s != "" && !strings.ContainsAny(s, " \t\"\n\r") {
		return s
	}
	return `"` + strings.ReplaceAll(s, `"`, `\"`) + `"`
}

var _ agents.Provider = (*Driver)(nil)
