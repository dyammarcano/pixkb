// Package agyhost installs pixkb as a self-contained agent plugin across
// multiple coding-agent hosts (Claude Code, Codex, Antigravity). It mirrors
// lensr's pkg/aihost: a lazy-factory Host registry whose members generate a
// plugin tree (agent definitions + an .mcp.json that registers `pixkb mcp
// serve`) and write it atomically into each host's config. Loading the host
// then surfaces pixkb's verbs as the agent's self-contained tool set.
package host

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"pixkb/pkg/agents"
)

// Host is one coding-agent target. The minimal surface is Name/Root/Files;
// Doctor is provided by every built-in host.
type Host interface {
	// Name is the short host id ("claude", "codex", "agy").
	Name() string
	// Root returns the host's config dir under base (base "" => default,
	// resolved from the user's home dir).
	Root(base string) (string, error)
	// Files returns plugin-tree files keyed by path relative to the install
	// dir (Root/pixkb).
	Files() (map[string][]byte, error)
	// Doctor reports install readiness.
	Doctor(base string) Report
}

// Check is one health-check line; Report rolls them up.
type Check struct {
	Name    string `json:"name"`
	Verdict string `json:"verdict"` // PASS | WARN | FAIL
	Detail  string `json:"detail,omitempty"`
}

// Report is a host's aggregate health.
type Report struct {
	Host    string  `json:"host"`
	Target  string  `json:"target"`
	Verdict string  `json:"verdict"` // OK | DEGRADED | FAILED
	Checks  []Check `json:"checks"`
}

// InstallResult summarizes one install.
type InstallResult struct {
	Host    string
	Target  string
	Written int
	Planned []string // dry-run: relative paths that would be written
}

var factories []func() Host

// register adds a host factory (called from init()).
func register(f func() Host) { factories = append(factories, f) }

// All returns every registered host, sorted by name.
func All() []Host {
	out := make([]Host, 0, len(factories))
	for _, f := range factories {
		out = append(out, f())
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name() < out[j].Name() })
	return out
}

// ByName returns the host with the given name.
func ByName(name string) (Host, bool) {
	for _, h := range All() {
		if h.Name() == name {
			return h, true
		}
	}
	return nil, false
}

// installDir is the namespaced subdir pixkb writes into under a host root, so
// installs never clobber the user's own host config.
const installDir = "pixkb"

// Install writes a host's plugin tree atomically. base overrides the host root
// ("" = default). When dryRun is set, no files are written; the planned paths
// are returned instead.
func Install(h Host, base string, dryRun bool) (InstallResult, error) {
	root, err := h.Root(base)
	if err != nil {
		return InstallResult{}, err
	}
	target := filepath.Join(root, installDir)
	files, err := h.Files()
	if err != nil {
		return InstallResult{}, err
	}
	res := InstallResult{Host: h.Name(), Target: target}

	rels := make([]string, 0, len(files))
	for rel := range files {
		rels = append(rels, rel)
	}
	sort.Strings(rels)

	for _, rel := range rels {
		if dryRun {
			res.Planned = append(res.Planned, rel)
			continue
		}
		if err := writeFileAtomic(filepath.Join(target, filepath.FromSlash(rel)), files[rel]); err != nil {
			return res, err
		}
		res.Written++
	}
	return res, nil
}

// writeFileAtomic writes data to path via tmp+rename, creating parents.
func writeFileAtomic(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("mkdir %q: %w", filepath.Dir(path), err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("write %q: %w", tmp, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("rename %q: %w", path, err)
	}
	return nil
}

// --- shared plugin-tree generation -------------------------------------------

// MCPManifest is the .mcp.json registering `pixkb mcp serve` (Codex/Claude
// share this format). bin "" defaults to "pixkb".
func MCPManifest(bin string) []byte {
	if bin == "" {
		bin = "pixkb"
	}
	// Hand-built so the output is stable and dependency-free.
	return []byte(`{
  "mcpServers": {
    "pixkb": {
      "command": "` + jsonEscape(bin) + `",
      "args": ["mcp", "serve"]
    }
  }
}
`)
}

// AgentMarkdown renders one agents.Agent as a host agent-definition file: YAML
// frontmatter (name/description/kind/tools) followed by the system prompt.
func AgentMarkdown(a agents.Agent) []byte {
	var b strings.Builder
	b.WriteString("---\n")
	fmt.Fprintf(&b, "name: %s\n", a.Name)
	fmt.Fprintf(&b, "description: %s\n", yamlScalar(a.Description))
	fmt.Fprintf(&b, "kind: %s\n", a.Kind)
	b.WriteString("tools: [" + strings.Join(a.Tools, ", ") + "]\n")
	b.WriteString("---\n\n")
	b.WriteString(a.System)
	b.WriteString("\n")
	return []byte(b.String())
}

// readme explains how to enable the generated plugin for a host.
func readme(host string) []byte {
	return []byte("# pixkb plugin for " + host + "\n\n" +
		"Generated by `pixkb agents install`. This bundle makes pixkb the agent's\n" +
		"self-contained tool surface.\n\n" +
		"- `.mcp.json` registers `pixkb mcp serve` (search/related/stats/concept_get/\n" +
		"  concept_upsert/reindex). Point your " + host + " config at it.\n" +
		"- `agents/*.md` are the agent definitions (control, gather, scraper,\n" +
		"  normalization, quality, governance, research, judge).\n\n" +
		"The agent reaches the KB ONLY through the pixkb verbs.\n")
}

// sharedFiles builds the plugin tree common to every host.
func sharedFiles(host string) map[string][]byte {
	files := map[string][]byte{
		".mcp.json": MCPManifest(""),
		"README.md": readme(host),
	}
	for _, a := range agents.All() {
		files["agents/"+a.Name+".md"] = AgentMarkdown(a)
	}
	return files
}

// homeRoot resolves base (or the user's home dir) joined with sub.
func homeRoot(base, sub string) (string, error) {
	if base != "" {
		return filepath.Join(base, sub), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home: %w", err)
	}
	return filepath.Join(home, sub), nil
}

func jsonEscape(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	return strings.ReplaceAll(s, `"`, `\"`)
}

func yamlScalar(s string) string {
	if strings.ContainsAny(s, ":#") {
		return `"` + strings.ReplaceAll(s, `"`, `\"`) + `"`
	}
	return s
}
