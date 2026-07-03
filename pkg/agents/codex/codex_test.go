package codex

import (
	"slices"
	"testing"

	"pixkb/pkg/agents"
)

func TestCodexPreset(t *testing.T) {
	p := New()
	if p.Name() != "codex" || p.Bin != "codex" {
		t.Fatalf("preset wrong: name=%q bin=%q", p.Name(), p.Bin)
	}
	if p.SchemaFlag != "--output-schema" || p.OutputFlag != "-o" || p.DirFlag != "-C" {
		t.Errorf("codex flags wrong: %+v", p)
	}
	if len(p.BaseArgs) == 0 || p.BaseArgs[0] != "exec" {
		t.Errorf("base args wrong: %v", p.BaseArgs)
	}
}

func TestCodexRegistered(t *testing.T) {
	p, err := agents.ProviderByName("codex")
	if err != nil {
		t.Fatalf("codex not registered: %v", err)
	}
	if p.Name() != "codex" {
		t.Errorf("provider name = %q, want codex", p.Name())
	}
	// Default provider resolves to codex.
	if _, err := agents.ProviderByName(""); err != nil {
		t.Errorf("default provider: %v", err)
	}
	if !slices.Contains(agents.RegisteredProviders(), "codex") {
		t.Error("codex missing from RegisteredProviders")
	}
}
