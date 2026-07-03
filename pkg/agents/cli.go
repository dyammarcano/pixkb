package agents

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// CLIProvider drives a headless coding-agent CLI. It is the shared mechanism the
// Codex and Claude Code provider packages build presets on (Antigravity uses its
// own ConPTY Driver instead). A turn is a single exec of the binary with the
// composed prompt; structured output is taken either from an --output file
// (Codex) or parsed from stdout. stdin is always /dev/null so the child never
// drains the parent's input.
type CLIProvider struct {
	ProviderName string   // "codex" | "claude" | ...
	Bin          string   // executable name on PATH
	BaseArgs     []string // fixed leading args (e.g. ["exec","--dangerously-bypass-approvals-and-sandbox"])
	DirFlag      string   // flag to set working dir (e.g. "-C"); "" => use cmd.Dir
	SchemaFlag   string   // flag taking a JSON-Schema file (e.g. "--output-schema"); "" => embed schema in prompt
	OutputFlag   string   // flag taking an output file (e.g. "-o"); "" => read stdout
	ModelFlag    string   // flag selecting the model (e.g. "--model"); "" => no flag emitted
	Model        string   // default model used when an Agent sets none (e.g. "claude-sonnet-4-6")
}

func (p *CLIProvider) Name() string { return p.ProviderName }

// modelFor resolves the model for a turn: the Agent's explicit hint wins,
// otherwise the provider default (p.Model).
func (p *CLIProvider) modelFor(a Agent) string {
	if strings.TrimSpace(a.Model) != "" {
		return a.Model
	}
	return p.Model
}

// Open satisfies SessionOpener with a one-shot session: stateless CLIs (Codex
// exec) spawn one process per turn, so there is no warm process to reuse. The
// pool still gives a uniform interface. Providers with a real persistent mode
// (Antigravity's ConPTY Driver) return a genuinely warm Session instead.
func (p *CLIProvider) Open(_ context.Context, _ Agent) (Session, error) {
	return oneShotSession{p: p}, nil
}

// argv builds the command-line for a request.
func (p *CLIProvider) argv(req RunRequest, schemaPath, outPath, prompt string) (args []string, fromFile bool) {
	args = append(args, p.BaseArgs...)
	if model := p.modelFor(req.Agent); p.ModelFlag != "" && model != "" {
		args = append(args, p.ModelFlag, model)
	}
	if p.DirFlag != "" && req.Dir != "" {
		args = append(args, p.DirFlag, req.Dir)
	}
	if schemaPath != "" && p.SchemaFlag != "" {
		args = append(args, p.SchemaFlag, schemaPath)
		if p.OutputFlag != "" {
			args = append(args, p.OutputFlag, outPath)
			fromFile = true
		}
	}
	if prompt != "" {
		args = append(args, prompt)
	}
	return args, fromFile
}

// maxArgPrompt caps how large a prompt may be before it is fed on stdin instead
// of as a command-line argument. Windows limits a whole command line to ~32 KB
// (CreateProcess), and the schema-embedded prompt for a big concept blows past
// that, failing with "The filename or extension is too long". Both `claude -p`
// and `codex exec` read the prompt from stdin, so large turns route there.
const maxArgPrompt = 8000

// Run executes one agent turn and returns its text (or raw JSON when a schema
// was requested).
func (p *CLIProvider) Run(ctx context.Context, req RunRequest) (RunResult, error) {
	schema := req.EffectiveSchema()
	nativeSchema := schema != "" && p.SchemaFlag != ""
	prompt := req.ComposePrompt(!nativeSchema) // embed schema in prompt only when not native

	var schemaPath, outPath string
	var cleanup []string
	defer func() {
		for _, f := range cleanup {
			_ = os.Remove(f)
		}
	}()
	if nativeSchema {
		sf, err := os.CreateTemp("", "agent-schema-*.json")
		if err != nil {
			return RunResult{}, fmt.Errorf("%s: schema temp: %w", p.ProviderName, err)
		}
		if _, err := sf.WriteString(schema); err != nil {
			_ = sf.Close()
			return RunResult{}, fmt.Errorf("%s: write schema: %w", p.ProviderName, err)
		}
		_ = sf.Close()
		schemaPath = sf.Name()
		cleanup = append(cleanup, schemaPath)
		if p.OutputFlag != "" {
			of, err := os.CreateTemp("", "agent-out-*.json")
			if err != nil {
				return RunResult{}, fmt.Errorf("%s: out temp: %w", p.ProviderName, err)
			}
			_ = of.Close()
			outPath = of.Name()
			cleanup = append(cleanup, outPath)
		}
	}

	// Feed an oversized prompt on stdin instead of as an argv entry to stay under
	// the OS command-line length limit (Windows ~32 KB). The CLIs read stdin.
	useStdin := len(prompt) > maxArgPrompt
	argPrompt := prompt
	if useStdin {
		argPrompt = ""
	}
	args, fromFile := p.argv(req, schemaPath, outPath, argPrompt)
	cmd := exec.CommandContext(ctx, p.Bin, args...)
	if p.DirFlag == "" && req.Dir != "" {
		cmd.Dir = req.Dir
	}
	if useStdin {
		cmd.Stdin = strings.NewReader(prompt)
	} else {
		devnull, err := os.Open(os.DevNull)
		if err != nil {
			return RunResult{}, fmt.Errorf("%s: open devnull: %w", p.ProviderName, err)
		}
		defer func() { _ = devnull.Close() }()
		cmd.Stdin = devnull
	}

	out, err := cmd.Output()
	if err != nil {
		return RunResult{}, fmt.Errorf("%s run: %w", p.ProviderName, err)
	}

	text := strings.TrimSpace(string(out))
	if fromFile {
		b, rerr := os.ReadFile(outPath)
		if rerr != nil {
			return RunResult{}, fmt.Errorf("%s: read output: %w", p.ProviderName, rerr)
		}
		text = strings.TrimSpace(string(b))
	}
	return RunResult{Text: text, Provider: p.ProviderName}, nil
}
