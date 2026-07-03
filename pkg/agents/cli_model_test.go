package agents

import (
	"slices"
	"testing"
)

func TestCLIProviderModelFlag(t *testing.T) {
	p := &CLIProvider{ProviderName: "claude", Bin: "claude", BaseArgs: []string{"-p"},
		ModelFlag: "--model", Model: "claude-sonnet-4-6"}

	// Default model is emitted when the agent sets none.
	args, _ := p.argv(RunRequest{Agent: Agent{}}, "", "", "prompt")
	if !hasFlagVal(args, "--model", "claude-sonnet-4-6") {
		t.Fatalf("default model not in argv: %v", args)
	}

	// An Agent.Model hint overrides the provider default.
	args, _ = p.argv(RunRequest{Agent: Agent{Model: "claude-opus-4-8"}}, "", "", "prompt")
	if !hasFlagVal(args, "--model", "claude-opus-4-8") {
		t.Fatalf("agent model override not in argv: %v", args)
	}

	// No ModelFlag => no model flag even if Model set.
	bare := &CLIProvider{ProviderName: "codex", Bin: "codex", BaseArgs: []string{"exec"}, Model: "x"}
	args, _ = bare.argv(RunRequest{Agent: Agent{}}, "", "", "prompt")
	if slices.Contains(args, "--model") {
		t.Fatalf("unexpected model flag for provider without ModelFlag: %v", args)
	}
}

func hasFlagVal(args []string, flag, val string) bool {
	for i := 0; i+1 < len(args); i++ {
		if args[i] == flag && args[i+1] == val {
			return true
		}
	}
	return false
}
