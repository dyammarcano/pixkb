package postgres

import (
	"context"
	"fmt"

	"pixkb/internal/okf"
)

// UpsertConcept inserts a concept or updates it in place keyed by id.
// On conflict, first_epoch is preserved (LEAST) and last_epoch advances
// (GREATEST) so the row tracks its full epoch lifespan. tags map to text[].
// The fts tsvector column is generated and must not be written directly.
func (s *Store) UpsertConcept(ctx context.Context, c okf.Concept) error {
	const q = `
INSERT INTO concept
  (id, type, title, description, resource, tags, language, body,
   content_sha, source_uri, first_epoch, last_epoch, updated_at, intent_terms)
VALUES
  ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
ON CONFLICT (id) DO UPDATE SET
  type         = EXCLUDED.type,
  title        = EXCLUDED.title,
  description  = EXCLUDED.description,
  resource     = EXCLUDED.resource,
  tags         = EXCLUDED.tags,
  language     = EXCLUDED.language,
  body         = EXCLUDED.body,
  content_sha  = EXCLUDED.content_sha,
  source_uri   = EXCLUDED.source_uri,
  intent_terms = EXCLUDED.intent_terms,
  first_epoch  = LEAST(concept.first_epoch, EXCLUDED.first_epoch),
  last_epoch   = GREATEST(concept.last_epoch, EXCLUDED.last_epoch),
  updated_at   = EXCLUDED.updated_at`

	tags := c.Tags
	if tags == nil {
		tags = []string{}
	}
	if _, err := s.pool.Exec(ctx, q,
		c.ID, c.Type, c.Title, c.Description, c.Resource, tags, c.Language,
		c.Body, c.ContentSHA, c.SourceURI, c.Epoch, c.Epoch, c.Timestamp, c.IntentTerms,
	); err != nil {
		return fmt.Errorf("upsert concept %q: %w", c.ID, err)
	}
	return nil
}
