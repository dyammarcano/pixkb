// internal/store/postgres/ispb.go
package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"pixkb/internal/ispb"
)

// UpsertSTR inserts or updates a participant from the STR (canonical)
// registry.
func (s *Store) UpsertSTR(ctx context.Context, r ispb.STRRecord) error {
	if err := r.Validate(); err != nil {
		return err
	}
	const q = `
INSERT INTO ispb_participant
  (ispb_code, institution_name, legal_name, compe_code, participates_compe,
   access_type, operation_start, str_synced_at, updated_at)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8, now())
ON CONFLICT (ispb_code) DO UPDATE SET
  institution_name   = EXCLUDED.institution_name,
  legal_name          = EXCLUDED.legal_name,
  compe_code          = EXCLUDED.compe_code,
  participates_compe  = EXCLUDED.participates_compe,
  access_type         = EXCLUDED.access_type,
  operation_start     = EXCLUDED.operation_start,
  str_synced_at       = EXCLUDED.str_synced_at,
  updated_at          = now()`
	_, err := s.pool.Exec(ctx, q,
		r.ISPB, r.Name, r.LegalName, r.CompeCode, r.ParticipatesCompe,
		r.AccessType, r.OperationStart, r.SyncedAt)
	if err != nil {
		return fmt.Errorf("upsert str participant %s: %w", r.ISPB, err)
	}
	return nil
}

// UpsertPix inserts or updates a participant from the Pix-participants
// registry. institution_name is only overwritten while str_synced_at is
// still at its zero-sentinel — once STR has synced this ISPB, its name
// sticks regardless of later Pix syncs.
func (s *Store) UpsertPix(ctx context.Context, r ispb.PixRecord) error {
	if err := r.Validate(); err != nil {
		return err
	}
	const q = `
INSERT INTO ispb_participant
  (ispb_code, institution_name, cnpj, pix_authorized, pix_synced_at, updated_at)
VALUES ($1,$2,$3,$4,$5, now())
ON CONFLICT (ispb_code) DO UPDATE SET
  institution_name = CASE
    WHEN ispb_participant.str_synced_at = '0001-01-01T00:00:00Z'::timestamptz
      THEN EXCLUDED.institution_name
    ELSE ispb_participant.institution_name
  END,
  cnpj           = EXCLUDED.cnpj,
  pix_authorized = EXCLUDED.pix_authorized,
  pix_synced_at  = EXCLUDED.pix_synced_at,
  updated_at     = now()`
	_, err := s.pool.Exec(ctx, q, r.ISPB, r.Name, r.CNPJ, r.Authorized, r.SyncedAt)
	if err != nil {
		return fmt.Errorf("upsert pix participant %s: %w", r.ISPB, err)
	}
	return nil
}

const ispbSelectCols = `ispb_code, institution_name, legal_name, compe_code, participates_compe,
	access_type, operation_start, str_synced_at, cnpj, pix_authorized, pix_synced_at`

func scanParticipant(row pgx.Row) (ispb.Participant, error) {
	var p ispb.Participant
	err := row.Scan(&p.ISPB, &p.Name, &p.LegalName, &p.CompeCode, &p.ParticipatesCompe,
		&p.AccessType, &p.OperationStart, &p.STRSyncedAt, &p.CNPJ, &p.PixAuthorized, &p.PixSyncedAt)
	return p, err
}

// GetISPB looks up a single participant by its 8-digit ISPB code.
func (s *Store) GetISPB(ctx context.Context, code string) (ispb.Participant, error) {
	if err := ispb.ValidateISPB(code); err != nil {
		return ispb.Participant{}, err
	}
	row := s.pool.QueryRow(ctx, "SELECT "+ispbSelectCols+" FROM ispb_participant WHERE ispb_code = $1", code)
	p, err := scanParticipant(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return ispb.Participant{}, fmt.Errorf("ispb %s: %w", code, pgx.ErrNoRows)
	}
	if err != nil {
		return ispb.Participant{}, fmt.Errorf("get ispb %s: %w", code, err)
	}
	return p, nil
}

// ListISPB returns every participant, ordered by ISPB code.
func (s *Store) ListISPB(ctx context.Context) ([]ispb.Participant, error) {
	rows, err := s.pool.Query(ctx, "SELECT "+ispbSelectCols+" FROM ispb_participant ORDER BY ispb_code")
	if err != nil {
		return nil, fmt.Errorf("list ispb: %w", err)
	}
	defer rows.Close()
	var out []ispb.Participant
	for rows.Next() {
		p, err := scanParticipant(rows)
		if err != nil {
			return nil, fmt.Errorf("scan ispb row: %w", err)
		}
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate ispb rows: %w", err)
	}
	return out, nil
}

// CountISPB returns the number of stored participants.
func (s *Store) CountISPB(ctx context.Context) (int, error) {
	var n int
	if err := s.pool.QueryRow(ctx, "SELECT count(*) FROM ispb_participant").Scan(&n); err != nil {
		return 0, fmt.Errorf("count ispb: %w", err)
	}
	return n, nil
}
