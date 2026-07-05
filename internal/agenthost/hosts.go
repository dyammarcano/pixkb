package agenthost

import (
	"os"
	"os/exec"
	"path/filepath"
)

func init() {
	register(func() Host { return claudeHost{} })
	register(func() Host { return codexHost{} })
	register(func() Host { return antigravityHost{} })
}

// doctorFor is the shared host health check: install root resolvable + the host
// CLI present on PATH.
func doctorFor(name, sub, bin string, base string) Report {
	r := Report{Host: name}
	root, err := homeRoot(base, sub)
	if err != nil {
		return Report{Host: name, Verdict: "FAILED", Checks: []Check{{Name: "root", Verdict: "FAIL", Detail: err.Error()}}}
	}
	r.Target = filepath.Join(root, installDir)

	parent := Check{Name: "config-dir"}
	if fi, err := os.Stat(root); err == nil && fi.IsDir() {
		parent.Verdict, parent.Detail = "PASS", root
	} else {
		// Not fatal: Install creates it. Flag as a heads-up.
		parent.Verdict, parent.Detail = "WARN", root+" (will be created)"
	}
	r.Checks = append(r.Checks, parent)

	cli := Check{Name: "cli:" + bin}
	if p, err := exec.LookPath(bin); err == nil {
		cli.Verdict, cli.Detail = "PASS", p
	} else {
		cli.Verdict, cli.Detail = "WARN", bin+" not on PATH"
	}
	r.Checks = append(r.Checks, cli)

	r.Verdict = "OK"
	for _, c := range r.Checks {
		if c.Verdict == "FAIL" {
			r.Verdict = "FAILED"
		}
	}
	return r
}

// --- Claude Code -------------------------------------------------------------

type claudeHost struct{}

func (claudeHost) Name() string                      { return "claude" }
func (claudeHost) Root(base string) (string, error)  { return homeRoot(base, ".claude") }
func (claudeHost) Files() (map[string][]byte, error) {
	return sharedFiles("Claude Code"), nil
}
func (claudeHost) Doctor(base string) Report { return doctorFor("claude", ".claude", "claude", base) }

// --- Codex -------------------------------------------------------------------

type codexHost struct{}

func (codexHost) Name() string                      { return "codex" }
func (codexHost) Root(base string) (string, error)  { return homeRoot(base, ".codex") }
func (codexHost) Files() (map[string][]byte, error) {
	return sharedFiles("Codex"), nil
}
func (codexHost) Doctor(base string) Report { return doctorFor("codex", ".codex", "codex", base) }

// --- Antigravity (agy) -------------------------------------------------------

type antigravityHost struct{}

func (antigravityHost) Name() string                     { return "agy" }
func (antigravityHost) Root(base string) (string, error) { return homeRoot(base, ".antigravity") }
func (antigravityHost) Files() (map[string][]byte, error) {
	return sharedFiles("Antigravity"), nil
}
func (antigravityHost) Doctor(base string) Report {
	return doctorFor("agy", ".antigravity", "agy", base)
}
