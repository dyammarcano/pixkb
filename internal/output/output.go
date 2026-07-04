// Package output renders search hits ([]postgres.Hit) into one of several
// output formats (text/json/md/yaml) for CLI presentation. It is purely a
// rendering layer: it never touches ranking or filtering logic.
package output

import (
	"encoding/json"
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"

	"pixkb/internal/store/postgres"
)

// Render formats hits according to format, one of "" (alias for "text"),
// "text", "json", "md", or "yaml". Unknown formats return an error.
func Render(format string, hits []postgres.Hit) (string, error) {
	switch format {
	case "", "text":
		return renderText(hits), nil
	case "json":
		return renderJSON(hits)
	case "md":
		return renderMD(hits), nil
	case "yaml":
		return renderYAML(hits)
	default:
		return "", fmt.Errorf("output: unknown format %q (want text|json|md|yaml)", format)
	}
}

// renderText reproduces pixkb search's historical plain-text format: a
// 2-digit right-aligned rank, a 34-char left-padded id, and the title.
func renderText(hits []postgres.Hit) string {
	var sb strings.Builder
	for _, h := range hits {
		fmt.Fprintf(&sb, "%2d  %-34s  %s\n", h.Rank, h.ID, h.Title)
	}
	return sb.String()
}

func renderJSON(hits []postgres.Hit) (string, error) {
	b, err := json.MarshalIndent(hits, "", "  ")
	if err != nil {
		return "", fmt.Errorf("output: marshal json: %w", err)
	}
	return string(b), nil
}

func renderMD(hits []postgres.Hit) string {
	var sb strings.Builder
	sb.WriteString("| rank | id | title | type | score |\n")
	sb.WriteString("| --- | --- | --- | --- | --- |\n")
	for _, h := range hits {
		fmt.Fprintf(&sb, "| %d | %s | %s | %s | %g |\n", h.Rank, h.ID, h.Title, h.Type, h.Score)
	}
	return sb.String()
}

func renderYAML(hits []postgres.Hit) (string, error) {
	b, err := yaml.Marshal(hits)
	if err != nil {
		return "", fmt.Errorf("output: marshal yaml: %w", err)
	}
	return string(b), nil
}
