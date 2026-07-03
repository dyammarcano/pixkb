package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"pixkb/internal/okf"
)

// agentConcept is the JSON shape the agent fleet emits (matches agy's
// conceptSchema). It maps to an okf.Concept for write-back into pixdb.
type agentConcept struct {
	ID        string   `json:"id"`
	Type      string   `json:"type"`
	Title     string   `json:"title"`
	Body      string   `json:"body"`
	Tags        []string `json:"tags"`
	Language    string   `json:"language"`
	SourceURI   string   `json:"source_uri"`
	IntentTerms string   `json:"intent_terms,omitempty"`
}

func (a agentConcept) toConcept() (okf.Concept, error) {
	if strings.TrimSpace(a.ID) == "" || strings.TrimSpace(a.Type) == "" ||
		strings.TrimSpace(a.Title) == "" || strings.TrimSpace(a.Body) == "" {
		return okf.Concept{}, fmt.Errorf("concept %q: id, type, title and body are required", a.ID)
	}
	body := a.Body
	if !strings.HasPrefix(strings.TrimSpace(body), "# ") {
		body = "# " + a.Title + "\n\n" + body
	}
	lang := a.Language
	if lang != "en" {
		lang = "pt"
	}
	return okf.Concept{
		ID:          a.ID,
		Type:        a.Type,
		Title:       a.Title,
		Description: a.Title,
		Tags:        a.Tags,
		Language:    lang,
		SourceURI:   a.SourceURI,
		IntentTerms: a.IntentTerms,
		Body:        body,
		ContentSHA:  okf.ComputeSHA(body),
	}, nil
}

// newConceptCmd is the agent fleet's read/write seam to pixdb (the central
// source): `get` reads a concept from the bundle, `upsert` writes agent-curated
// concepts back into the bundle + index.
func newConceptCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "concept",
		Short: "Read/write KB concepts — the agent fleet's seam to pixdb as central source",
	}
	cmd.AddCommand(newConceptGetCmd(), newConceptUpsertCmd(), newConceptRmCmd())
	return cmd
}

// newConceptRmCmd removes a concept from the canonical bundle + index. Used to
// drop genuine junk that the agents cannot repair — e.g. an OCR'd example-screen
// fragment (a `sample-data` hygiene finding) whose body is noise, not content.
// It deletes the bundle file, rebuilds the bundle indexes, commits the removal,
// then reindexes the DB from the (now-smaller) bundle — the bundle is the source
// of truth, so the dropped concept disappears from search.
func newConceptRmCmd() *cobra.Command {
	var dsn string
	cmd := &cobra.Command{
		Use:   "rm <concept-id>",
		Short: "Remove a concept from the bundle + index (drop unrepairable junk)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := loadConfig()
			if dsn != "" {
				cfg.DSN = dsn
			}
			id := args[0]
			path := filepath.Join(cfg.BundleDir, filepath.FromSlash(id))
			if _, err := os.Stat(path); err != nil {
				return fmt.Errorf("concept %q not found: %w", id, err)
			}
			if err := os.Remove(path); err != nil {
				return fmt.Errorf("remove %q: %w", id, err)
			}
			if err := okf.WriteIndexes(cfg.BundleDir); err != nil {
				return fmt.Errorf("rebuild indexes: %w", err)
			}
			ctx := cmd.Context()
			r, st, err := newRunner(ctx, cfg)
			if err != nil {
				return err
			}
			defer st.Close()
			sha, err := r.Git.Commit(ctx, fmt.Sprintf("remove concept %s", id))
			if err != nil {
				return fmt.Errorf("commit removal: %w", err)
			}
			if err := r.Reindex(ctx); err != nil {
				return fmt.Errorf("reindex after removal: %w", err)
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "removed %s; commit %.7s; reindexed\n", id, sha)
			return nil
		},
	}
	cmd.Flags().StringVar(&dsn, "dsn", "", "Postgres DSN (overrides PIXKB_DSN)")
	return cmd
}

func newConceptGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "get <concept-id>",
		Short: "Print a concept's markdown from the canonical bundle",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := loadConfig()
			path := filepath.Join(cfg.BundleDir, filepath.FromSlash(args[0]))
			b, err := os.ReadFile(path)
			if err != nil {
				return fmt.Errorf("read concept %q: %w", args[0], err)
			}
			_, _ = cmd.OutOrStdout().Write(b)
			return nil
		},
	}
}

func newConceptUpsertCmd() *cobra.Command {
	var dsn, source string
	cmd := &cobra.Command{
		Use:   "upsert",
		Short: "Write agent-curated concepts (JSON on stdin) into the bundle + index",
		Long: "Reads JSON from stdin: either {\"concepts\":[...]}, a bare array, or a single " +
			"concept object. Each concept needs id, type, title, body (tags/language/source_uri " +
			"optional). Writes them as one non-destructive epoch — existing concepts are never removed.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			raw, err := io.ReadAll(cmd.InOrStdin())
			if err != nil {
				return fmt.Errorf("read stdin: %w", err)
			}
			items, err := parseConcepts(raw)
			if err != nil {
				return err
			}
			concepts := make([]okf.Concept, 0, len(items))
			for _, it := range items {
				c, err := it.toConcept()
				if err != nil {
					return err
				}
				concepts = append(concepts, c)
			}
			if len(concepts) == 0 {
				return fmt.Errorf("no concepts on stdin")
			}

			cfg := loadConfig()
			if dsn != "" {
				cfg.DSN = dsn
			}
			ctx := cmd.Context()
			r, st, err := newRunner(ctx, cfg)
			if err != nil {
				return err
			}
			defer st.Close()

			res, err := r.UpsertBatch(ctx, concepts, source)
			if err != nil {
				return err
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "epoch %d: ~%d commit %s\n", res.Epoch, res.Changed, short(res.Commit))
			return nil
		},
	}
	cmd.Flags().StringVar(&dsn, "dsn", "", "Postgres DSN (overrides PIXKB_DSN)")
	cmd.Flags().StringVar(&source, "source", "agent-upsert", "epoch source label")
	return cmd
}

// parseConcepts accepts {"concepts":[...]}, a bare array, or a single object.
func parseConcepts(raw []byte) ([]agentConcept, error) {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" {
		return nil, fmt.Errorf("empty stdin")
	}
	if strings.HasPrefix(trimmed, "{") {
		// Try the {"concepts":[...]} wrapper first.
		var wrap struct {
			Concepts []agentConcept `json:"concepts"`
		}
		if err := json.Unmarshal(raw, &wrap); err == nil && len(wrap.Concepts) > 0 {
			return wrap.Concepts, nil
		}
		// Fall back to a single concept object.
		var one agentConcept
		if err := json.Unmarshal(raw, &one); err != nil {
			return nil, fmt.Errorf("parse concept object: %w", err)
		}
		return []agentConcept{one}, nil
	}
	var arr []agentConcept
	if err := json.Unmarshal(raw, &arr); err != nil {
		return nil, fmt.Errorf("parse concept array: %w", err)
	}
	return arr, nil
}
