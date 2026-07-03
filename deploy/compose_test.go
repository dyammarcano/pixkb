package deploy_test

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestComposePinsPgvector(t *testing.T) {
	t.Parallel()

	raw, err := os.ReadFile("docker-compose.yml")
	require.NoError(t, err)

	var compose struct {
		Services map[string]struct {
			Image       string   `yaml:"image"`
			Ports       []string `yaml:"ports"`
			Environment []string `yaml:"environment"`
		} `yaml:"services"`
	}
	require.NoError(t, yaml.Unmarshal(raw, &compose))

	db, ok := compose.Services["db"]
	require.True(t, ok, "compose missing db service")
	assert.Equal(t, "pgvector/pgvector:pg17", db.Image)
	assert.Contains(t, db.Ports, "5432:5432")
	assert.Contains(t, db.Environment, "POSTGRES_DB=pixkb")
}
