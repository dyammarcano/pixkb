package main

import (
	"context"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDBCmdWiring(t *testing.T) {
	t.Parallel()

	root := NewRootCmd()
	db, _, err := root.Find([]string{"db"})
	require.NoError(t, err)
	require.Equal(t, "db", db.Name())

	names := map[string]bool{}
	for _, c := range db.Commands() {
		names[c.Name()] = true
	}
	assert.True(t, names["up"], "db missing up subcommand")
	assert.True(t, names["down"], "db missing down subcommand")
}

func TestResolveDSN(t *testing.T) {
	tests := []struct {
		name      string
		flagVal   string
		envVal    string
		wantDSN   string
		wantError bool
	}{
		{
			name:    "flag set, env unset",
			flagVal: "postgres://user:pass@localhost/db",
			envVal:  "",
			wantDSN: "postgres://user:pass@localhost/db",
		},
		{
			name:    "flag empty, env set",
			flagVal: "",
			envVal:  "postgres://envuser:envpass@localhost/envdb",
			wantDSN: "postgres://envuser:envpass@localhost/envdb",
		},
		{
			name:      "flag empty, env empty",
			flagVal:   "",
			envVal:    "",
			wantError: true,
		},
		{
			name:    "flag takes precedence over env",
			flagVal: "postgres://flaguser:flagpass@localhost/flagdb",
			envVal:  "postgres://envuser:envpass@localhost/envdb",
			wantDSN: "postgres://flaguser:flagpass@localhost/flagdb",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("PIXKB_DSN", tt.envVal)
			dsn, err := resolveDSN(tt.flagVal)
			if tt.wantError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.wantDSN, dsn)
			}
		})
	}
}

func TestDBUpRun(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PIXKB_TEST_DSN postgres")
	}
	dsn := os.Getenv("PIXKB_TEST_DSN")
	if dsn == "" {
		t.Skip("PIXKB_TEST_DSN not set")
	}
	// This test drops the schema; never run it against the live KB.
	if prod := os.Getenv("PIXKB_DSN"); prod != "" && prod == dsn {
		t.Fatal("PIXKB_TEST_DSN equals PIXKB_DSN (prod KB) — use a throwaway database")
	}

	root := NewRootCmd()
	root.SetArgs([]string{"db", "up", "--dsn", dsn})
	require.NoError(t, root.ExecuteContext(context.Background()))

	root2 := NewRootCmd()
	root2.SetArgs([]string{"db", "down", "--dsn", dsn})
	require.NoError(t, root2.ExecuteContext(context.Background()))

	// Re-provision the schema. PIXKB_TEST_DSN is a SHARED database: other
	// integration packages (internal/epoch, internal/query) assume the schema
	// exists and do not self-apply it. Leaving it dropped after this down
	// round-trip drops the rug from under every package that runs after
	// cmd/pixkb, so restore it before returning.
	root3 := NewRootCmd()
	root3.SetArgs([]string{"db", "up", "--dsn", dsn})
	require.NoError(t, root3.ExecuteContext(context.Background()))
}
