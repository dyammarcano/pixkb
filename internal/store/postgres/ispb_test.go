// internal/store/postgres/ispb_test.go
package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"pixkb/internal/ispb"
)

func truncateISPB(t *testing.T, s *Store) {
	t.Helper()
	_, err := s.pool.Exec(context.Background(), "TRUNCATE ispb_participant")
	require.NoError(t, err)
}

func TestUpsertSTR_ThenGet(t *testing.T) {
	dsn := testDSN(t)
	ctx := context.Background()
	applyTestSchema(t, dsn)
	s, err := Open(ctx, dsn)
	require.NoError(t, err)
	defer s.Close()
	truncateISPB(t, s)

	synced := time.Date(2026, 7, 3, 0, 0, 0, 0, time.UTC)
	rec := ispb.STRRecord{
		ISPB: "00000208", Name: "BRB - BCO DE BRASILIA S.A.", LegalName: "BRB - BANCO DE BRASILIA S.A.",
		CompeCode: "070", ParticipatesCompe: true, AccessType: "RSFN",
		OperationStart: time.Date(2002, 4, 22, 0, 0, 0, 0, time.UTC), SyncedAt: synced,
	}
	require.NoError(t, s.UpsertSTR(ctx, rec))

	got, err := s.GetISPB(ctx, "00000208")
	require.NoError(t, err)
	assert.Equal(t, rec.Name, got.Name)
	assert.Equal(t, rec.LegalName, got.LegalName)
	assert.Equal(t, rec.CompeCode, got.CompeCode)
	assert.True(t, got.ParticipatesCompe)
	assert.Equal(t, rec.AccessType, got.AccessType)
	assert.True(t, got.STRSyncedAt.Equal(synced))
	assert.True(t, got.PixSyncedAt.IsZero(), "never Pix-synced")
}

func TestUpsertPix_DoesNotOverwriteSTRName(t *testing.T) {
	dsn := testDSN(t)
	ctx := context.Background()
	applyTestSchema(t, dsn)
	s, err := Open(ctx, dsn)
	require.NoError(t, err)
	defer s.Close()
	truncateISPB(t, s)

	require.NoError(t, s.UpsertSTR(ctx, ispb.STRRecord{
		ISPB: "00000208", Name: "STR CANONICAL NAME", SyncedAt: time.Now(),
	}))
	require.NoError(t, s.UpsertPix(ctx, ispb.PixRecord{
		ISPB: "00000208", Name: "PIX NAME VARIANT", CNPJ: "00000208000100", Authorized: true, SyncedAt: time.Now(),
	}))

	got, err := s.GetISPB(ctx, "00000208")
	require.NoError(t, err)
	assert.Equal(t, "STR CANONICAL NAME", got.Name, "STR name must survive a later Pix upsert")
	assert.Equal(t, "00000208000100", got.CNPJ)
	assert.True(t, got.PixAuthorized)
	assert.False(t, got.PixSyncedAt.IsZero())
}

func TestUpsertPix_NameUsedUntilSTRSyncs(t *testing.T) {
	dsn := testDSN(t)
	ctx := context.Background()
	applyTestSchema(t, dsn)
	s, err := Open(ctx, dsn)
	require.NoError(t, err)
	defer s.Close()
	truncateISPB(t, s)

	require.NoError(t, s.UpsertPix(ctx, ispb.PixRecord{ISPB: "00000208", Name: "PIX ONLY NAME", SyncedAt: time.Now()}))
	got, err := s.GetISPB(ctx, "00000208")
	require.NoError(t, err)
	assert.Equal(t, "PIX ONLY NAME", got.Name, "Pix name is authoritative before any STR sync")
	assert.True(t, got.STRSyncedAt.IsZero())
}

func TestUpsertPix_SecondSyncUpdatesNameBeforeSTR(t *testing.T) {
	dsn := testDSN(t)
	ctx := context.Background()
	applyTestSchema(t, dsn)
	s, err := Open(ctx, dsn)
	require.NoError(t, err)
	defer s.Close()
	truncateISPB(t, s)

	require.NoError(t, s.UpsertPix(ctx, ispb.PixRecord{ISPB: "00000208", Name: "FIRST PIX NAME", SyncedAt: time.Now()}))
	require.NoError(t, s.UpsertPix(ctx, ispb.PixRecord{ISPB: "00000208", Name: "SECOND PIX NAME", SyncedAt: time.Now()}))

	got, err := s.GetISPB(ctx, "00000208")
	require.NoError(t, err)
	assert.Equal(t, "SECOND PIX NAME", got.Name, "WHEN branch: name must update on repeat Pix sync before any STR sync")
	assert.True(t, got.STRSyncedAt.IsZero())
}

func TestGetISPB_NotFound(t *testing.T) {
	dsn := testDSN(t)
	ctx := context.Background()
	applyTestSchema(t, dsn)
	s, err := Open(ctx, dsn)
	require.NoError(t, err)
	defer s.Close()
	truncateISPB(t, s)

	_, err = s.GetISPB(ctx, "99999999")
	require.Error(t, err)
	assert.True(t, errors.Is(err, pgx.ErrNoRows))
}

func TestListISPB_And_CountISPB(t *testing.T) {
	dsn := testDSN(t)
	ctx := context.Background()
	applyTestSchema(t, dsn)
	s, err := Open(ctx, dsn)
	require.NoError(t, err)
	defer s.Close()
	truncateISPB(t, s)

	require.NoError(t, s.UpsertSTR(ctx, ispb.STRRecord{ISPB: "00000208", Name: "B", SyncedAt: time.Now()}))
	require.NoError(t, s.UpsertSTR(ctx, ispb.STRRecord{ISPB: "00000001", Name: "A", SyncedAt: time.Now()}))

	n, err := s.CountISPB(ctx)
	require.NoError(t, err)
	assert.Equal(t, 2, n)

	list, err := s.ListISPB(ctx)
	require.NoError(t, err)
	require.Len(t, list, 2)
	assert.Equal(t, "00000001", list[0].ISPB, "ordered by ispb_code")
}

func TestSearchISPB_MatchesNameSubstringCaseInsensitive(t *testing.T) {
	dsn := testDSN(t)
	ctx := context.Background()
	applyTestSchema(t, dsn)
	s, err := Open(ctx, dsn)
	require.NoError(t, err)
	defer s.Close()
	truncateISPB(t, s)

	require.NoError(t, s.UpsertSTR(ctx, ispb.STRRecord{ISPB: "60701190", Name: "ITAÚ UNIBANCO S.A.", SyncedAt: time.Now()}))
	require.NoError(t, s.UpsertSTR(ctx, ispb.STRRecord{ISPB: "00000000", Name: "BCO DO BRASIL S.A.", SyncedAt: time.Now()}))

	matches, err := s.SearchISPB(ctx, "itaú")
	require.NoError(t, err)
	require.Len(t, matches, 1)
	assert.Equal(t, "60701190", matches[0].ISPB)

	none, err := s.SearchISPB(ctx, "nonexistent bank name")
	require.NoError(t, err)
	assert.Empty(t, none)
}
