// Package postgres holds the derived (rebuildable) search index for pixkb,
// backed by Postgres + pgvector. The canonical store remains the OKF bundle.
package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	pgxvector "github.com/pgvector/pgvector-go/pgx"
)

// Store is the derived Postgres-backed index.
type Store struct {
	pool *pgxpool.Pool
}

// Open creates a pgx connection pool and registers pgvector types on every
// connection via the AfterConnect hook, then verifies connectivity.
func Open(ctx context.Context, dsn string) (*Store, error) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("parse dsn: %w", err)
	}
	cfg.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
		if err := pgxvector.RegisterTypes(ctx, conn); err != nil {
			return fmt.Errorf("register pgvector types: %w", err)
		}
		return nil
	}

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("new pool: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping: %w", err)
	}
	return &Store{pool: pool}, nil
}

// Close releases the underlying connection pool.
func (s *Store) Close() {
	if s.pool != nil {
		s.pool.Close()
	}
}

// GuardDim verifies the configured embedder width matches the dims already
// stored in embedding.dim. Because embedding.vec is an untyped `vector` column
// (serving both 256 and 384), nothing at the schema level stops mixing dims —
// this runtime guard does. It is a no-op on an empty table (first build sets the
// dim) and returns an error if a conflicting dim exists. NOTE: Open does not
// call this; the ingest path must invoke GuardDim before upserting embeddings.
func (s *Store) GuardDim(ctx context.Context, dim int) error {
	var existing int
	err := s.pool.QueryRow(ctx,
		"SELECT dim FROM embedding LIMIT 1").Scan(&existing)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil // empty table: no existing dim to conflict with
	}
	if err != nil {
		return fmt.Errorf("guard dim: query existing dim: %w", err)
	}
	if existing != dim {
		return fmt.Errorf("guard dim: embedding table has dim %d, configured embedder is dim %d (one dim per DB)", existing, dim)
	}
	return nil
}
