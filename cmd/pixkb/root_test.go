package main

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewRootCmd(t *testing.T) {
	t.Parallel()

	cmd := NewRootCmd()
	require.NotNil(t, cmd)
	assert.Equal(t, "pixkb", cmd.Use)

	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"--version"})
	require.NoError(t, cmd.Execute())
	assert.Contains(t, out.String(), "0.0.0-dev")
}
