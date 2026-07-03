package postgres

import (
	"context"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

// testDSN returns the integration DSN or skips when unset / under -short.
//
// It REFUSES to run when PIXKB_TEST_DSN points at the same database as the
// production KB (PIXKB_DSN): these tests truncate/drop tables, so running them
// against the live KB silently wipes its index. Point PIXKB_TEST_DSN at a
// throwaway database instead.
func testDSN(t *testing.T) string {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping postgres integration test under -short")
	}
	dsn := os.Getenv("PIXKB_TEST_DSN")
	if dsn == "" {
		t.Skip("PIXKB_TEST_DSN not set; skipping postgres integration test")
	}
	guardNotProdDSN(t, dsn)
	return dsn
}

// guardNotProdDSN fails the test if the integration DSN is the production KB
// DSN. Destructive integration tests must never target the live database.
func guardNotProdDSN(t *testing.T, testDSN string) {
	t.Helper()
	if prod := os.Getenv("PIXKB_DSN"); prod != "" && prod == testDSN {
		t.Fatal("PIXKB_TEST_DSN equals PIXKB_DSN (the production KB) — refusing " +
			"to run destructive integration tests against it. Point PIXKB_TEST_DSN " +
			"at a separate throwaway database.")
	}
}

func TestOpen_PingsAndRegistersVector(t *testing.T) {
	dsn := testDSN(t)
	ctx := context.Background()

	s, err := Open(ctx, dsn)
	require.NoError(t, err)
	require.NotNil(t, s)
	defer s.Close()

	// A round-trip through a pgvector value proves RegisterTypes ran on the pool.
	var got string
	err = s.pool.QueryRow(ctx, "SELECT '[1,2,3]'::vector::text").Scan(&got)
	require.NoError(t, err)
	require.Equal(t, "[1,2,3]", got)
}

func TestOpen_BadDSN(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping postgres integration test under -short")
	}
	_, err := Open(context.Background(), "postgres://nope:nope@127.0.0.1:1/none?connect_timeout=1")
	require.Error(t, err)
}

func TestGuardDim_EmptyTable(t *testing.T) {
	dsn := testDSN(t)
	ctx := context.Background()

	s, err := Open(ctx, dsn)
	require.NoError(t, err)
	defer s.Close()

	// GuardDim against empty embedding table should return nil (no conflict).
	err = s.GuardDim(ctx, 384)
	require.NoError(t, err)
}
