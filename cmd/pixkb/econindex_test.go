package main

import (
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewEconIndexCmd_Wiring(t *testing.T) {
	t.Parallel()
	root := newEconIndexCmd()
	assert.Equal(t, "econindex", root.Name())

	names := map[string]bool{}
	for _, c := range root.Commands() {
		names[c.Name()] = true
	}
	for _, want := range []string{"fetch", "load", "sync"} {
		assert.True(t, names[want], "missing subcommand %q", want)
	}
}

// TestNewEconIndexCmd_NoDSNFlag guards the project rule that the DSN must
// come from config/env only — no econindex subcommand may expose --dsn.
func TestNewEconIndexCmd_NoDSNFlag(t *testing.T) {
	t.Parallel()
	var walk func(cmd *cobra.Command)
	walk = func(cmd *cobra.Command) {
		assert.Nilf(t, cmd.Flags().Lookup("dsn"), "%s must not have a --dsn flag", cmd.CommandPath())
		for _, c := range cmd.Commands() {
			walk(c)
		}
	}
	walk(newEconIndexCmd())
}

func TestEconIndexFetch_UnknownSeries(t *testing.T) {
	t.Parallel()
	cmd := newEconIndexCmd()
	cmd.SetArgs([]string{"fetch", "--series", "not-a-series"})
	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown econindex series")
}

func TestEconIndexFetch_MissingSeries(t *testing.T) {
	t.Parallel()
	cmd := newEconIndexCmd()
	cmd.SetArgs([]string{"fetch"})
	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--series is required")
}

// TestEconIndexSync_NoDSN documents that sync opens the store (to fail fast
// on a missing DSN) before validating --series/--all, matching the ordering
// established by ispb_test.go's TestISPBLookup_NoDSN.
func TestEconIndexSync_NoDSN(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("PIXKB_DSN", "")
	root := NewRootCmd()
	root.SetArgs([]string{"econindex", "sync"})
	err := root.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no database DSN")
}
