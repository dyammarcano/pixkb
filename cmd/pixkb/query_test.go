package main

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestQuery_ParseError verifies that a malformed HQL expression fails before
// any store is opened — hql.Parse runs first, so this needs no DB.
func TestQuery_ParseError(t *testing.T) {
	t.Parallel()
	root := NewRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetArgs([]string{"query", "type = = ="})
	err := root.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parse query")
}

func TestQuery_NoDSN(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("PIXKB_CONFIG_DIR", t.TempDir())
	t.Setenv("PIXKB_DSN", "")
	root := NewRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetArgs([]string{"query", "type = LegalArticle"})
	err := root.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no database DSN")
}
