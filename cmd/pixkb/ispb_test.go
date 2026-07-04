package main

import (
	"bytes"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewISPBCmd_Wiring(t *testing.T) {
	t.Parallel()
	root := newISPBCmd()
	assert.Equal(t, "ispb", root.Use)

	names := map[string]bool{}
	for _, c := range root.Commands() {
		names[c.Name()] = true
	}
	for _, want := range []string{"str", "pix", "sync", "lookup"} {
		assert.True(t, names[want], "missing subcommand %q", want)
	}

	str, _, err := root.Find([]string{"str"})
	require.NoError(t, err)
	strNames := map[string]bool{}
	for _, c := range str.Commands() {
		strNames[c.Name()] = true
	}
	for _, want := range []string{"fetch", "load", "sync"} {
		assert.True(t, strNames[want], "missing str subcommand %q", want)
	}

	pix, _, err := root.Find([]string{"pix"})
	require.NoError(t, err)
	pixNames := map[string]bool{}
	for _, c := range pix.Commands() {
		pixNames[c.Name()] = true
	}
	for _, want := range []string{"fetch", "load", "sync"} {
		assert.True(t, pixNames[want], "missing pix subcommand %q", want)
	}
}

// TestNewISPBCmd_NoDSNFlag guards the project rule that the DSN must come
// from config/env only — no ispb subcommand may expose a --dsn flag.
func TestNewISPBCmd_NoDSNFlag(t *testing.T) {
	t.Parallel()
	var walk func(cmd *cobra.Command)
	walk = func(cmd *cobra.Command) {
		assert.Nilf(t, cmd.Flags().Lookup("dsn"), "%s must not have a --dsn flag", cmd.CommandPath())
		for _, c := range cmd.Commands() {
			walk(c)
		}
	}
	walk(newISPBCmd())
}

func TestISPBLookup_InvalidCode(t *testing.T) {
	t.Parallel()
	root := NewRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetArgs([]string{"ispb", "lookup", "bad"})
	err := root.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid ISPB code")
}

func TestISPBLookup_NoDSN(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("PIXKB_CONFIG_DIR", t.TempDir()) // isolate from any real global config
	t.Setenv("PIXKB_DSN", "")
	root := NewRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetArgs([]string{"ispb", "lookup", "00000208"})
	err := root.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no database DSN")
}
