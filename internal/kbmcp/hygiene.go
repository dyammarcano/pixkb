package kbmcp

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"pixkb/internal/hygiene"
	"pixkb/internal/okf"
)

type hygieneIn struct {
	Check    string `json:"check,omitempty" jsonschema:"optional check filter (deviation, junk-title, stub-body, duplicate, broken-link, missing-prov, missing-type)"`
	Severity string `json:"severity,omitempty" jsonschema:"optional severity filter (error|warn)"`
}
type hygieneOut struct {
	Concepts int               `json:"concepts"`
	Errors   int               `json:"errors"`
	Warnings int               `json:"warnings"`
	Findings []hygiene.Finding `json:"findings"`
}

// registerHygieneScan exposes the deterministic hygiene engine as a read-only
// tool: the agents' "eyes" on KB health (the deviation + mechanical findings the
// hygiene/deviation agents act on). No DB or LLM — reads the canonical bundle.
func registerHygieneScan(s *mcp.Server, d Deps) {
	mcp.AddTool(s, &mcp.Tool{
		Name:        "hygiene_scan",
		Description: "Deterministic KB health report: BACEN-charter deviations (implementation-specific content) plus mechanical issues (junk titles, stub bodies, duplicates, broken links, missing provenance). The curate loop's trigger.",
	}, func(_ context.Context, _ *mcp.CallToolRequest, in hygieneIn) (*mcp.CallToolResult, hygieneOut, error) {
		concepts, err := okf.ReadBundle(d.Bundle)
		if err != nil {
			return nil, hygieneOut{}, fmt.Errorf("read bundle: %w", err)
		}
		rep := hygiene.Scan(concepts)
		out := hygieneOut{Concepts: rep.Concepts, Findings: make([]hygiene.Finding, 0, len(rep.Findings))}
		for _, f := range rep.Findings {
			if in.Check != "" && string(f.Check) != in.Check {
				continue
			}
			if in.Severity != "" && string(f.Severity) != in.Severity {
				continue
			}
			if f.Severity == hygiene.SeverityError {
				out.Errors++
			} else {
				out.Warnings++
			}
			out.Findings = append(out.Findings, f)
		}
		return textResult(fmt.Sprintf("%d concepts: %d errors, %d warnings", out.Concepts, out.Errors, out.Warnings)), out, nil
	})
}
