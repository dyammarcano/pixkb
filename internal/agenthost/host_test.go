package agenthost_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"pixkb/internal/agenthost"
	_ "pixkb/internal/roster" // populate corral's registry (judge, control, ...)
)

func TestRegistry(t *testing.T) {
	hosts := agenthost.All()
	if len(hosts) != 3 {
		t.Fatalf("want 3 hosts, got %d", len(hosts))
	}
	for _, name := range []string{"claude", "codex", "agy"} {
		if _, ok := agenthost.ByName(name); !ok {
			t.Errorf("host %q not registered", name)
		}
	}
}

func TestMCPManifest(t *testing.T) {
	m := string(agenthost.MCPManifest("C:/bin/pixkb.exe"))
	for _, want := range []string{`"pixkb"`, `"mcp", "serve"`, `C:/bin/pixkb.exe`} {
		if !strings.Contains(m, want) {
			t.Errorf("manifest missing %q:\n%s", want, m)
		}
	}
}

func TestSharedFilesHaveAgentsAndManifest(t *testing.T) {
	h, _ := agenthost.ByName("codex")
	files, err := h.Files()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := files[".mcp.json"]; !ok {
		t.Error("missing .mcp.json")
	}
	if _, ok := files["agents/judge.md"]; !ok {
		t.Error("missing agents/judge.md")
	}
	// Agent md carries frontmatter + the pixkb contract.
	jb := string(files["agents/judge.md"])
	if !strings.HasPrefix(jb, "---\nname: judge\n") {
		t.Errorf("judge.md frontmatter wrong:\n%s", jb[:min(80, len(jb))])
	}
	if !strings.Contains(jb, "pixkb operating contract") {
		t.Error("judge.md missing pixkb contract")
	}
}

func TestInstallWritesTree(t *testing.T) {
	base := t.TempDir()
	h, _ := agenthost.ByName("claude")

	// Dry-run writes nothing but plans files.
	dr, err := agenthost.Install(h, base, true)
	if err != nil {
		t.Fatal(err)
	}
	if dr.Written != 0 || len(dr.Planned) == 0 {
		t.Fatalf("dry-run wrote=%d planned=%d", dr.Written, len(dr.Planned))
	}
	if _, err := os.Stat(filepath.Join(base, ".claude", "pixkb")); !os.IsNotExist(err) {
		t.Error("dry-run created files")
	}

	// Real install writes the tree under base/.claude/pixkb — confirms
	// installDir is still "pixkb", not corral's own "corral".
	res, err := agenthost.Install(h, base, false)
	if err != nil {
		t.Fatal(err)
	}
	if res.Written == 0 {
		t.Fatal("install wrote nothing")
	}
	for _, rel := range []string{".mcp.json", "README.md", "agents/control.md", "agents/judge.md"} {
		p := filepath.Join(base, ".claude", "pixkb", filepath.FromSlash(rel))
		if _, err := os.Stat(p); err != nil {
			t.Errorf("expected %s: %v", rel, err)
		}
	}
}

func TestDoctor(t *testing.T) {
	base := t.TempDir()
	// Doctor is exercised through the registered host, not the unexported
	// claudeHost type directly (that type is package-private to agenthost).
	h, _ := agenthost.ByName("claude")
	r := h.Doctor(base)
	if r.Host != "claude" || r.Verdict == "" || len(r.Checks) == 0 {
		t.Fatalf("bad report: %+v", r)
	}
}
