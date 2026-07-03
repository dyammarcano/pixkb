package agents

import (
	"os"
	"os/exec"
)

// Check is one health-check result.
type Check struct {
	Name    string `json:"name"`
	Verdict string `json:"verdict"` // PASS | WARN | FAIL
	Detail  string `json:"detail,omitempty"`
}

// Report is the aggregate agy health report.
type Report struct {
	Verdict string  `json:"verdict"` // OK | DEGRADED | FAILED
	Checks  []Check `json:"checks"`
}

// Doctor runs structured health checks for the agent stack: which coding-agent
// CLIs are on PATH, whether the embedding key is set, and how many agents are
// registered. It mirrors aihost's Doctor pattern (per-check verdicts rolled up
// to one verdict).
func Doctor() Report {
	var checks []Check

	for _, bin := range []string{"codex", "claude", "agy"} {
		c := Check{Name: "cli:" + bin}
		if path, err := exec.LookPath(bin); err == nil {
			c.Verdict, c.Detail = "PASS", path
		} else {
			c.Verdict, c.Detail = "WARN", "not on PATH"
		}
		checks = append(checks, c)
	}

	// Embeddings default to deterministic hashing (offline, unmetered); the
	// agent fleet drives semantic quality over pixdb. The optional OpenAI
	// embedder is informational only.
	emb := Check{Name: "embedder", Verdict: "PASS", Detail: "hashing (offline default); agents curate pixdb"}
	if os.Getenv("OPENAI_API_KEY") != "" {
		emb.Detail = "hashing default; OpenAI embedder available (OPENAI_API_KEY set)"
	}
	checks = append(checks, emb)

	// Codex rate-limit usage is provider-specific and lives in pkg/agents/codex;
	// callers (cmd `agents doctor`) append it via codex.ReadUsage so this core
	// package stays free of provider imports.

	roster := Check{Name: "roster"}
	n := len(All())
	if n > 0 {
		roster.Verdict, roster.Detail = "PASS", fmtInt(n)+" agents registered"
	} else {
		roster.Verdict, roster.Detail = "FAIL", "no agents registered"
	}
	checks = append(checks, roster)

	// Roll up: any FAIL → FAILED; at least one coding-agent CLI must be present
	// for the stack to be usable, else DEGRADED.
	verdict := "OK"
	anyCLI := false
	for _, c := range checks {
		switch {
		case c.Verdict == "FAIL":
			verdict = "FAILED"
		case c.Name == "cli:codex" || c.Name == "cli:claude" || c.Name == "cli:agy":
			if c.Verdict == "PASS" {
				anyCLI = true
			}
		}
	}
	if verdict == "OK" && !anyCLI {
		verdict = "DEGRADED"
	}
	return Report{Verdict: verdict, Checks: checks}
}

func fmtInt(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
