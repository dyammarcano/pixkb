// Package output renders CLI result sets — search hits, graph neighbours, KB
// stats, and ISPB participants — into one of several output formats
// (text/json/md/yaml). It is purely a rendering layer: it never touches
// ranking, filtering, or lookup logic.
//
// Every Render* function accepts the same format vocabulary: "" (alias for
// "text"), "text", "json", "md", or "yaml", and reports an unknown format as
// an error. Each "text" renderer reproduces, byte for byte, the plain-text
// output its command printed before gaining a --format flag.
package output

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"pixkb/internal/ispb"
	"pixkb/internal/store/postgres"
)

// errUnknownFormat reports format as unsupported. Every Render* function
// funnels its default case through here so the CLI surfaces one consistent
// message whatever the result shape.
func errUnknownFormat(format string) error {
	return fmt.Errorf("output: unknown format %q (want text|json|md|yaml)", format)
}

// marshalJSON renders v as indented JSON.
func marshalJSON(v any) (string, error) {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return "", fmt.Errorf("output: marshal json: %w", err)
	}
	return string(b), nil
}

// marshalYAML renders v as YAML.
func marshalYAML(v any) (string, error) {
	b, err := yaml.Marshal(v)
	if err != nil {
		return "", fmt.Errorf("output: marshal yaml: %w", err)
	}
	return string(b), nil
}

// Render formats hits according to format, one of "" (alias for "text"),
// "text", "json", "md", or "yaml". Unknown formats return an error.
func Render(format string, hits []postgres.Hit) (string, error) {
	switch format {
	case "", "text":
		return renderText(hits), nil
	case "json":
		return marshalJSON(hits)
	case "md":
		return renderMD(hits), nil
	case "yaml":
		return marshalYAML(hits)
	default:
		return "", errUnknownFormat(format)
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

func renderMD(hits []postgres.Hit) string {
	var sb strings.Builder
	sb.WriteString("| rank | id | title | type | score |\n")
	sb.WriteString("| --- | --- | --- | --- | --- |\n")
	for _, h := range hits {
		fmt.Fprintf(&sb, "| %d | %s | %s | %s | %g |\n", h.Rank, h.ID, h.Title, h.Type, h.Score)
	}
	return sb.String()
}

// RenderRelated formats a concept's graph neighbours according to format.
// Unknown formats return an error.
func RenderRelated(format string, rels []postgres.RelatedConcept) (string, error) {
	switch format {
	case "", "text":
		return renderRelatedText(rels), nil
	case "json":
		return marshalJSON(rels)
	case "md":
		return renderRelatedMD(rels), nil
	case "yaml":
		return marshalYAML(rels)
	default:
		return "", errUnknownFormat(format)
	}
}

// renderRelatedText reproduces pixkb related's historical plain-text format: a
// 3-char left-aligned direction, a 34-char left-padded id, and the title.
func renderRelatedText(rels []postgres.RelatedConcept) string {
	var sb strings.Builder
	for _, r := range rels {
		fmt.Fprintf(&sb, "%-3s %-34s  %s\n", r.Direction, r.ID, r.Title)
	}
	return sb.String()
}

func renderRelatedMD(rels []postgres.RelatedConcept) string {
	var sb strings.Builder
	sb.WriteString("| direction | id | title | type |\n")
	sb.WriteString("| --- | --- | --- | --- |\n")
	for _, r := range rels {
		fmt.Fprintf(&sb, "| %s | %s | %s | %s |\n", r.Direction, r.ID, r.Title, r.Type)
	}
	return sb.String()
}

// RenderStats formats KB stats according to format. Unknown formats return an
// error.
func RenderStats(format string, s postgres.Stats) (string, error) {
	switch format {
	case "", "text":
		return renderStatsText(s), nil
	case "json":
		return marshalJSON(s)
	case "md":
		return renderStatsMD(s), nil
	case "yaml":
		return marshalYAML(s)
	default:
		return "", errUnknownFormat(format)
	}
}

// renderStatsText reproduces pixkb stats's historical plain-text format. The
// per-type breakdown is omitted entirely when no types are present, matching
// the original command.
func renderStatsText(s postgres.Stats) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "concepts:    %d\n", s.Concepts)
	fmt.Fprintf(&sb, "embeddings:  %d\n", s.Embeddings)
	fmt.Fprintf(&sb, "epochs:      %d (latest: %d)\n", s.Epochs, s.LatestEpoch)
	if len(s.TypeOrder) > 0 {
		sb.WriteString("by type:\n")
		for _, typ := range s.TypeOrder {
			fmt.Fprintf(&sb, "  %-16s %d\n", typ, s.ByType[typ])
		}
	}
	return sb.String()
}

// renderStatsMD renders stats as a metric/value table. TypeOrder drives the
// per-type rows so their order matches the text renderer's (count descending)
// rather than Go's randomized map iteration.
func renderStatsMD(s postgres.Stats) string {
	var sb strings.Builder
	sb.WriteString("| metric | value |\n")
	sb.WriteString("| --- | --- |\n")
	fmt.Fprintf(&sb, "| concepts | %d |\n", s.Concepts)
	fmt.Fprintf(&sb, "| embeddings | %d |\n", s.Embeddings)
	fmt.Fprintf(&sb, "| epochs | %d |\n", s.Epochs)
	fmt.Fprintf(&sb, "| latest epoch | %d |\n", s.LatestEpoch)
	for _, typ := range s.TypeOrder {
		fmt.Fprintf(&sb, "| type: %s | %d |\n", typ, s.ByType[typ])
	}
	return sb.String()
}

// RenderISPB formats an ISPB participant according to format. Unknown formats
// return an error.
func RenderISPB(format string, p ispb.Participant) (string, error) {
	switch format {
	case "", "text":
		return renderISPBText(p), nil
	case "json":
		return marshalJSON(p)
	case "md":
		return renderISPBMD(p), nil
	case "yaml":
		return marshalYAML(p)
	default:
		return "", errUnknownFormat(format)
	}
}

// renderISPBText reproduces pixkb ispb lookup's historical plain-text format.
// Optional fields are omitted when unset, and the STR and Pix blocks each
// appear only once their respective sync timestamp is populated — a
// participant known only from the ISPB registry prints just ISPB and Name.
func renderISPBText(p ispb.Participant) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "ISPB:          %s\n", p.ISPB)
	fmt.Fprintf(&sb, "Name:          %s\n", p.Name)
	if p.LegalName != "" {
		fmt.Fprintf(&sb, "Legal name:    %s\n", p.LegalName)
	}
	if p.CompeCode != "" {
		fmt.Fprintf(&sb, "COMPE code:    %s\n", p.CompeCode)
	}
	if !p.STRSyncedAt.IsZero() {
		fmt.Fprintf(&sb, "Participates COMPE: %t\n", p.ParticipatesCompe)
		fmt.Fprintf(&sb, "Access type:   %s\n", p.AccessType)
		fmt.Fprintf(&sb, "STR synced:    %s\n", p.STRSyncedAt.Format(time.RFC3339))
	}
	if !p.PixSyncedAt.IsZero() {
		fmt.Fprintf(&sb, "CNPJ:          %s\n", p.CNPJ)
		fmt.Fprintf(&sb, "Pix authorized: %t\n", p.PixAuthorized)
		fmt.Fprintf(&sb, "Pix synced:    %s\n", p.PixSyncedAt.Format(time.RFC3339))
	}
	return sb.String()
}

// renderISPBMD renders a participant as a field/value table, mirroring the
// text renderer's conditional blocks so the two agree on which fields a
// partially-synced participant has.
func renderISPBMD(p ispb.Participant) string {
	var sb strings.Builder
	sb.WriteString("| field | value |\n")
	sb.WriteString("| --- | --- |\n")
	fmt.Fprintf(&sb, "| ISPB | %s |\n", p.ISPB)
	fmt.Fprintf(&sb, "| Name | %s |\n", p.Name)
	if p.LegalName != "" {
		fmt.Fprintf(&sb, "| Legal name | %s |\n", p.LegalName)
	}
	if p.CompeCode != "" {
		fmt.Fprintf(&sb, "| COMPE code | %s |\n", p.CompeCode)
	}
	if !p.STRSyncedAt.IsZero() {
		fmt.Fprintf(&sb, "| Participates COMPE | %t |\n", p.ParticipatesCompe)
		fmt.Fprintf(&sb, "| Access type | %s |\n", p.AccessType)
		fmt.Fprintf(&sb, "| STR synced | %s |\n", p.STRSyncedAt.Format(time.RFC3339))
	}
	if !p.PixSyncedAt.IsZero() {
		fmt.Fprintf(&sb, "| CNPJ | %s |\n", p.CNPJ)
		fmt.Fprintf(&sb, "| Pix authorized | %t |\n", p.PixAuthorized)
		fmt.Fprintf(&sb, "| Pix synced | %s |\n", p.PixSyncedAt.Format(time.RFC3339))
	}
	return sb.String()
}
