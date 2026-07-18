package postgres

import (
	"context"
	"fmt"
	"time"

	"pixkb/internal/okf"
)

// RecordFact appends a new bitemporal row to concept_fact for c.ID. It first
// closes the prior open window for that id — capping its transaction range at
// the database clock and its valid range at validFrom (so prior versions get
// true valid-time history, not an open-ended [from, ∞)) — then inserts the new
// row with valid = [validFrom, ∞) and tx = [clock_timestamp(), ∞).
//
// Both tx bounds use clock_timestamp() (the live wall clock, which advances
// between statements) rather than a caller-supplied timestamp. This guarantees
// the closed prior tx range is non-empty and cannot overlap the new one even
// when two facts for the same id are recorded inside a single epoch — avoiding
// both the empty-range row-drop and the gist EXCLUDE (id WITH =, tx WITH &&)
// violation that a shared epoch timestamp would cause. txFrom is retained for
// API compatibility; valid-time is driven by validFrom, tx-time by the DB clock.
func (s *Store) RecordFact(ctx context.Context, c okf.Concept, validFrom, txFrom time.Time) error {
	_ = txFrom // tx-time is DB-clock driven; see doc comment.
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin fact tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// Close the prior open row: cap its tx at the DB clock and its valid range at
	// the new version's validFrom. clock_timestamp() advances per statement, so
	// the closed tx range stays non-empty and cannot overlap the new one.
	if _, err := tx.Exec(ctx,
		`UPDATE concept_fact
		    SET tx    = tstzrange(lower(tx), clock_timestamp()),
		        valid = tstzrange(lower(valid), $2)
		  WHERE id = $1 AND `+currentTxPred("tx"), c.ID, validFrom); err != nil {
		return fmt.Errorf("close prior fact window %q: %w", c.ID, err)
	}

	if _, err := tx.Exec(ctx,
		`INSERT INTO concept_fact (id, type, title, content_sha, epoch, valid, tx)
		 VALUES ($1, $2, $3, $4, $5,
		         tstzrange($6, NULL), tstzrange(clock_timestamp(), NULL))`,
		c.ID, c.Type, c.Title, c.ContentSHA, c.Epoch, validFrom); err != nil {
		return fmt.Errorf("insert fact %q: %w", c.ID, err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit fact tx: %w", err)
	}
	return nil
}
